package main

import (
	"datapath/agentLog"
	"datapath/app"
	"datapath/config"
	mceetcd "datapath/etcd"
	"datapath/public"
	"encoding/json"
	"fmt"
	"runtime"

	"gitlab.daho.tech/gdaho/etcd"
	"gitlab.daho.tech/gdaho/network"
)

var (
	etcdClient   *etcd.Client
	BackendNodes []string = []string{"http://127.0.0.1:2379"}
	logName               = "/var/log/pop/recover.log"
)

func phyifRecover() error {

	paths := []string{config.PhyifConfPath}
	phyifs, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("phyifs not found: ", err.Error())
		return nil
	}

	for _, value := range phyifs {
		agentLog.AgentLogger.Info("recover phyif: " + value)

		bytes := []byte(value)

		fp := &app.PhyifConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]phyif data unmarshal failed: ", err.Error())
			continue
		}

		chg, err := fp.Recover(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]phyif %s create failed: %s", fp.Id, err))
			continue
		}

		if chg {
			agentLog.AgentLogger.Info("phyif device change, id=", fp.Id)
			saveData, err := json.Marshal(fp)
			if err == nil {
				agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
				err = etcdClient.SetValue(config.PhyifConfPath+fp.Id, string(saveData[:]))
				if err != nil {
					agentLog.AgentLogger.Info("[ERROR]phyif data SetValue failed: ", err.Error())
				}
			}
		}
		agentLog.AgentLogger.Info("recover phyif success, id=" + fp.Id)
	}

	return nil
}

func portRecover() error {

	paths := []string{config.PortConfPath}
	ports, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("ports not found: ", err.Error())
		return nil
	}

	for _, value := range ports {
		agentLog.AgentLogger.Info("recover port: " + value)

		bytes := []byte(value)

		fp := &app.PortConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]port data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]port %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover port success, id=" + fp.Id)
	}

	return nil
}

func vapRecover() error {

	paths := []string{config.VapConfPath}
	vaps, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("vaps not found: ", err.Error())
		return nil
	}

	for _, value := range vaps {
		agentLog.AgentLogger.Info("recover vap: " + value)

		bytes := []byte(value)

		fp := &app.VapConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]vap data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]vap %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover vap success, id=" + fp.Id)
	}

	return nil
}

func linkTranRecover() error {

	paths := []string{config.LinkTranConfPath}
	linkTrans, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("linkEndps not found: ", err.Error())
		return nil
	}

	for _, value := range linkTrans {
		agentLog.AgentLogger.Info("recover linkTran: " + value)

		bytes := []byte(value)

		fp := &app.LinkTranConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]linkTran data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]linkTran %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover linkTran success, id=" + fp.Id)
	}

	return nil
}

func linkEndpRecover() error {

	paths := []string{config.LinkEndpConfPath}
	linkEndps, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("linkEndps not found: ", err.Error())
		return nil
	}

	for _, value := range linkEndps {
		agentLog.AgentLogger.Info("recover linkEndp: " + value)

		bytes := []byte(value)

		fp := &app.LinkEndpConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]linkEndp data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]linkEndp %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover linkEndp success, id=" + fp.Id)
	}

	return nil
}

func vplTranRecover() error {

	paths := []string{config.VplTranConfPath}
	vplTrans, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("vplTran not found: ", err.Error())
		return nil
	}

	for _, value := range vplTrans {
		agentLog.AgentLogger.Info("recover vplTran: " + value)

		bytes := []byte(value)

		fp := &app.LinkTranConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]vplTran data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]vplTran %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover vplTran success, id=" + fp.Id)
	}

	return nil
}

func vplEndpRecover() error {

	paths := []string{config.VplEndpConfPath}
	vplEndps, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("VplEndp not found: ", err.Error())
		return nil
	}

	for _, value := range vplEndps {
		agentLog.AgentLogger.Info("recover vplEndp: " + value)

		bytes := []byte(value)

		fp := &app.VplEndpConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]vplEndp data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]vplEndp %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover vplEndp success, id=" + fp.Id)
	}

	return nil
}

func natRecover() error {

	paths := []string{config.NatConfPath}
	natgws, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("nat not found: ", err.Error())
		return nil
	}

	for _, value := range natgws {
		agentLog.AgentLogger.Info("recover nat: " + value)

		bytes := []byte(value)

		fp := &app.NatConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]nat data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]nat %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover nat success, id=" + fp.Id)
	}

	return nil
}

func natshareRecover() error {

	paths := []string{config.NatshareConfPath}
	natgws, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("natshare not found: ", err.Error())
		return nil
	}

	for _, value := range natgws {
		agentLog.AgentLogger.Info("recover natshare: " + value)

		bytes := []byte(value)

		fp := &app.NatshareConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]natshare data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]natshare %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover natshare success, id=" + fp.Id)
	}

	return nil
}

func edgeRecover() error {

	paths := []string{config.EdgeConfPath}
	edges, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("edges not found: ", err.Error())
		return nil
	}

	for _, value := range edges {
		agentLog.AgentLogger.Info("recover edge: " + value)

		bytes := []byte(value)

		fp := &app.EdgeConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]edge data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]edge %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover edge success, id=" + fp.Id)
	}

	return nil
}

func tunnelRecover() error {

	paths := []string{config.TunnelConfPath}
	tunnels, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("tunnels not found: ", err.Error())
		return nil
	}

	for _, value := range tunnels {
		agentLog.AgentLogger.Info("recover tunnel: " + value)

		bytes := []byte(value)

		fp := &app.TunnelConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]tunnel data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]tunnel %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover tunnel success, id=" + fp.Id)
	}

	return nil
}

func connRecover() error {

	paths := []string{config.ConnConfPath}
	conns, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("conns not found: ", err.Error())
		return nil
	}

	for _, value := range conns {
		agentLog.AgentLogger.Info("recover conn: " + value)

		bytes := []byte(value)

		fp := &app.ConnConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]conn data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]conn %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover conn success, id=" + fp.Id)
	}

	return nil
}

func dnatRecover() error {

	paths := []string{config.DnatConfPath}
	dnats, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("dnats not found: ", err.Error())
		return nil
	}

	for _, value := range dnats {
		agentLog.AgentLogger.Info("recover dnat: " + value)

		bytes := []byte(value)

		fp := &app.DnatConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]dnat data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]dnat %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover dnat success, id=" + fp.Id)
	}

	return nil
}

func snatRecover() error {

	paths := []string{config.SnatConfPath}
	snats, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("dnats not found: ", err.Error())
		return nil
	}

	for _, value := range snats {
		agentLog.AgentLogger.Info("recover snat: " + value)

		bytes := []byte(value)

		fp := &app.SnatConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]snat data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]snat %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover snat success, id=" + fp.Id)
	}

	return nil
}

func tcpRecover() error {

	paths := []string{config.TcpConfPath}
	snats, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("tcps not found: ", err.Error())
		return nil
	}

	for _, value := range snats {
		agentLog.AgentLogger.Info("recover tcp: " + value)

		bytes := []byte(value)

		fp := &app.TcpConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]tcp data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]tcp %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover tcp success, id=" + fp.Id)
	}

	return nil
}

func iperf3Recover() error {

	paths := []string{config.Iperf3ConfPath}
	iperfs, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("iperf3 not found: ", err.Error())
		return nil
	}

	for _, value := range iperfs {
		agentLog.AgentLogger.Info("recover iperf3: " + value)

		bytes := []byte(value)

		fp := &app.ToolIperf3Conf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]iperf3 data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.StartService(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]iperf3 %s start failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover iperf3 success, id=" + fp.Id)
	}

	return nil
}

func frrRecover() error {

	err, _ := public.ExecBashCmdWithRet("systemctl restart frr")
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]systemctl restart frr failed.", err)
		return err
	}

	return nil
}

func PopRecover() error {
	//为了解决bgp第一次启动，找不到该目录，影响邻居建立不起来
	public.MakeDir("/var/run/netns")

	/* 底层骨干恢复 */
	if err := phyifRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]phyifRecover fail %s \n", err)
		return err
	}

	if err := portRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]portRecover fail %s \n", err)
		return err
	}

	if err := vapRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]vapRecover fail %s \n", err)
		return err
	}

	if err := linkEndpRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]linkEndpRecover fail %s \n", err)
		return err
	}

	if err := linkTranRecover(); err != nil {
		agentLog.AgentLogger.Error("linkTranRecover fail %s \n", err)
		return err
	}

	if err := vplEndpRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]vplEndpRecover fail %s \n", err)
		return err
	}

	if err := vplTranRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]vplTranRecover fail %s \n", err)
		return err
	}

	if err := natRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]natRecover fail %s \n", err)
		return err
	}

	if err := natshareRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]natshareRecover fail %s \n", err)
		return err
	}

	/* 租户业务恢复 */
	if err := edgeRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]edgeRecover fail %s \n", err)
		return err
	}

	if err := tunnelRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]tunnelRecover fail %s \n", err)
		return err
	}

	if err := connRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]connRecover fail %s \n", err)
		return err
	}

	if err := dnatRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]dnatRecover fail %s \n", err)
		return err
	}

	if err := snatRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]snatRecover fail %s \n", err)
		return err
	}

	if err := tcpRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]tcpRecover fail %s \n", err)
		return err
	}

	/* Router recover */
	if err := frrRecover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]frrRecover fail %s \n", err)
		return err
	}

	if err := iperf3Recover(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]iperf3Recover fail %s \n", err)
		return err
	}

	agentLog.AgentLogger.Info("pop configure recover success")
	return nil
}

func main() {
	var err error

	runtime.LockOSThread()
	agentLog.Init(logName)
	network.Init()
	agentLog.AgentLogger.Info("pop configure recover start.")

	err = mceetcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Error("[ERROR]init etcd failed: ", err.Error())
		return
	}

	etcdClient, err = etcd.NewEtcdClient(BackendNodes, "", "", "", false, "", "")
	if err != nil {
		agentLog.AgentLogger.Error("[ERROR]can not init etcd client: ", err.Error())
		return
	}

	if err = public.PopConfigInit(); err != nil {
		agentLog.AgentLogger.Error("[ERROR]configInit fail err: ", err)
		return
	}

	/* recovery 配置 */
	PopRecover()
}
