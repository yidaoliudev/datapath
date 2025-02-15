package main

import (
	"context"
	"datapath/agentLog"
	"datapath/app"
	"datapath/config"
	"datapath/public"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/etcd"
	"gitlab.daho.tech/gdaho/log"
	"gitlab.daho.tech/gdaho/network"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type VapStatusToCore struct {
	Status string `json:"status"` /* UP 或者 DOWN */
	Tsms   int64  `json:"ts"`
}

type VapCheckConf struct {
	Type     int
	PingType int
	ObjType  int
	Target   string
	Source   string
}

type VapStatus struct {
	Namespace string `json:"namespace"`
	Id        string `json:"id"`
	Tsms      string `json:"tsms"`
	Status    string `json:"status"`
}

type VapStatusCtl struct {
	VapStatus
	Conf      VapCheckConf
	DownCnt   int
	AgeFlag   bool
	StatusChg bool
	ChgTime   time.Time
}

type DelayResult struct {
	VapId     string
	Delay     float64
	StartTime time.Time
}

type RespConfig struct {
	Success bool   `json:"success"`
	Ret     int    `json:"ret"`
	Code    string `json:"code"`
	Msg     string `json:"msg"`
}

const (
	DOWNCNT_MAX       = 3
	TIMEOUT           = 1000
	VapDetectInterval = 2
	ProtocolICMP      = 1
	fakeLatency       = 0xFFFF
	EtcdServer        = "127.0.0.1:2379"

	/*  Vap check Type: 检查类型 */
	VapCheckType_Ipsec = 0
	VapCheckType_Gre   = 1
	VapCheckType_Port  = 2

	/* Vap Obj Tyoe：实例类型 */
	VapObjType_Port  = 0
	VapObjType_Vap   = 1
	VapObjType_Link  = 2
	VapObjType_Conn  = 3
	VapObjType_Tunn  = 4
	VapObjType_Ipsec = 5
	VapObjType_Nat   = 6

	/* PingType : Ping方式 */
	PingType_NoPing    = 0
	PingType_PingInNs  = 1
	PingType_PingOutNs = 2
)

var (
	EtcdClient         *etcd.Client
	swanctl_cmd        = "swanctl"
	swanctl_cmd_listsa = "--list-sa --ike %s --noblock"
	logName            = "/var/log/pop/monitor.log"
	timestampFilepath  = "/var/log/pop/monitor.timestamp"
	G_VapStatusCtl     = make(map[string]*VapStatusCtl)
	Up                 = 1
	Down               = 0
	G_SmoothFlag       = true
	monitorpath        = "/var/run/monitor/"
)

func updateStatusToFile(status, filepath string) error {

	if status == public.VAPNORMAL {
		if !public.FileExists(filepath) {
			fileConf, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
			if err != nil && !os.IsExist(err) {
				agentLog.AgentLogger.Info("updateStatusToFile OpenFile fail: ", filepath, err)
				return err
			}
			defer fileConf.Close()
			agentLog.AgentLogger.Info("updateStatusToFile change to Up: ", filepath)
		}
	} else {
		if public.FileExists(filepath) {
			err := os.Remove(filepath)
			if err != nil {
				agentLog.AgentLogger.Info("updateStatusToFile Remove fail: ", filepath, err)
				return err
			}
			agentLog.AgentLogger.Info("updateStatusToFile change to Down: ", filepath)
		}
	}

	return nil
}

func updateChangedToFile(filepath string) error {

	if !public.FileExists(filepath) {
		fileConf, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
		if err != nil && !os.IsExist(err) {
			agentLog.AgentLogger.Info("updateChangedToFile OpenFile fail: ", filepath, err)
			return err
		}
		defer fileConf.Close()
		agentLog.AgentLogger.Info("updateChangedToFile has changed: ", filepath)
	}

	return nil
}

func updateStatusNoCheck(resStatus *DelayResult, vapStatus *VapStatusCtl) {
	StatusChg := false
	if resStatus.Delay == 0 {
		vapStatus.DownCnt++
		if resStatus.Delay == 0 && vapStatus.Status != public.VAPOFFLINE && vapStatus.DownCnt >= DOWNCNT_MAX {
			vapStatus.Status = public.VAPOFFLINE
			vapStatus.StatusChg = true
			StatusChg = true
			vapStatus.ChgTime = resStatus.StartTime
			agentLog.AgentLogger.Info("Monitor [", vapStatus.Id, "] Status Change to Down Namespace:", vapStatus.Namespace)
		}
	} else {
		vapStatus.DownCnt = 0
		if resStatus.Delay != 0 && vapStatus.Status != public.VAPNORMAL {
			vapStatus.Status = public.VAPNORMAL
			vapStatus.StatusChg = true
			StatusChg = true
			vapStatus.ChgTime = resStatus.StartTime
			agentLog.AgentLogger.Info("Monitor [", vapStatus.Id, "] Status Change to Up Namespace:", vapStatus.Namespace)
		}
	}

	if StatusChg && vapStatus.Conf.ObjType != VapObjType_Ipsec {
		updateChangedToFile(monitorpath + resStatus.VapId + ".changed")
	}

	if vapStatus.Conf.ObjType != VapObjType_Ipsec {
		updateStatusToFile(vapStatus.Status, monitorpath+resStatus.VapId)
	}
}

/*
port check
*/
func checkPortStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//port status
	/* 如果port没有开启healthcheck，则在Namespace中填充LogicPortName */
	cmdstr := fmt.Sprintf(`ifconfig %s | grep "flags"`, vap.Namespace)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("IfconfigStatusDetect:", cmdstr, err)
	} else {
		if strings.Contains(result_str, vap.Namespace) &&
			strings.Contains(result_str, "RUNNING") &&
			strings.Contains(result_str, "UP") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
gre check
*/
func checkGreStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//gre status
	var cmdstr string
	if vap.Namespace == "" {
		cmdstr = fmt.Sprintf(`ifconfig %s | grep "flags"`, vap.Id)
	} else {
		cmdstr = fmt.Sprintf(`ip netns exec %s ifconfig %s | grep "flags"`, vap.Namespace, vap.Id)
	}
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("IfconfigStatusDetect:", cmdstr, err)
	} else {
		if strings.Contains(result_str, vap.Id) &&
			strings.Contains(result_str, "RUNNING") &&
			strings.Contains(result_str, "UP") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
ipsec vpn check
*/
func checkIpsecStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//ipsec tunnel
	/* 设置id */
	id := vap.Id
	if vap.Conf.ObjType == VapObjType_Ipsec {
		id = strings.Split(vap.Id, "/")[0]
	}

	para := strings.Split(fmt.Sprintf(swanctl_cmd_listsa, id), " ")
	if err, ret := public.ExecCmdWithRet(swanctl_cmd, para...); err != nil {
		agentLog.AgentLogger.Error("swanctl --list-sa exec err: %v", err)
	} else {
		if strings.Contains(ret, id) &&
			strings.Contains(ret, "ESTABLISHED") &&
			strings.Contains(ret, "INSTALLED") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
ping check
*/
func checkPingStatus(vap *VapStatusCtl, timeout int, chs chan DelayResult) {
	t_start := time.Now()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vapId := vap.Id
	targetIP := vap.Conf.Target
	if strings.Contains(targetIP, "/") {
		targetIP = strings.Split(targetIP, "/")[0]
	}

	if vap.Conf.PingType == PingType_PingInNs {
		if err := network.SwitchNS(vap.Namespace); err != nil {
			agentLog.AgentLogger.Debug(fmt.Sprintf("Switch ns to %v err: %v", vapId, err))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		}
		defer network.SwitchOriginNS()
	}

	listenAddr := vap.Conf.Source
	if strings.Contains(listenAddr, "/") {
		listenAddr = strings.Split(listenAddr, "/")[0]
	}

	c, err := icmp.ListenPacket("ip4:icmp", listenAddr)
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("icmp listen for %v err: %v", vapId, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Millisecond))

	rand.Seed(time.Now().UnixNano())
	seq := rand.Intn(1000)

	echoMsg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: seq,
			Data: []byte("HELLO-R-U-THERE"),
		},
	}

	emsg, err := echoMsg.Marshal(nil)
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("marshal icmp message for %v err: %v", vapId, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}

	if _, err := c.WriteTo(emsg, &net.IPAddr{IP: net.ParseIP(targetIP)}); err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("write icmp request for %v to %v err: %v", vapId, targetIP, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}

	rb := make([]byte, 1500)
	timeOutTimer := time.NewTimer(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-timeOutTimer.C:
			//agentLog.AgentLogger.Debug(fmt.Sprintf("read icmp request for %v from %v timeout", vapId, targetIP))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		default:
		}

		n, peer, err := c.ReadFrom(rb)
		if err != nil {

			//agentLog.AgentLogger.Debug(fmt.Sprintf("read icmp reply for %v from %v err: %v\n", vapId, targetIP, err))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		}
		t_end := time.Now()

		rMsg, err := icmp.ParseMessage(ProtocolICMP, rb[:n])
		if err != nil {
			// log.Println(err, vapId, targetIP)
			// log.Error("parse icmp reply for %v from %v err: %v", vapId, targetIP, err)
			// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			// return
			continue
		}

		if 0 == strings.Compare(targetIP, peer.String()) {
			if rMsg.Type == ipv4.ICMPTypeEchoReply {
				reply, ok := (rMsg.Body).(*icmp.Echo)
				if !ok {
					continue
				}
				if reply.Seq != seq {
					continue
				}
				delay := t_end.Sub(t_start).Seconds() * 1000 // ms
				chs <- DelayResult{VapId: vapId, Delay: delay, StartTime: t_start}
				return
			} else {
				// err = errors.New("icmp reply type error")
				// log.Println(err, vapId, targetIP)
				// log.Error("parse icmp reply type for %v from %v err", vapId, targetIP)
				// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
				// return
				continue
			}
		} else {
			// err = errors.New("read from ip diff targetip")
			// log.Println(err, peer.String(), targetIP)
			// log.Error("read from ip diff targetip for %v, peer %v, target %v", vapId, peer, targetIP)
			// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			// return
			continue
		}
	}
}

func checkVapStatus(vap *VapStatusCtl, chs chan DelayResult) {

	switch vap.Conf.Type {
	case VapCheckType_Ipsec:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkIpsecStatus(vap, chs)
		}
	case VapCheckType_Gre:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkGreStatus(vap, chs)
		}
	case VapCheckType_Port:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkPortStatus(vap, chs)
		}
	default:
	}
}

func ageVapStart() {
	for _, v := range G_VapStatusCtl {
		v.AgeFlag = true
	}
}

func ageVapEnd() bool {
	aged := false
	//log.Info("age end.")
	for vapId, vapS := range G_VapStatusCtl {
		if vapS.AgeFlag {
			updateStatusToFile(public.VAPOFFLINE, monitorpath+vapId)
			delete(G_VapStatusCtl, vapId)
			log.Info("delete vap %v.", vapId)
			aged = true
		}
	}
	return aged
}

func updateVapStatus(resultList []DelayResult) {
	for _, ins := range resultList {
		if _, ok := G_VapStatusCtl[ins.VapId]; ok {
			vapStatusCtl := G_VapStatusCtl[ins.VapId]
			///agentLog.AgentLogger.Info("updateVapStatus: VapId: ", ins.VapId, " Delay:", ins.Delay, " Status:", vapStatusCtl.Status, " Type:", vapStatusCtl.Conf.Type, " PingType:", vapStatusCtl.Conf.PingType)
			updateStatusNoCheck(&ins, vapStatusCtl)
		}
	}
}

func changeEnd() {
	/*
		for _, vapS := range G_VapStatusCtl {
			vapS.StatusChg = false
		}
	*/

	if G_SmoothFlag {
		G_SmoothFlag = false
	}
}

func updateStatus2InfluxDbAndCore() (error, bool) {

	var chg = false
	var url string

	for _, vapS := range G_VapStatusCtl {
		if vapS.StatusChg {
			chg = true

			var vapStatusToCore VapStatusToCore
			vapStatusToCore.Tsms = vapS.ChgTime.UnixNano() //fmt.Sprintf("%d", vapS.ChgTime.UnixNano())
			if vapS.Status == public.VAPNORMAL {
				if vapS.Conf.ObjType == VapObjType_Ipsec {
					vapStatusToCore.Status = "Established"
				} else {
					vapStatusToCore.Status = "UP"
				}
			} else {
				if vapS.Conf.ObjType == VapObjType_Ipsec {
					vapStatusToCore.Status = "Idle"
				} else {
					vapStatusToCore.Status = "DOWN"
				}
			}

			bytedata, err := json.Marshal(vapStatusToCore)
			agentLog.AgentLogger.Info("Request Core data, ObjType:", vapS.Conf.ObjType, ", Id:", vapS.Id, ", Info:", string(bytedata[:]))
			if err != nil {
				agentLog.AgentLogger.Error("Marshal post edge err:", err)
				return errors.New("Marshal post edge msg err:" + err.Error()), false
			}

			switch vapS.Conf.ObjType {
			case VapObjType_Port:
				url = fmt.Sprintf("/api/dpConfig/pops/%s/logicPorts/%s/status", public.G_coreConf.Sn, vapS.Id)
			case VapObjType_Vap:
				url = fmt.Sprintf("/api/dpConfig/vaps/%s/status", vapS.Id)
			case VapObjType_Link:
				url = fmt.Sprintf("/api/dpConfig/links/%s/status", vapS.Id)
			case VapObjType_Conn:
				url = fmt.Sprintf("/api/dpConfig/connections/%s/status", vapS.Id)
			case VapObjType_Tunn:
				url = fmt.Sprintf("/api/dpConfig/tunnels/%s/status", vapS.Id)
			case VapObjType_Ipsec:
				url = fmt.Sprintf("/api/dpConfig/connections/%s/status", vapS.Id) /// connections/%s/ipsecSa/status
			case VapObjType_Nat:
				url = fmt.Sprintf("/api/dpConfig/nats/%s/status", vapS.Id)
			default:
				url = ""
			}

			if url != "" {
				respBody, err := public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
				if err != nil {
					///log.Warning("Update Status requestCore error :", err, ", url:", url)
					///return errors.New("RequestCore error: " + err.Error()), false
					agentLog.AgentLogger.Info("Request Core Error, ", vapS.Conf.ObjType, ", Id:", vapS.Id)
				} else {
					/* 转化成json */
					conf := &RespConfig{}
					if err := json.Unmarshal(respBody, conf); err == nil {
						if conf.Success && conf.Ret == 0 {
							vapS.StatusChg = false
							agentLog.AgentLogger.Info("Request Core Success, ", vapS.Conf.ObjType, ", Id:", vapS.Id)
						}
					}
				}
			} else {
				vapS.StatusChg = false
			}
		}
	}

	if chg {
		changeEnd()
		return nil, true
	}

	return nil, false
}

func setVapStatus2Etcd() error {
	vapNews := make([]VapStatus, 0)
	for _, vapSCtl := range G_VapStatusCtl {
		vapNews = append(vapNews, vapSCtl.VapStatus)
	}

	//save vap status to etcd
	byteData, err := json.Marshal(vapNews)
	if err != nil {
		return err
	}

	err = EtcdClient.SetValue(config.MoniStatusPath, string(byteData[:]))
	if err != nil {
		return err
	}
	return nil
}

func getEtcdInfoMap(etcdClient *etcd.Client) (map[string]*VapStatusCtl, error) {

	etcdVapMap := make(map[string]*VapStatusCtl)
	timeStr := fmt.Sprintf("%d", time.Now().UnixNano())

	/* port */
	paths := []string{config.PortConfPath}
	ports, _ := etcdClient.GetValues(paths)
	for _, v := range ports {
		ins := &app.PortConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.HealthCheck {
				pingType = PingType_PingOutNs
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: "", Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Port, ins.RemoteAddress, ins.LocalAddress},
					0, false, false, time.Now()}
			} else {
				/* 如果不开启健康检查，则Namespace 填充port的LogicPortName */
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.LogicPortName, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Port, ins.RemoteAddress, ins.LocalAddress},
					0, false, false, time.Now()}
			}
		}
	}

	/* vap */
	paths = []string{config.VapConfPath}
	vaps, _ := etcdClient.GetValues(paths)
	for _, v := range vaps {
		ins := &app.VapConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Type == app.VapType_Eport {
				if ins.EportInfo.HealthCheck {
					pingType = PingType_PingOutNs
					etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.EportInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
						VapCheckConf{VapCheckType_Port, pingType, VapObjType_Vap, ins.EportInfo.RemoteAddress, ins.EportInfo.LocalAddress},
						0, false, false, time.Now()}
				} else {
					/* 如果不开启健康检查，则Namespace填充eport的Name */
					etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.EportInfo.Name, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
						VapCheckConf{VapCheckType_Port, pingType, VapObjType_Vap, ins.EportInfo.RemoteAddress, ins.EportInfo.LocalAddress},
						0, false, false, time.Now()}
				}
			} else if ins.Type == app.VapType_Gre {
				if ins.GreInfo.HealthCheck {
					pingType = PingType_PingOutNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.GreInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Vap, ins.GreInfo.RemoteAddress, ins.GreInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.VapType_Ipsec {
				if ins.IpsecInfo.HealthCheck {
					pingType = PingType_PingOutNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.IpsecInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Ipsec, pingType, VapObjType_Vap, ins.IpsecInfo.RemoteAddress, ins.IpsecInfo.LocalAddress},
					0, false, false, time.Now()}
			}
		}
	}

	/* linkEndp */
	paths = []string{config.LinkEndpConfPath}
	linkEndps, _ := etcdClient.GetValues(paths)
	for _, v := range linkEndps {
		ins := &app.LinkEndpConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.HealthCheck {
				pingType = PingType_PingOutNs
			}
			etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: "", Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
				VapCheckConf{VapCheckType_Port, pingType, VapObjType_Link, ins.RemoteAddress, ins.LocalAddress},
				0, false, false, time.Now()}
		}
	}

	/* vplEndp */
	paths = []string{config.VplEndpConfPath}
	vplEndps, _ := etcdClient.GetValues(paths)
	for _, v := range vplEndps {
		ins := &app.VplEndpConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.LinkId == "" {
				if ins.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.Id, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Link, ins.RemoteAddress, ins.LocalAddress},
					0, false, false, time.Now()}
			} else {
				//联动Link状态
				vapStatus, ok := etcdVapMap[ins.LinkId]
				if ok {
					//如果已经存在，则同步配置
					if ins.HealthCheck {
						pingType = PingType_PingOutNs
					}
					etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: "", Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
						VapCheckConf{VapCheckType_Port, pingType, VapObjType_Link, vapStatus.Conf.Target, vapStatus.Conf.Source},
						0, false, false, time.Now()}
				}
			}
		}
	}

	/* nat */
	paths = []string{config.NatConfPath}
	nats, _ := etcdClient.GetValues(paths)
	for _, v := range nats {
		ins := &app.NatConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Type == app.NatType_Line && ins.PortId == "" {
				//Eport
				if ins.EportInfo.HealthCheck {
					pingType = PingType_PingOutNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.NatName, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Nat, ins.EportInfo.RemoteAddress, ins.EportInfo.LocalAddress},
					0, false, false, time.Now()}
			}
		}
	}

	/* conn */
	paths = []string{config.ConnConfPath}
	conns, _ := etcdClient.GetValues(paths)
	for _, v := range conns {
		ins := &app.ConnConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Type == app.ConnType_Eport {
				if ins.EportInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.EportInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Conn, ins.EportInfo.RemoteAddress, ins.EportInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Gre {
				if ins.GreInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.GreInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Conn, ins.GreInfo.RemoteAddress, ins.GreInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Ipsec {
				if ins.IpsecInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.IpsecInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Ipsec, pingType, VapObjType_Conn, ins.IpsecInfo.RemoteAddress, ins.IpsecInfo.LocalAddress},
					0, false, false, time.Now()}

				/* 增加ipsecSa检查条目 */
				etcdVapMap[ins.Id+"/ipsecSa"] = &VapStatusCtl{VapStatus{Namespace: ins.IpsecInfo.EdgeId, Id: ins.Id + "/ipsecSa", Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Ipsec, PingType_NoPing, VapObjType_Ipsec, ins.IpsecInfo.RemoteAddress, ins.IpsecInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Nat {
				if ins.NatgwInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.NatgwInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Conn, ins.NatgwInfo.RemoteAddress, ins.NatgwInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Nats {
				if ins.NatgwsInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.NatgwsInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Conn, ins.NatgwsInfo.RemoteAddress, ins.NatgwsInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Ssl {
				if ins.SslInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.SslInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Conn, ins.SslInfo.RemoteAddress, ins.SslInfo.LocalAddress},
					0, false, false, time.Now()}
			}

		}
	}

	/* tunnel */
	paths = []string{config.TunnelConfPath}
	tunns, _ := etcdClient.GetValues(paths)
	for _, v := range tunns {
		ins := &app.TunnelConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Type == app.TunnelType_Veth {
				if ins.VethInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.VethInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Port, pingType, VapObjType_Tunn, ins.VethInfo.RemoteAddress, ins.VethInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.TunnelType_Gre {
				if ins.GreInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.GreInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Tunn, ins.GreInfo.RemoteAddress, ins.GreInfo.LocalAddress},
					0, false, false, time.Now()}
			} else if ins.Type == app.TunnelType_Vpl {
				if ins.GreInfo.HealthCheck {
					pingType = PingType_PingInNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.GreInfo.EdgeId, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Tunn, ins.GreInfo.RemoteAddress, ins.GreInfo.LocalAddress},
					0, false, false, time.Now()}
			}
		}
	}

	return etcdVapMap, nil
}

func writeTimestampToFile(filePath string) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	return os.WriteFile(filePath, []byte(timestamp), 0644) // 使用0644权限覆盖文件内容
}

func startStatusDetect() {

	// setupdate
	writeTimestampToFile(timestampFilepath)

	// 从etch获取最新配置
	etcdVapMap, _ := getEtcdInfoMap(EtcdClient)

	// 老化开始，置老化标记
	ageVapStart()

	num := len(etcdVapMap)
	chs := make(chan DelayResult)
	resultList := make([]DelayResult, num)
	for _, vap := range etcdVapMap {
		vapStatus, ok := G_VapStatusCtl[vap.Id]
		if !ok {
			G_VapStatusCtl[vap.Id] = vap
		} else {
			//如果已经存在，则同步配置
			G_VapStatusCtl[vap.Id].Conf = vap.Conf
			G_VapStatusCtl[vap.Id].Namespace = vap.Namespace
		}
		G_VapStatusCtl[vap.Id].AgeFlag = false

		// 进程重启，所有抑制状态的vap状态重新上报
		if G_SmoothFlag && ok && vapStatus.Status != public.VAPUNKNOWN {
			G_VapStatusCtl[vap.Id].StatusChg = true
		}

		go checkVapStatus(vap, chs)
	}

	// 老化结束，没有更新标记的全部老化
	ageChg := ageVapEnd()

	// 抓取返回结果
	for i := 0; i < num; i++ {
		res := <-chs
		resultList[i] = res
	}

	// 结果处理
	updateVapStatus(resultList)
	err, chg := updateStatus2InfluxDbAndCore()
	if err == nil && (ageChg || chg) {
		err := setVapStatus2Etcd()
		if err != nil {
			agentLog.AgentLogger.Debug("set vap status to etcd err: %v", err)
		}
	}
}

func getVapHistoryStatus(etcdClient *etcd.Client) error {

	/* Get history status */
	vapStatus := make([]VapStatus, 0)
	etcdStatus, err := etcdClient.GetValue(config.MoniStatusPath)
	if err != nil {
		errCode := err.Error()
		if !strings.EqualFold("100", strings.Split(errCode, ":")[0]) {
			return err
		}
	} else {
		if err := json.Unmarshal([]byte(etcdStatus), &vapStatus); err != nil {
			return err
		}

		for _, v := range vapStatus {
			vapctl := &VapStatusCtl{VapStatus{Namespace: v.Namespace, Id: v.Id, Status: v.Status, Tsms: v.Tsms}, VapCheckConf{0, 0, 0, "", ""}, 0, false, false, time.Now()}
			G_VapStatusCtl[v.Id] = vapctl
		}
	}

	return nil
}

func bootInit() error {

	agentLog.Init(logName)
	network.Init()
	if err := public.PopConfigInit(); err != nil {
		agentLog.AgentLogger.Error("ConfigInit fail err: ", err)
		return err
	}

	return nil
}

func flagInit() error {
	logLevel := flag.Int("l", 2, "setLogLevel 0:debug 1:info 2:warning 3:error")
	flag.Parse()
	if err := agentLog.SetLevel(*logLevel); err != nil {
		return err
	}

	return nil
}

func main() {

	var err error

	if err := flagInit(); err != nil {
		agentLog.AgentLogger.Info("flagInit err:", err)
		return
	}
	err = bootInit()
	if err != nil {
		agentLog.AgentLogger.Info("bootInit err:", err)
		os.Exit(1)
	} else {
		agentLog.AgentLogger.Info("init monitor success.")
	}

	//save monitor status
	public.MakeDir(monitorpath)

	ips := make([]string, 1)
	ips[0] = "http://" + EtcdServer
	EtcdClient, err = etcd.NewEtcdClient(ips, "", "", "", false, "", "")
	if err != nil {
		agentLog.AgentLogger.Error("Etcd Client init failed.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = getVapHistoryStatus(EtcdClient)
	if err != nil {
		agentLog.AgentLogger.Debug("get vap history status err:", err)
	}

	// 2s timer
	tick := time.NewTicker(time.Second * time.Duration(VapDetectInterval))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			startStatusDetect()
		}
	}
}
