package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

type SslConf struct {
	Name          string `json:"name"`          //[继承]sslvpn名称
	EdgeId        string `json:"edgeId"`        //[继承]如果edgeId非空，则表示Edge内部sslvpn
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps
	LocalAddress  string `json:"localAddress"`  //(必填)sslvpn隧道本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)sslvpn隧道对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
	TunnelSrc     string `json:"tunnelSrc"`     //(必填)服务地址
	Port          int    `json:"port"`          //(必填)服务端口
	Protocol      string `json:"protocol"`      //(必填)服务协议
	Username      string `json:"username"`      //(必填)账号名称
	Passwd        string `json:"passwd"`        //(必填)账号密码
	ClientPool    string `json:"clientPool"`    //{不填}客户地址池
	Proto         string `json:"proto"`         //{不填}openVPN配置文件参数
}

const (
	sslDefaultConfPath = "/home/sslrsa_mgr/ns-1"
	sslNsPath          = "/home/sslrsa_mgr/%s"
	serverEnvPath      = "/home/sslrsa_mgr/%s_%s.env"
	serverConfPath     = "/home/sslrsa_mgr/%s/server-rsa/%s_default.conf"
	clientPwsConfPath  = "/home/sslrsa_mgr/%s/openvpn/psw-file"
	clientCcdConfPath  = "/home/sslrsa_mgr/%s/openvpn/ccd/%s"
	sysctl_cmd         = "systemctl"
	stopOpenvpn        = "stop openvpn@%s_%s"
	enableOpenvpn      = "enable openvpn@%s_%s"
	disableOpenvpn     = "disable openvpn@%s_%s"
	restartOpenvpn     = "restart openvpn@%s_%s"
	serverNameDef      = "default"
)

const (
	SSLServerConfFormat = `mode server
max-clients 16383
keepalive 5 60
persist-key
#comp-lzo
#topology subnet
#client-to-client
status default-status.log
log /var/log/sslvpn/{{.EdgeId}}_default.log
cert /home/sslrsa_mgr/{{.EdgeId}}/server-rsa/sslServer.crt
tun-mtu 1392
#mssfix 1400
#txqueuelen 1000
port {{.Port}}
cipher AES-128-CBC
persist-tun
isolation-name {{.EdgeId}}
dev {{.Name}}
dev-type tun
proto {{.Proto}}
ca /home/sslrsa_mgr/{{.EdgeId}}/server-rsa/ca.crt
dh /home/sslrsa_mgr/{{.EdgeId}}/server-rsa/dh.pem
#mute 20
isolation-type netns
local {{.TunnelSrc}}
key /home/sslrsa_mgr/{{.EdgeId}}/server-rsa/sslServer.key
verb 3
script-security 3
auth-user-pass-verify /home/sslrsa_mgr/{{.EdgeId}}/openvpn/checkpsw.sh via-env
username-as-common-name
verify-client-cert none
#client-cert-not-required
server {{.ClientPool}}
ifconfig-pool-persist ipp.txt
client-config-dir /home/sslrsa_mgr/{{.EdgeId}}/openvpn/ccd
`
	PswfileFormat = `{{.Username}} {{.Passwd}}
`
	CcdfileFormat = `ifconfig-push {{.RemoteAddress}} {{.LocalAddress}}
iroute 0.0.0.0 0.0.0.0
`
)

func sslGetServerConfPath(NamespaceId string) string {
	return fmt.Sprintf(serverConfPath, NamespaceId, NamespaceId)
}

func sslGetPwsConfPath(NamespaceId string) string {
	return fmt.Sprintf(clientPwsConfPath, NamespaceId)
}

func sslGetClientCcdConfPath(NamespaceId string, Username string) string {
	return fmt.Sprintf(clientCcdConfPath, NamespaceId, Username)
}

func stopOpenVPN(NamespaceId string, serverName string) error {
	para := strings.Split(fmt.Sprintf(stopOpenvpn, NamespaceId, serverName), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(fmt.Sprintf(disableOpenvpn, NamespaceId, serverName), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func restartOpenVPN(NamespaceId string) error {
	var err error
	para := strings.Split(fmt.Sprintf(enableOpenvpn, NamespaceId, serverNameDef), " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(fmt.Sprintf(restartOpenvpn, NamespaceId, serverNameDef), " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	return nil
}

func resetSSL(namespaceId string, device string, bandwidth int) error {

	var err error

	/* 服务端配置变更重启 openVPN进程 */
	if err = restartOpenVPN(namespaceId); err != nil {
		agentLog.AgentLogger.Info("restartOpenVPN  failed", namespaceId, err)
		return err
	}

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if public.VrfDeviceExist(namespaceId, device) {
			break
		}
	}

	/* 重启进程，限速需要重新配置 */
	err = public.VrfSetInterfaceIngressLimit(namespaceId, device, bandwidth)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceIngressLimit  failed: ", namespaceId, err)
		return err
	}

	return nil
}

func ServerConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetServerConfPath(fp.EdgeId)
	tmpl, err = template.New(fp.EdgeId).Parse(SSLServerConfFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func ClientCcdConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetClientCcdConfPath(fp.EdgeId, fp.Username)
	tmpl, err = template.New(fp.EdgeId).Parse(CcdfileFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func PwsConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetPwsConfPath(fp.EdgeId)
	tmpl, err = template.New(fp.EdgeId).Parse(PswfileFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func creatEnvFile(edgeId string, serverName string) error {
	var err error
	b := []byte("nsId=" + edgeId)
	EnvPath := fmt.Sprintf(serverEnvPath, edgeId, serverName)

	defer func() {
		if err != nil {
			if public.FileExists(EnvPath) {
				os.Remove(EnvPath)
			}
		}
	}()

	if err = public.Write2File(EnvPath, b); err != nil {
		agentLog.AgentLogger.Info("creatEnvFile failed")
		return err
	}
	return nil
}

func (conf *SslConf) Create(action int) error {
	var err error

	/* recover */
	if action == public.ACTION_RECOVER {
		if public.NsExist(conf.EdgeId) {
			if err := resetSSL(conf.EdgeId, conf.Name, conf.Bandwidth); err != nil {
				agentLog.AgentLogger.Info("RecoverSsl : resetSSL  failed", conf.EdgeId, err)
			}
		} else {
			agentLog.AgentLogger.Info("RecoverSsl : NamespaceNotExist failed, ", conf.EdgeId)
		}
		return nil
	}

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	/* 初始化env文件 */
	if err := creatEnvFile(conf.EdgeId, serverNameDef); err != nil {
		agentLog.AgentLogger.Info("CreateSslServer creatEnvFile failed")
		return err
	}

	//文件夹存在 先删除
	dirPath := fmt.Sprintf(sslNsPath, conf.EdgeId)
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	/* 拷贝公共的配置文件 */
	cmdstr := fmt.Sprintf("cp -r %s /home/sslrsa_mgr/%s", sslDefaultConfPath, conf.EdgeId)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* 修改 checkpsw.sh*/
	cmdstr = fmt.Sprintf(`sed -i "s/ns-1/%s/g" /home/sslrsa_mgr/%s/openvpn/checkpsw.sh`, conf.EdgeId, conf.EdgeId)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* 初始化基础数据 */
	/* 根据源地址计算 ClientPool */
	conf.ClientPool = public.GetCidrIpRange(conf.LocalAddress+"/29") + " 255.255.255.248"
	if strings.Contains(strings.ToLower(conf.Protocol), strings.ToLower("udp")) {
		conf.Proto = "udp"
	} else {
		conf.Proto = "tcp"
	}

	/* 生成conf 文件 */
	fileName, err := ServerConfCreate(conf)
	if err != nil {
		agentLog.AgentLogger.Info("ServerConfCreate failed ", fileName)
		return err
	}

	/* 生成账号密码文件 */
	fileName, err = PwsConfCreate(conf)
	if err != nil {
		agentLog.AgentLogger.Info("PwsConfCreate failed ", fileName)
		return err
	}

	/* 生产ccd文件 */
	fileName, err = ClientCcdConfCreate(conf)
	if err != nil {
		agentLog.AgentLogger.Info("ClientCcdConfCreate failed ", fileName)
		return err
	}

	if err = resetSSL(conf.EdgeId, conf.Name, conf.Bandwidth); err != nil {
		agentLog.AgentLogger.Info("ssl Create: resetSSL  failed", conf.EdgeId, err)
		return err
	}

	return nil
}

func (cfgCur *SslConf) Modify(cfgNew *SslConf) (error, bool) {
	var serverChg = false
	var chg = false

	if cfgCur.TunnelSrc != cfgNew.TunnelSrc {
		cfgCur.TunnelSrc = cfgNew.TunnelSrc
		chg = true
		serverChg = true
	}
	if cfgCur.Port != cfgNew.Port {
		cfgCur.Port = cfgNew.Port
		chg = true
		serverChg = true
	}
	if cfgCur.Protocol != cfgNew.Protocol {
		if strings.Contains(strings.ToLower(cfgNew.Protocol), strings.ToLower("udp")) {
			cfgNew.Proto = "udp"
		} else {
			cfgNew.Proto = "tcp"
		}
		cfgCur.Proto = cfgNew.Proto
		cfgCur.Protocol = cfgNew.Protocol
		chg = true
		serverChg = true
	}

	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		cfgCur.Bandwidth = cfgNew.Bandwidth
		if err := public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgCur.Bandwidth); err != nil {
			agentLog.AgentLogger.Info("VrfSetInterfaceIngressLimit  failed", cfgCur.EdgeId, err)
			return err, true
		}
		chg = true
	}

	if cfgCur.Username != cfgNew.Username || cfgCur.Passwd != cfgNew.Passwd {
		cfgCur.Username = cfgNew.Username
		cfgCur.Passwd = cfgNew.Passwd
		/* 重新生成账号密码文件 */
		fileName, err := PwsConfCreate(cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("PwsConfCreate failed ", fileName)
			return err, false
		}
		chg = true
	}

	if cfgCur.LocalAddress != cfgNew.LocalAddress || cfgCur.RemoteAddress != cfgNew.RemoteAddress {
		/* 重新计算clientPool */
		cfgNew.ClientPool = public.GetCidrIpRange(cfgNew.LocalAddress+"/29") + " 255.255.255.248"
		if cfgNew.ClientPool != cfgCur.ClientPool {
			cfgCur.ClientPool = cfgNew.ClientPool
			serverChg = true
		}
		cfgCur.LocalAddress = cfgNew.LocalAddress
		cfgCur.RemoteAddress = cfgNew.RemoteAddress
		/* 重新生成ccd文件 */
		fileName, err := ClientCcdConfCreate(cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("ClientCcdConfCreate failed ", fileName)
			return err, false
		}
		chg = true
	}

	if serverChg {
		fileName, err := ServerConfCreate(cfgCur)
		agentLog.AgentLogger.Info("ServerConfCreate: SSLCfgCur: ", cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("ServerConfCreate failed ", fileName)
			return nil, true
		}

		if err := resetSSL(cfgCur.EdgeId, cfgCur.Name, cfgCur.Bandwidth); err != nil {
			agentLog.AgentLogger.Info("SSL Modify: resetSSL  failed", cfgCur.EdgeId, err)
			return err, true
		}
	}

	return nil, chg
}

func (conf *SslConf) Destroy() error {

	//关闭openVpn进程
	if err := stopOpenVPN(conf.EdgeId, serverNameDef); err != nil {
		return err
	}

	//删除ns sslVpn相关的目录
	dirPath := fmt.Sprintf(sslNsPath, conf.EdgeId)
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	//删除env文件
	EnvPath := fmt.Sprintf(serverEnvPath, conf.EdgeId, serverNameDef)
	if public.FileExists(EnvPath) {
		os.Remove(EnvPath)
	}

	return nil
}
