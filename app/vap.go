package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"encoding/json"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Vap Type: Vap类型 */
	VapType_Eport = 0 /* Eport Vap */
	VapType_Gre   = 1 /* Gre Vap */
	VapType_Ipsec = 2 /* IPsec Vap */
)

/*

概述：vap 作为POP上的Port到network之间的数据通道，
如果是Phy Port 或者 Vlan Port直接与network互联，则独享该Port。
如果是在Phy Port通过VPN与network互联，则共享该Port，并独享VPN。

vap分类：
1，Phy Port 直接互联（例如：aws ec2作为pop时，使用独立的接口作为vap实例）
2，Phy Port 通过Vlan互联（独享Vlan Port）
3，Phy Port 通过VPN互联，支持IPsec 和Gre两种方式。(共享Phy Port，独享VPN Port)

*/

type VapConf struct {
	Id            string    `json:"id"`            //(必填)
	PortId        string    `json:"portId"`        //[选填]portid //限制只能是物理接口，暂不考虑vlan子接口
	Type          int       `json:"type"`          //(必填)eport(0),gre(1),ipsec(2)
	Bandwidth     int       `json:"bandwidth"`     //[选填]带宽限速，单位Mbps，默认0
	EportInfo     EportConf `json:"eportInfo"`     //[选填]如果Type为eport
	GreInfo       GreConf   `json:"greInfo"`       //[选填]如果Type为gre
	IpsecInfo     IpsecConf `json:"ipsecInfo"`     //[选填]如果Type为ipsec
	VapName       string    `json:"vapName"`       //{不填}
	VapNexthop    string    `json:"vapNexthop"`    //{不填}
	LocalAddress  string    `json:"localAddress"`  //{不填}
	RemoteAddress string    `json:"remoteAddress"` //{不填}
}

func (conf *VapConf) Create(action int) error {

	var err error

	/* set VapName */
	conf.VapName = conf.Id

	switch conf.Type {
	case VapType_Eport:
		/* create port */
		eport := &conf.EportInfo
		eport.Name = conf.VapName
		/* vap 不限速 */
		eport.Bandwidth = 0
		eport.EdgeId = ""
		err = eport.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap create eport fail: ", eport.PhyifName, ", vlanId:", eport.VlanId)
			return err
		}
		/* set LocalAddress */
		conf.LocalAddress = strings.Split(eport.LocalAddress, "/")[0]
		conf.RemoteAddress = eport.RemoteAddress
		/* set VapNexthop */
		if eport.Nexthop != "" {
			conf.VapNexthop = eport.Nexthop
		} else {
			conf.VapNexthop = eport.Name
		}
	case VapType_Gre:
		/* 获取Vap对应的Port信息 */
		err, port := GetPortInfoById(conf.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap get Port info fail: ", conf.PortId)
			return err
		}
		/* create gre port */
		gre := &conf.GreInfo
		gre.Name = conf.VapName
		/* vap 不限速 */
		gre.Bandwidth = 0
		gre.EdgeId = ""
		/* 设置vap关联的物理口LocalAddress  */
		gre.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		err = gre.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap create gre fail: ", port.LogicPortName, ", src:", conf.GreInfo.TunnelSrc, ", dst:", conf.GreInfo.TunnelDst)
			return err
		}
		/* set LocalAddress */
		conf.LocalAddress = strings.Split(gre.LocalAddress, "/")[0]
		conf.RemoteAddress = gre.RemoteAddress
		/* set VapNexthop */
		conf.VapNexthop = gre.Name
	case VapType_Ipsec:
		/* 获取Vap对应的Port信息 */
		err, port := GetPortInfoById(conf.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap get Port info fail: ", conf.PortId)
			return err
		}
		/* Creare ipsec port */
		ipsec := &conf.IpsecInfo
		ipsec.Name = conf.VapName
		/* vap 不限速 */
		ipsec.Bandwidth = 0
		ipsec.EdgeId = ""
		/* 设置vap关联的物理口LocalAddress  */
		ipsec.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		err = ipsec.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap create ipsec fail: ", port.LogicPortName, ", src:", conf.IpsecInfo.TunnelSrc, ", dst:", conf.IpsecInfo.TunnelDst)
			return err
		}
		/* set LocalAddress */
		conf.LocalAddress = strings.Split(ipsec.LocalAddress, "/")[0]
		conf.RemoteAddress = ipsec.RemoteAddress
		/* set VapNexthop */
		conf.VapNexthop = ipsec.Name
	default:
		return derr.Error{In: err.Error(), Out: "VapTypeError"}
	}

	return nil
}

func (cfgCur *VapConf) Modify(cfgNew *VapConf) (error, bool) {

	var chg = false
	var err error

	switch cfgCur.Type {
	case VapType_Eport:
		/* Modify eport vap */
		eport := &cfgCur.EportInfo
		eportNew := &cfgNew.EportInfo
		eportNew.Name = eport.Name
		/* vap 不限速 */
		eportNew.Bandwidth = 0
		eportNew.EdgeId = ""
		err, chg = eport.Modify(eportNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap modify eport fail: ", cfgCur.VapName)
			return derr.Error{In: err.Error(), Out: "VapModifyError"}, false
		}
		/* set LocalAddress */
		cfgCur.LocalAddress = strings.Split(eport.LocalAddress, "/")[0]
		cfgCur.RemoteAddress = eport.RemoteAddress
	case VapType_Gre:
		/* 获取Vap对应的Port信息 */
		err, port := GetPortInfoById(cfgCur.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap get Port info fail: ", cfgCur.PortId)
			return err, false
		}
		/* Modify gre vap */
		gre := &cfgCur.GreInfo
		greNew := &cfgNew.GreInfo
		greNew.Name = gre.Name
		/* 设置vap关联的物理口LocalAddress  */
		greNew.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		/* vap 不限速 */
		greNew.Bandwidth = 0
		greNew.EdgeId = ""
		err, chg = gre.Modify(greNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap modify gre fail: ", cfgCur.VapName)
			return derr.Error{In: err.Error(), Out: "VapModifyError"}, false
		}
		/* set LocalAddress */
		cfgCur.LocalAddress = strings.Split(gre.LocalAddress, "/")[0]
		cfgCur.RemoteAddress = gre.RemoteAddress
	case VapType_Ipsec:
		/* 获取Vap对应的Port信息 */
		err, port := GetPortInfoById(cfgCur.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap get Port info fail: ", cfgCur.PortId)
			return err, false
		}
		/* Modify ipsec vap */
		ipsec := &cfgCur.IpsecInfo
		ipsecNew := &cfgNew.IpsecInfo
		ipsecNew.Name = ipsec.Name
		/* 设置vap关联的物理口LocalAddress  */
		ipsecNew.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		/* vap 不限速 */
		ipsecNew.Bandwidth = 0
		ipsecNew.EdgeId = ""
		err, chg = ipsec.Modify(ipsecNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap modify ipsec fail: ", cfgCur.VapName)
			return derr.Error{In: err.Error(), Out: "VapModifyError"}, false
		}
		/* set LocalAddress */
		cfgCur.LocalAddress = strings.Split(ipsec.LocalAddress, "/")[0]
		cfgCur.RemoteAddress = ipsec.RemoteAddress
	default:
		return derr.Error{In: err.Error(), Out: "VapTypeError"}, false
	}

	return nil, chg
}

func (conf *VapConf) Destroy() error {

	switch conf.Type {
	case VapType_Eport:
		/* Destroy eport port */
		eport := &conf.EportInfo
		err := eport.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap destroy eport fail: ", conf.VapName)
			return err
		}
	case VapType_Gre:
		/* Destroy gre port */
		gre := &conf.GreInfo
		err := gre.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap destroy gre fail: ", conf.VapName)
			return err
		}
	case VapType_Ipsec:
		/* Destroy ipsec port */
		ipsec := &conf.IpsecInfo
		err := ipsec.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Vap destroy ipsec fail: ", conf.VapName)
			return err
		}
	default:
	}

	return nil
}

func (conf *VapConf) InitIpsecSa() (error, string) {

	var resInfo string
	var err error

	switch conf.Type {
	case VapType_Ipsec:
		/* init ipsec sa */
		ipsec := &conf.IpsecInfo
		err, resInfo = ipsec.InitIpsecSa()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Init vap ipsec sa fail: ", conf.VapName)
			return err, "Init Vap ipsec sa fail"
		}
	default:
		return derr.Error{In: err.Error(), Out: "VapTypeError"}, "VapType Error"
	}

	return nil, resInfo
}

func (conf *VapConf) GetIpsecSa() (error, string) {

	var resInfo string
	var err error

	switch conf.Type {
	case VapType_Ipsec:
		/* get ipsec sa */
		ipsec := &conf.IpsecInfo
		err, resInfo = ipsec.GetIpsecSa()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Get vap ipsec sa fail: ", conf.VapName)
			return err, "Get vap ipsec sa fail"
		}
	default:
		return derr.Error{In: err.Error(), Out: "VapTypeError"}, "VapType Error"
	}

	return nil, resInfo
}

func GetVapInfoById(id string) (error, VapConf) {

	var find = false
	var vap VapConf
	paths := []string{config.VapConfPath}
	vaps, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.VapConfPath not found: ", err.Error())
	} else {
		for _, value := range vaps {
			bytes := []byte(value)
			fp := &VapConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			vap = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "VapNotFound"}, vap
	}

	return nil, vap
}

func CheckVapRemoteAddrExist(vapId, remoteAddress string) bool {

	var find = false
	paths := []string{config.VapConfPath}
	vaps, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.VapConfPath not found: ", err.Error())
	} else {
		for _, value := range vaps {
			bytes := []byte(value)
			fp := &VapConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id == vapId && fp.RemoteAddress == remoteAddress {
				find = true
				break
			}
		}
	}

	return find
}
