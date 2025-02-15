package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Nat Type: Nat资源类型 */
	NatType_Line  = 0 /* 直连Nat，根据PortId区分扩展port还是专属port */
	NatType_Vxlan = 1 /* Vxlan Nat */
)

/*
概述：nat 作为POP上租户网络到互联网的出口网关。
*/

type NatConf struct {
	Id            string    `json:"id"`            //(必填)
	Type          int       `json:"type"`          //(必填)Nat类型
	PortId        string    `json:"portId"`        //[选填]专属Port
	EportInfo     EportConf `json:"eportInfo"`     //[选填]扩展Port
	VxlanInfo     VxlanConf `json:"vxlanInfo"`     //[选填]Vxlan Port
	Nexthop       string    `json:"nexthop"`       //(必填)
	HealthCheck   bool      `json:"healthCheck"`   //(必填)
	LocalAddress  string    `json:"localAddress"`  //(必填)
	RemoteAddress string    `json:"remoteAddress"` //(必填)
	NatName       string    `json:"natName"`       //{不填}
}

func (conf *NatConf) Create(action int) error {

	var err error
	conf.NatName = conf.Id

	switch conf.Type {
	case NatType_Line:
		if conf.PortId == "" {
			//扩展port
			eport := &conf.EportInfo
			eport.Name = conf.NatName
			eport.Bandwidth = 0
			eport.EdgeId = ""
			if action == public.ACTION_ADD {
				eport.LocalAddress = conf.LocalAddress
				eport.RemoteAddress = conf.RemoteAddress
				eport.Nexthop = conf.Nexthop
				eport.HealthCheck = conf.HealthCheck
			}
			err := eport.Create(action)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat create eport fail: ", eport.PhyifName, eport.VlanId)
				return err
			}
		} else {
			//专属port
			err, port := GetPortInfoById(conf.PortId)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat get Port info fail: ", conf.PortId)
				return err
			}
			conf.Nexthop = port.Nexthop
			conf.LocalAddress = port.LocalAddress
		}
	case NatType_Vxlan:
		/* 暂时不支持 */
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	return nil
}

func (cfgCur *NatConf) Modify(cfgNew *NatConf) (error, bool) {

	var err error
	chg := false
	switch cfgCur.Type {
	case NatType_Line:
		if cfgCur.PortId == "" {
			//扩展port
			eport := &cfgCur.EportInfo
			eportNew := &cfgNew.EportInfo
			eportNew.Name = cfgNew.NatName
			eportNew.Bandwidth = 0
			eportNew.EdgeId = ""

			/* 因为eport需要加入ovs，无需配置IP */
			eportNew.LocalAddress = cfgNew.LocalAddress
			eportNew.RemoteAddress = cfgNew.RemoteAddress
			eportNew.Nexthop = cfgNew.Nexthop
			eportNew.HealthCheck = cfgNew.HealthCheck
			err, chg = eport.Modify(eportNew)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat modify eport fail: ", eport.PhyifName, eport.VlanId)
				return err, false
			}
		} else {
			//专属port
			err, _ := GetPortInfoById(cfgCur.PortId)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat get Port info fail: ", cfgCur.PortId)
				return err, false
			}
		}
	case NatType_Vxlan:
		/* 暂时不支持 */
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}, false
	}

	return nil, chg
}

func (conf *NatConf) Destroy() error {

	var err error

	switch conf.Type {
	case NatType_Line:
		if conf.PortId == "" {
			//扩展port
			/* delete eport */
			eport := &conf.EportInfo
			err := eport.Destroy()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat destroy eport fail: ", eport.PhyifName, eport.VlanId)
				return err
			}
		} else {
			//专属port
			err, _ := GetPortInfoById(conf.PortId)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Nat get Port info fail: ", conf.PortId)
				return err
			}
		}
	case NatType_Vxlan:
		/* 暂时不支持 */
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}
	return nil
}

func GetNatDeviceById(id string) (error, string) {

	var find = false
	var nat NatConf
	paths := []string{config.NatConfPath}
	nats, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.NatConfPath not found: ", err.Error())
	} else {
		for _, value := range nats {
			bytes := []byte(value)
			fp := &NatConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			nat = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "NatNotFound"}, id
	}

	if nat.PortId != "" {
		err, port := GetPortInfoById(nat.PortId)
		if err != nil {
			return derr.Error{In: err.Error(), Out: "PortNotFound"}, id
		} else {
			return nil, port.PhyifName
		}
	}

	return nil, id
}
