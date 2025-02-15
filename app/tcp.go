package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gitlab.daho.tech/gdaho/util/derr"
)

/*
概述：TCP Proxy 作为POP上租户网络连接代理服务。
*/
type TcpRuleConf struct {
	TargetAddr string `json:"targetAddr"` //(必填)目标地址
	TargetPort int    `json:"targetPort"` //(必填)目标端口
	ListenPort int    `json:"listenPort"` //(必填)监听端口
	Reverse    bool   `json:"reverse"`    //(必填)反向代理
}

type TcpConf struct {
	Id       string        `json:"id"`       //(必填)
	EdgeId   string        `json:"edgeId"`   //(必填)Edge Id
	TcpRules []TcpRuleConf `json:"tcpRules"` //(必填)Rule信息
	TcpName  string        `json:"tcpName"`  //{不填}
}

const (
	tcpServerNameDef    = "default"
	tcpServerEdgePath   = "/home/tcpproxy/%s"
	tcpServerConfPath   = "/home/tcpproxy/%s/nginx.conf"
	tcpServerEnvPath    = "/home/tcpproxy/%s_%s.env"
	tcpServerRestart    = "restart tcpproxy@%s_%s"
	tcpServerStop       = "stop tcpproxy@%s_%s"
	tcpServerReload     = "reload tcpproxy@%s_%s"
	tcpServerConfFormat = `worker_processes  1;
events {
    worker_connections  1024;
}
stream {
{{- range .TcpRules}}
    upstream service_{{.ListenPort}} {
        server {{.TargetAddr}}:{{.TargetPort}};
    }
    server {
        listen {{.ListenPort}};
        proxy_connect_timeout 8s;
        proxy_timeout 24h;
        proxy_pass service_{{.ListenPort}};
    }
{{- end}}
}
`
)

func tcpGetServerConfPath(NamespaceId string) string {
	return fmt.Sprintf(tcpServerConfPath, NamespaceId)
}

func tcpServerEnvFileCreate(edgeId string, serverName string) error {
	var err error
	b := []byte("nsId=" + edgeId)
	EnvPath := fmt.Sprintf(tcpServerEnvPath, edgeId, serverName)

	defer func() {
		if err != nil {
			if public.FileExists(EnvPath) {
				os.Remove(EnvPath)
			}
		}
	}()

	if err = public.Write2File(EnvPath, b); err != nil {
		agentLog.AgentLogger.Info("creatEnvFile failed ", err)
		return err
	}
	return nil
}
func tcpServerConfCreate(fp *TcpConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := tcpGetServerConfPath(fp.EdgeId)
	tmpl, err = template.New("nginx").Parse(tcpServerConfFormat)
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

func restartTcpServer(fp *TcpConf) error {
	para := strings.Split(fmt.Sprintf(tcpServerRestart, fp.EdgeId, tcpServerNameDef), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func stopTcpServer(fp *TcpConf) error {
	para := strings.Split(fmt.Sprintf(tcpServerStop, fp.EdgeId, tcpServerNameDef), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func reloadTcpServer(fp *TcpConf) error {
	para := strings.Split(fmt.Sprintf(tcpServerReload, fp.EdgeId, tcpServerNameDef), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func (conf *TcpConf) Create(action int) error {
	var err error

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	if len(conf.TcpRules) == 0 {
		return errors.New("NoRules")
	}

	/* 设置edge内的tcp算法为bbr */
	err = public.VrfSetTcpCongestionControl(conf.EdgeId, "bbr")
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetTcpCongestionControl failed ", err)
		return err
	}

	/* 设置iptables重定向规则 */
	for _, rule := range conf.TcpRules {
		if !rule.Reverse {
			err = public.VrfSetInterfaceDnatRedirect(false, conf.EdgeId, "any", rule.ListenPort, "tcp", rule.TargetAddr, rule.TargetPort)
			if err != nil {
				return err
			}
		}
	}

	/* reload configre */
	if action == public.ACTION_ADD {
		/* 初始化env文件 */
		if err := tcpServerEnvFileCreate(conf.EdgeId, tcpServerNameDef); err != nil {
			agentLog.AgentLogger.Info("tcpServerEnvFileCreate creatEnvFile failed ", err)
			return err
		}

		/* 文件夹存在 先删除 */
		dirPath := fmt.Sprintf(tcpServerEdgePath, conf.EdgeId)
		if public.FileExists(dirPath) {
			os.RemoveAll(dirPath)
		}

		/* 公共的配置文件 */
		cmdstr := fmt.Sprintf("mkdir -p %s", dirPath)
		if err, _ := public.ExecBashCmdWithRet(cmdstr); err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		/* 创建配置配置文件 */
		confFile, err := tcpServerConfCreate(conf)
		agentLog.AgentLogger.Info("Create file: ", confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Create file failed ", confFile)
			return err
		}
	}

	/* restart tcp server */
	if err = restartTcpServer(conf); err != nil {
		agentLog.AgentLogger.Info("restartTcpServer failed ", err)
		return err
	}

	return nil
}

func (cfgCur *TcpConf) Modify(cfgNew *TcpConf) (error, bool) {

	var err error
	chg := false
	serverChg := false
	serverReload := false

	if cfgCur.EdgeId != cfgNew.EdgeId {
		return derr.Error{In: err.Error(), Out: "EdgeIdNotSame"}, false
	}

	if len(cfgCur.TcpRules) != 0 {
		serverReload = true
	}

	for _, old := range cfgCur.TcpRules {
		found := false
		for _, new := range cfgNew.TcpRules {
			if new.ListenPort == old.ListenPort &&
				new.TargetPort == old.TargetPort &&
				new.TargetAddr == old.TargetAddr {
				found = true
				if new.Reverse != old.Reverse {
					if !new.Reverse {
						public.VrfSetInterfaceDnatRedirect(false, cfgCur.EdgeId, "any", new.ListenPort, "tcp", new.TargetAddr, new.TargetPort)
					}

					if !old.Reverse {
						public.VrfSetInterfaceDnatRedirect(true, cfgCur.EdgeId, "any", old.ListenPort, "tcp", old.TargetAddr, old.TargetPort)
					}
				}
			}
		}

		if !found {
			//Del old
			if !old.Reverse {
				public.VrfSetInterfaceDnatRedirect(true, cfgCur.EdgeId, "any", old.ListenPort, "tcp", old.TargetAddr, old.TargetPort)
			}
			chg = true
			serverChg = true
		}
	}

	for _, new := range cfgNew.TcpRules {
		found := false
		for _, old := range cfgCur.TcpRules {
			if new.ListenPort == old.ListenPort &&
				new.TargetPort == old.TargetPort &&
				new.TargetAddr == old.TargetAddr {
				found = true
			}
		}

		if !found {
			//Add new
			if !new.Reverse {
				public.VrfSetInterfaceDnatRedirect(false, cfgCur.EdgeId, "any", new.ListenPort, "tcp", new.TargetAddr, new.TargetPort)
			}
			chg = true
			serverChg = true
		}
	}

	if serverChg {
		/* reload configure */
		confFile, err := tcpServerConfCreate(cfgNew)
		agentLog.AgentLogger.Info("Create file: ", confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Create file failed ", confFile)
			return err, false
		}

		if serverReload {
			/* reload server */
			//if err = reloadTcpServer(cfgCur); err != nil {
			if err = restartTcpServer(cfgCur); err != nil {
				agentLog.AgentLogger.Info("reloadTcpServer failed ", err)
				return err, false
			}
			agentLog.AgentLogger.Info("reloadTcpServer success.")
		} else {
			/* restarttcp server */
			if err = restartTcpServer(cfgCur); err != nil {
				agentLog.AgentLogger.Info("restartTcpServer failed ", err)
				return err, false
			}
			agentLog.AgentLogger.Info("restartTcpServer success.")
		}
	}

	return nil, chg
}

func (conf *TcpConf) Destroy() error {

	var err error

	/* stop tcp server */
	if err = stopTcpServer(conf); err != nil {
		agentLog.AgentLogger.Info("stopTcpServer failed ", err)
		return err
	}

	/* remove configre */
	/* 删除env文件 */
	envPath := fmt.Sprintf(tcpServerEnvPath, conf.EdgeId, tcpServerNameDef)
	if public.FileExists(envPath) {
		os.Remove(envPath)
	}

	/* 文件夹存在，删除 */
	dirPath := fmt.Sprintf(tcpServerEdgePath, conf.EdgeId)
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	/* 删除iptables重定向规则 */
	for _, rule := range conf.TcpRules {
		if !rule.Reverse {
			err = public.VrfSetInterfaceDnatRedirect(true, conf.EdgeId, "any", rule.ListenPort, "tcp", rule.TargetAddr, rule.TargetPort)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
