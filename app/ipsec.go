package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gitlab.daho.tech/gdaho/network"
	"gitlab.daho.tech/gdaho/util/derr"
)

type XfrmiConf struct {
	XfrmName  string `json:"xfrmName"`
	XfrmIdIn  int    `json:"xfrmIdIn"`
	XfrmIdOut int    `json:"xfrmIdOut"`
}

type IpsecSuite struct {
	Ikelifetime  string `json:"ikelifetime"`
	Lifetime     string `json:"lifetime"`
	Rekeytime    string `json:"rekeytime"`
	Authby       string `json:"authby"`
	Keyexchange  string `json:"keyexchange"`
	Mobike       string `json:"mobike"`
	Ike          string `json:"ike"`
	Esp          string `json:"esp"`
	IkeFull      string `json:"ikeFull"`
	EspFull      string `json:"espFull"`
	Left         string `json:"left"`
	Right        string `json:"right"`
	Leftsubnet   string `json:"leftsubnet"`
	Rightsubnet  string `json:"rightsubnet"`
	Auto         string `json:"auto"`
	Dpdaction    string `json:"dpdaction"`
	Dpddelay     string `json:"dpddelay"`
	Keyingtries  string `json:"keyingtries"`
	Leftid       string `json:"leftid"`
	Rightid      string `json:"rightid"`
	ConnName     string `json:"connname"`
	Aggressive   string `json:"aggressive"`
	ReplayWindow string `json:"replaywindow"`
	Secret       string `json:"secret"`
	XfrmiConf
}

type IpsecConf struct {
	Name          string `json:"name"`          //[继承]Ipsec名称
	EdgeId        string `json:"edgeId"`        //[继承]如果edgeId非空，则表示Edge内部ipsec
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps
	TunnelSrc     string `json:"tunnelSrc"`     //(必填)Ipsec本端地址
	TunnelDst     string `json:"tunnelDst"`     //(必填)Ipsec对端地址
	LocalAddress  string `json:"localAddress"`  //(必填)Ipsec隧道本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Ipsec隧道对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
	XfrmId        int    `json:"xfrmId"`        //(必填)Ipsec xfrmid，全局唯一，可以递增分发
	LeftFqdn      string `json:"leftFqdn"`      //(必填)本端Ipsec身份ID，只有主模式不能使用
	RightFqdn     string `json:"rightFqdn"`     //(必填)对端Ipsec身份ID，只有主模式不能使用
	IpsecSecret   string `json:"ipsecSecret"`   //(必填)Ipsec预共享密钥
	MainMode      bool   `json:"mainMode"`      //(必填)Ikev1的主模式，只有在remoteGateway模式下启用
	LifeTime      string `json:"lifeTime"`      //(必填)Ipsec sa生存期，0s,3600s,7200s
	RekeyTime     string `json:"rekeyTime"`     //(必填)Ipsec rekey生存期
	ReplayWin     bool   `json:"replayWin"`     //(必填)Ipsec抗重放开关
	Suite         IpsecSuite
}

const (
	xfrmi_cmd  = "/usr/libexec/strongswan/xfrmi"
	xfrmi_para = "--name %s --id %d --dev lo"

	/* 加载配置，sgw上只需要加载不需要启动，等待用户侧首先接入
	   断开一个连接, force选项不需要等待
	*/
	swanctl_cmd          = "swanctl"
	swanctl_cmd_load     = "--load-all"
	swanctl_cmd_terminal = "-t --ike %s --force"

	swanConfPath = "/etc/strongswan/swanctl/conf.d/"

	swanctl_cmd_listsa       = "--list-sa --ike %s --noblock"
	IpescMonitorIntervelFast = 30
	IpescMonitorIntervelSlow = 60
	numMin                   = 2
	numMax                   = 1000
	swanctl_cmd_clear_sa     = "swanctl -t --ike %s --force"
	swanctl_cmd_init_sa      = "swanctl --initiate --child %s --timeout 5"
)

const (
	SwanConfFormat = `
connections {
    {{.ConnName}} {
      local_addrs={{.Left}}
      {{- if ne .Right ""}}    
      remote_addrs={{.Right}}
      {{- end}}
      version=0
      {{- if ne .XfrmIdIn 0}}
      if_id_in={{.XfrmIdIn}}
      {{- end}}
      {{- if ne .XfrmIdOut 0}}
      if_id_out={{.XfrmIdOut}}
      {{- end}}
      rekey_time=0
      reauth_time=0
      {{- if ne .Mobike ""}}
      mobike={{.Mobike}}
      {{- end}}
      {{- if ne .Keyingtries ""}}
      keyingtries={{.Keyingtries}}
      {{- end}}
      {{- if ne .IkeFull ""}}
      proposals={{.IkeFull}}
      {{- end}}
      {{- if ne .Dpddelay ""}}
      dpd_delay={{.Dpddelay}}
      {{- end}}
      aggressive=yes
      local {
         auth={{.Authby}}
         id={{.Leftid}}
      }
      remote {
         auth={{.Authby}} 
         id={{.Rightid}}
      }
      children {
         {{.ConnName}} {
            {{- if ne .Leftsubnet ""}}
            local_ts={{.Leftsubnet}}
            {{- end}}
            {{- if ne .Rightsubnet ""}}
            remote_ts={{.Rightsubnet}}
            {{- end}}
            {{- if ne .Auto ""}}    
            start_action={{.Auto}}
            {{- end}}
            {{- if ne .Dpdaction ""}}
            dpd_action={{.Dpdaction}}
            {{- end}}
            {{- if ne .EspFull ""}}
            esp_proposals={{.EspFull}}
            {{- end}}
            {{- if ne .ReplayWindow ""}}
            replay_window={{.ReplayWindow}}
            {{- end}}
            {{- if ne .Rekeytime ""}}
            rekey_time={{.Rekeytime}}
            {{- end}}
            {{- if ne .Lifetime ""}}
            life_time={{.Lifetime}}
            {{- end}}
         }
      }
    }
}

secrets {
   ike-{{.ConnName}} {
      id-{{.ConnName}}-l = {{.Leftid}}
      id-{{.ConnName}}-r = {{.Rightid}}
      secret = {{.Secret}}
   }
}
`
	SwanRemoteConfFormat = `
connections {
    {{.ConnName}} {
      local_addrs={{.Left}}
      {{- if ne .Right ""}}    
      remote_addrs={{.Right}}
      {{- end}}
      version=0
      {{- if ne .XfrmIdIn 0}}
      if_id_in={{.XfrmIdIn}}
      {{- end}}
      {{- if ne .XfrmIdOut 0}}
      if_id_out={{.XfrmIdOut}}
      {{- end}}
      rekey_time=0
      reauth_time=0
      {{- if ne .Mobike ""}}
      mobike={{.Mobike}}
      {{- end}}
      {{- if ne .Keyingtries ""}}
      keyingtries={{.Keyingtries}}
      {{- end}}
      {{- if ne .IkeFull ""}}
      proposals={{.IkeFull}}
      {{- end}}
      {{- if ne .Dpddelay ""}}
      dpd_delay={{.Dpddelay}}
      {{- end}}
      aggressive={{.Aggressive}}
      local {
         auth=psk
         id={{.Leftid}}
      }
      remote {
        auth=psk
        id={{.Rightid}} 
      }
      children {
         {{.ConnName}} {
            {{- if ne .Leftsubnet ""}}
            local_ts={{.Leftsubnet}}
            {{- end}}
            {{- if ne .Rightsubnet ""}}
            remote_ts={{.Rightsubnet}}
            {{- end}}
            {{- if ne .Auto ""}}    
            start_action={{.Auto}}
            {{- end}}
            {{- if ne .Dpdaction ""}}
            dpd_action={{.Dpdaction}}
            {{- end}}
            # start_action=start
            # dpd_action=restart
            # close_action=restart
            {{- if ne .EspFull ""}}
            esp_proposals={{.EspFull}}
            {{- end}}
            {{- if ne .ReplayWindow ""}}
            replay_window={{.ReplayWindow}}
            {{- end}}
            {{- if ne .Rekeytime ""}}
            rekey_time={{.Rekeytime}}
            {{- end}}
            {{- if ne .Lifetime ""}}
            life_time={{.Lifetime}}
            {{- end}}
         }
      }
    }
}

secrets {
   ike-{{.ConnName}} {
      id-{{.ConnName}}-l = {{.Leftid}}
      id-{{.ConnName}}-r = {{.Rightid}}
      secret = {{.Secret}}
   }
}
`
)

func GetIpsecConfFile(connName string) string {
	return swanConfPath + connName + ".conf"
}

func TerminalIpsecConn(connName string) error {
	para := strings.Split(fmt.Sprintf(swanctl_cmd_terminal, connName), " ")
	if err := public.ExecCmd(swanctl_cmd, para...); err != nil {
		if strings.Contains(err.Error(), "no matching") {
			return nil
		}
		return err
	}
	return nil
}

func PersistentIpsecConn(fp *IpsecConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := GetIpsecConfFile(fp.Name)

	if fp.TunnelDst == "" {
		/* 如果不是remote Gateway模式 */
		tmpl, err = template.New(fp.Name).Parse(SwanConfFormat)
		if err != nil {
			return confFile, err
		}
	} else {
		tmpl, err = template.New(fp.Name).Parse(SwanRemoteConfFormat)
		if err != nil {
			return confFile, err
		}
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

	if err = tmpl.Execute(fileConf, fp.Suite); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func LoadIpsecConn() error {
	if err := public.ExecCmd(swanctl_cmd, swanctl_cmd_load); err != nil {
		return err
	}

	return nil
}

func InitIpsecConn(conf *IpsecConf) error {

	if conf.TunnelDst != "" {
		para := strings.Split(fmt.Sprintf(swanctl_cmd_init_sa, conf.Name), " ")
		if err, _ := public.ExecCmdWithRet(swanctl_cmd, para...); err != nil {
			return err
		}
	}
	return nil
}

func deleteIpsecConf(connName string) {

	go func() {
		agentLog.AgentLogger.Info("delete ipsec routine start, connName=" + connName)

		defer func() { recover() }()
		err := TerminalIpsecConn(connName)
		if err != nil {
			agentLog.AgentLogger.Warn(fmt.Sprintf("Terminal ipsec conn failed, connName=%s, err=%s", connName, err.Error()))
		}
		agentLog.AgentLogger.Info("Terminal ipsec conn sucess, connName=" + connName)

		// 移除配置文件，并加载
		file := GetIpsecConfFile(connName)
		if public.FileExists(file) {
			err = os.Remove(file)
			if err != nil {
				agentLog.AgentLogger.Warn(fmt.Sprintf("Remove ipsec config file failed, connName=%s, err=%s", connName, err.Error()))
			}
		}
		agentLog.AgentLogger.Info("remove ipsec config file, connName=" + connName)

		err = LoadIpsecConn()
		if err != nil {
			agentLog.AgentLogger.Warn(fmt.Sprintf("Load ipsec conn failed, connName=%s, err=%s", connName, err.Error()))
		}
		agentLog.AgentLogger.Info("reload ipsec conn success, connName=" + connName)

	}()
}

func InitRomteGateWayIpsecConf(fp *IpsecConf) error {

	ipsec := &fp.Suite
	ipsec.ConnName = fp.Name
	ipsec.Secret = fp.IpsecSecret

	leftIP := strings.Split(fp.TunnelSrc, "/")[0]
	ipsec.Left = leftIP
	ipsec.Right = fp.TunnelDst

	if fp.LeftFqdn != "" {
		ipsec.Leftid = fp.LeftFqdn
	} else {
		ipsec.Leftid = leftIP
	}
	if fp.RightFqdn != "" {
		ipsec.Rightid = fp.RightFqdn
	} else {
		ipsec.Rightid = fp.TunnelDst
	}

	if ipsec.Leftsubnet == "" {
		ipsec.Leftsubnet = "0.0.0.0/0"
	}
	if ipsec.Rightsubnet == "" {
		ipsec.Rightsubnet = "0.0.0.0/0"
	}

	if ipsec.Keyexchange == "" {
		ipsec.Keyexchange = "ikev2"
	}

	if fp.MainMode {
		ipsec.Aggressive = "no"
	} else {
		ipsec.Aggressive = "yes"
	}

	ipsec.Ike = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1536-modp1024-modp2048-modp4096-curve25519-x25519"
	ipsec.IkeFull = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096-curve25519-x25519"

	ipsec.Esp = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512,3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096,default"
	ipsec.EspFull = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512,3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096,default"

	// 某些第三方厂商不支持下划线
	//ipsec.Auto = "start"
	ipsec.Auto = "none"
	if ipsec.Mobike == "" {
		ipsec.Mobike = "no"
	}
	if ipsec.Dpdaction == "" {
		//ipsec.Dpdaction = "restart"
		ipsec.Dpdaction = "clear"
	}
	if ipsec.Dpddelay == "" {
		ipsec.Dpddelay = "10"
	}
	if ipsec.Keyingtries == "" {
		ipsec.Keyingtries = "0"
	}
	if ipsec.Authby == "" {
		ipsec.Authby = "psk"
	}

	if !fp.ReplayWin {
		ipsec.ReplayWindow = "0"
	}

	if fp.LifeTime == "3600" {
		ipsec.Lifetime = "3600s"
		ipsec.Rekeytime = "3300s"
	} else if fp.LifeTime == "7200" {
		ipsec.Lifetime = "7200s"
		ipsec.Rekeytime = "6600s"
	} else if fp.LifeTime == "14400" {
		ipsec.Lifetime = "14400s"
		ipsec.Rekeytime = "13200s"
	} else if fp.LifeTime == "28800" {
		ipsec.Lifetime = "28800s"
		ipsec.Rekeytime = "26400s"
	} else if fp.LifeTime == "43200" {
		ipsec.Lifetime = "43200s"
		ipsec.Rekeytime = "39600s"
	} else {
		ipsec.Lifetime = "0"
		ipsec.Rekeytime = "0"
	}

	ipsec.XfrmiConf.XfrmIdIn = fp.XfrmId
	ipsec.XfrmiConf.XfrmIdOut = fp.XfrmId
	ipsec.XfrmiConf.XfrmName = fp.Name

	return nil
}

func InitIpsecConf(fp *IpsecConf) error {

	ipsec := &fp.Suite
	ipsec.ConnName = fp.Name
	ipsec.Secret = fp.IpsecSecret

	leftIP := strings.Split(fp.TunnelSrc, "/")[0]
	ipsec.Left = leftIP
	if ipsec.Right == "" {
		ipsec.Right = "0.0.0.0/0"
	}

	if ipsec.Leftsubnet == "" {
		ipsec.Leftsubnet = "0.0.0.0/0"
	}
	if ipsec.Rightsubnet == "" {
		ipsec.Rightsubnet = "0.0.0.0/0"
	}

	if ipsec.Keyexchange == "" {
		ipsec.Keyexchange = "ikev2"
	}

	ipsec.Aggressive = "yes"

	ipsec.Ike = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1536-modp1024-modp2048-modp4096-curve25519-x25519"
	ipsec.IkeFull = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096-curve25519-x25519"

	ipsec.Esp = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512,3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096,default"
	ipsec.EspFull = "3des-aes128-aes192-aes256-md5-sha1-sha256-sha512,3des-aes128-aes192-aes256-md5-sha1-sha256-sha512-modp768-modp1024-modp1536-modp2048-modp4096,default"

	// 某些第三方厂商不支持下划线
	if ipsec.Leftid == "" {
		ipsec.Leftid = fp.LeftFqdn
	}

	if ipsec.Rightid == "" {
		ipsec.Rightid = fp.RightFqdn
	}

	ipsec.Auto = "none"
	if ipsec.Mobike == "" {
		ipsec.Mobike = "no"
	}
	if ipsec.Dpdaction == "" {
		ipsec.Dpdaction = "clear"
	}
	if ipsec.Dpddelay == "" {
		ipsec.Dpddelay = "10"
	}
	if ipsec.Keyingtries == "" {
		ipsec.Keyingtries = "0"
	}
	if ipsec.Authby == "" {
		ipsec.Authby = "psk"
	}

	if !fp.ReplayWin {
		ipsec.ReplayWindow = "0"
	}

	if fp.LifeTime == "3600" {
		ipsec.Lifetime = "3600s"
		ipsec.Rekeytime = "3300s"
	} else if fp.LifeTime == "7200" {
		ipsec.Lifetime = "7200s"
		ipsec.Rekeytime = "6600s"
	} else if fp.LifeTime == "14400" {
		ipsec.Lifetime = "14400s"
		ipsec.Rekeytime = "13200s"
	} else if fp.LifeTime == "28800" {
		ipsec.Lifetime = "28800s"
		ipsec.Rekeytime = "26400s"
	} else if fp.LifeTime == "43200" {
		ipsec.Lifetime = "43200s"
		ipsec.Rekeytime = "39600s"
	} else {
		ipsec.Lifetime = "0"
		ipsec.Rekeytime = "0"
	}

	ipsec.XfrmiConf.XfrmIdIn = fp.XfrmId
	ipsec.XfrmiConf.XfrmIdOut = fp.XfrmId
	ipsec.XfrmiConf.XfrmName = fp.Name

	return nil
}

func RollbackCreateIpsecConf(fp *IpsecConf) {
	if err := recover(); err != nil {
		deleteIpsecConf(fp.Name)
		panic(err)
	}
}

func CreateIpsecConf(fp *IpsecConf, action int) error {
	agentLog.AgentLogger.Info("create and reload ipsec, connName=" + fp.Name)

	//create ipsec conf and secret
	if fp.TunnelDst == "" {
		/* 如果不是remote Gateway模式 */
		err := InitIpsecConf(fp)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "IpsecConfInitError"}
		}
	} else {
		err := InitRomteGateWayIpsecConf(fp)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "RemoteIpsecConfInitError"}
		}
	}

	if public.ACTION_ADD == action {
		fileConf, err := PersistentIpsecConn(fp)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "PersistentIpsecConnError"}
		}
		defer func() {
			if err != nil {
				os.Remove(fileConf)
			}
		}()
	}

	err := LoadIpsecConn()
	if err != nil {
		return err
	}

	agentLog.AgentLogger.Info("create and reload ipsec success, connName=" + fp.Name)

	return nil
}

func GetIpsecConnName(connName string) string {
	return connName
}

/*-----------创建xfrm接口-----------------*/
func CreateXfrmLink(connName string, xfrmId int) error {
	name := GetIpsecConnName(connName)
	para := strings.Split(fmt.Sprintf(xfrmi_para, name, xfrmId), " ")

	if err := public.ExecCmd(xfrmi_cmd, para...); err != nil {
		return err
	}

	return nil
}

func GetIPsecPorts(fp *IpsecConf) []string {
	var ipsecPort []string
	ipsecPort = append(ipsecPort, GetIpsecConnName(fp.Name))
	return ipsecPort
}

func ipsecNetworkinit(fp *IpsecConf, ipsecPorts []string) error {

	for _, port := range ipsecPorts {

		if err := network.SetLinkUp(port); err != nil {
			agentLog.AgentLogger.Info("initNetnsNetwork SetLinkUp port:", port)
			return derr.Error{In: err.Error(), Out: "SetLinkUpError"}
		}

		if fp.LocalAddress != "" {
			if err := network.AssignIP(port, fp.LocalAddress+"/32"); err != nil {
				agentLog.AgentLogger.Info("initNetnsNetwork xfrm AssignIp failed !!!" + fp.LocalAddress)
				return derr.Error{In: err.Error(), Out: "AssignIPError"}
			}
		}

		if err := public.SetInterfaceIngressLimit(port, fp.Bandwidth); err != nil {
			return derr.Error{In: err.Error(), Out: "SetXfrmRouteError"}
		}
	}

	if err := network.SetLinkUp("lo"); err != nil {
		agentLog.AgentLogger.Info("SetLoLinkUp error ", "lo")
		return derr.Error{In: err.Error(), Out: "SetLoUpError"}
	}

	return nil
}

func ipsecVrfNetworkinit(fp *IpsecConf, ipsecPorts []string) error {

	for _, port := range ipsecPorts {
		if err := network.LinkSetNS(port, fp.EdgeId); err != nil {
			return derr.Error{In: err.Error(), Out: "LinkSetNSError"}
		}
	}

	if err := network.SwitchNS(fp.EdgeId); err != nil {
		return derr.Error{In: err.Error(), Out: "NamespaceSetError"}
	}
	defer network.SwitchOriginNS()

	for _, port := range ipsecPorts {

		if err := network.SetLinkUp(port); err != nil {
			agentLog.AgentLogger.Info("initNetnsNetwork SetLinkUp port:", port)
			return derr.Error{In: err.Error(), Out: "SetLinkUpError"}
		}

		if fp.LocalAddress != "" {
			if err := network.AssignIP(port, fp.LocalAddress+"/32"); err != nil {
				agentLog.AgentLogger.Info("initNetnsNetwork xfrm AssignIp failed !!!" + fp.LocalAddress)
				return derr.Error{In: err.Error(), Out: "AssignIPError"}
			}
		}

		if err := public.SetInterfaceIngressLimit(port, fp.Bandwidth); err != nil {
			return derr.Error{In: err.Error(), Out: "SetXfrmRouteError"}
		}
	}

	if err := network.SetLinkUp("lo"); err != nil {
		agentLog.AgentLogger.Info("SetLoLinkUp error ", "lo")
		return derr.Error{In: err.Error(), Out: "SetLoUpError"}
	}

	return nil
}

func ModifyIpsecConf(cfgCur *IpsecConf, cfgNew *IpsecConf) (bool, error) {
	var chg = false
	var err error

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
	}

	if cfgCur.EdgeId == "" {

		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			err = public.SetInterfaceIngressLimit(cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return false, err
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			//chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {
				err = public.SetInterfaceAddress(true, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return false, err
				}

				err = public.SetInterfaceAddress(false, cfgNew.Name, cfgNew.LocalAddress)
				if err != nil {
					return false, err
				}

				cfgCur.LocalAddress = cfgNew.LocalAddress
				//chg = true
			}
			if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
				find := false
				/* 如果是vap，需要检查vap关联linkendp实例的RemoteAddress地址是否和vap旧的RemoteAddress地址相同，如果相同，则不能删除 */
				if strings.Contains(cfgCur.Name, "vap") {
					find = CheckLinkEndpRemoteAddrExist(cfgCur.Name, cfgCur.RemoteAddress)
				} else if strings.Contains(cfgCur.Name, "conn") {
					find = CheckConnCidrRemoteAddrExist(cfgCur.Name, cfgCur.RemoteAddress)
				}
				if !find && cfgCur.RemoteAddress != "" {
					/* (FRR) del RemoteAddress route */
					err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name)
					if err != nil {
						return false, err
					}
				}

				if cfgNew.RemoteAddress != "" {
					/* (FRR) add RemoteAddress route */
					err = AddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgNew.Name)
					if err != nil {
						return false, err
					}
				}
				cfgCur.RemoteAddress = cfgNew.RemoteAddress
				//chg = true
			}
		}
	} else {
		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			err = public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return false, err
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			//chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {
				err = public.VrfSetInterfaceAddress(true, cfgCur.EdgeId, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return false, err
				}

				err = public.VrfSetInterfaceAddress(false, cfgNew.EdgeId, cfgNew.Name, cfgNew.LocalAddress)
				if err != nil {
					return false, err
				}
				cfgCur.LocalAddress = cfgNew.LocalAddress
				//chg = true
			}

			if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
				if cfgCur.RemoteAddress != "" {
					/* (FRR) del RemoteAddress route */
					err = VrfAddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name, cfgCur.EdgeId)
					if err != nil {
						return false, err
					}
				}

				if cfgNew.RemoteAddress != "" {
					/* (FRR) add RemoteAddress route */
					err = VrfAddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgNew.Name, cfgCur.EdgeId)
					if err != nil {
						return false, err
					}
				}
				cfgCur.RemoteAddress = cfgNew.RemoteAddress
				//chg = true
			}
		}
	}

	if cfgCur.IpsecSecret != cfgNew.IpsecSecret {
		chg = true
		cfgCur.IpsecSecret = cfgNew.IpsecSecret
	}

	if cfgCur.LeftFqdn != cfgNew.LeftFqdn {
		cfgCur.LeftFqdn = cfgNew.LeftFqdn
		chg = true
	}

	if cfgCur.RightFqdn != cfgNew.RightFqdn {
		cfgCur.RightFqdn = cfgNew.RightFqdn
		chg = true
	}

	if cfgCur.TunnelDst != cfgNew.TunnelDst {
		cfgCur.TunnelDst = cfgNew.TunnelDst
		chg = true
	}

	if cfgCur.TunnelSrc != cfgNew.TunnelSrc {
		cfgCur.TunnelSrc = cfgNew.TunnelSrc
		chg = true
	}

	if cfgCur.MainMode != cfgNew.MainMode {
		cfgCur.MainMode = cfgNew.MainMode
		chg = true
	}

	if cfgCur.ReplayWin != cfgNew.ReplayWin {
		cfgCur.ReplayWin = cfgNew.ReplayWin
		chg = true
	}

	if cfgCur.LifeTime != cfgNew.LifeTime {
		cfgCur.LifeTime = cfgNew.LifeTime
		chg = true
	}

	if cfgCur.RekeyTime != cfgNew.RekeyTime {
		cfgCur.RekeyTime = cfgNew.RekeyTime
		chg = true
	}

	return chg, nil
}

func (conf *IpsecConf) Create(action int) error {

	var err error

	if conf.EdgeId != "" && !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	if err := CreateIpsecConf(conf, action); err != nil {
		return derr.Error{In: err.Error(), Out: "CreateIpsecConfError"}
	}

	defer RollbackCreateIpsecConf(conf)

	//创建xfrm接口
	if err := CreateXfrmLink(conf.Name, conf.XfrmId); err != nil {
		return derr.Error{In: err.Error(), Out: "CreateXfrmLinkError"}
	}
	ipsecPorts := GetIPsecPorts(conf)
	if conf.EdgeId == "" {
		if err := ipsecNetworkinit(conf, ipsecPorts); err != nil {
			return derr.Error{In: err.Error(), Out: "ipsecNetworkinitError"}
		}
	} else {
		if err := ipsecVrfNetworkinit(conf, ipsecPorts); err != nil {
			return derr.Error{In: err.Error(), Out: "ipsecVrfNetworkinitError"}
		}
	}

	/*配置remote IP 的路由*/
	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add RemoteAddress route */
		if conf.EdgeId != "" {
			err := VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			if err != nil {
				return err
			}
		} else {
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (cfgCur *IpsecConf) Modify(cfgNew *IpsecConf) (error, bool) {
	var err error

	if cfgNew.EdgeId != "" && !public.NsExist(cfgNew.EdgeId) {
		return errors.New("VrfNotExist"), false
	}
	if cfgNew.TunnelDst == "" {
		/* 如果不是remote Gateway模式 */
		err = InitIpsecConf(cfgNew)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "InitIpsecConfError"}, false
		}
	} else {
		err = InitRomteGateWayIpsecConf(cfgNew)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "RemoteIpsecConfInitError"}, false
		}
	}

	ipsecChg, err := ModifyIpsecConf(cfgCur, cfgNew)
	if err != nil {
		return derr.Error{In: err.Error(), Out: "ModifyIpsecConf"}, false
	}

	if ipsecChg {
		_, err = PersistentIpsecConn(cfgNew)
		if err != nil {
			return err, false
		}

		defer func() {
			if err != nil {
				PersistentIpsecConn(cfgCur)
			}
		}()

		// 生效配置
		err = LoadIpsecConn()
		if err != nil {
			return err, false
		}

		//删除旧连接
		if err = TerminalIpsecConn(cfgCur.Name); err != nil {
			return err, false
		}

		// 如果是Remote Gateway模式，则启动init
		InitIpsecConn(cfgNew)

		return nil, true
	}

	return nil, true
}

func (conf *IpsecConf) Destroy() error {

	var err error

	// 删除ipsec 配置文件并重新加载
	if conf.EdgeId != "" && !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	/* (FRR) delete ipsec RemoteAddress route */
	if conf.RemoteAddress != "" {
		if conf.EdgeId != "" {
			err := VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			if err != nil {
				return err
			}
		} else {
			err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			if err != nil {
				return err
			}
		}
	}

	if conf.EdgeId != "" && network.SwitchNS(conf.EdgeId) != nil {
		return derr.Error{In: err.Error(), Out: "EdgeSetError"}
	} else {
		file := GetIpsecConfFile(conf.Name)
		if public.FileExists(file) {
			// 移除配置文件，并加载
			err = os.Remove(file)
			if err != nil {
				return err
			}
			defer func() {
				if err != nil {
					PersistentIpsecConn(conf)
				}
			}()
		}
		// 生效配置
		err = LoadIpsecConn()
		if err != nil {
			return err
		}
		// 删除旧连接
		err = TerminalIpsecConn(conf.Name)
		if err != nil {
			return err
		}
		ipsecPorts := GetIPsecPorts(conf)
		for _, port := range ipsecPorts {
			err = network.DeleteLink(port)
			if err != nil {
				return derr.Error{In: err.Error(), Out: "DestroyPortError"}
			}
		}

		if conf.EdgeId != "" {
			network.SwitchOriginNS()
		}
	}

	return nil
}

func (conf *IpsecConf) InitIpsecSa() (error, string) {

	var err error
	var saStr string
	para := strings.Split(fmt.Sprintf(swanctl_cmd_terminal, conf.Name), " ")
	if err = public.ExecCmd(swanctl_cmd, para...); err != nil {
		if conf.TunnelDst == "" /* && strings.Contains(err.Error(), "no matching") */ {
			return nil, err.Error()
		}
	}

	/* 如果是Remote Gateway模式，则启动init */
	if conf.TunnelDst != "" {
		para = strings.Split(fmt.Sprintf(swanctl_cmd_init_sa, conf.Name), " ")
		if err, saStr = public.ExecCmdWithRet(swanctl_cmd, para...); err != nil {
			return nil, err.Error()
		}
	}

	return nil, saStr
}

func (conf *IpsecConf) GetIpsecSa() (error, string) {

	var err error
	var saStr string
	para := strings.Split(fmt.Sprintf(swanctl_cmd_listsa, conf.Name), " ")
	if err, saStr = public.ExecCmdWithRet(swanctl_cmd, para...); err != nil {
		agentLog.AgentLogger.Error("swanctl --list-sa exec err: ", err, "cmd: ", para)
		return err, ""
	}

	return nil, saStr
}
