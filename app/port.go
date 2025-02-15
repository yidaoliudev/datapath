package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"
	"strconv"
	"strings"

	"gitlab.daho.tech/gdaho/network"
	"gitlab.daho.tech/gdaho/util/derr"
)

const ()

/**
概述：Port 作为构建网络的最底层的实例。
Port分类：
1， Phy Port：可以直接使用porttrans中启用的phyIf。其中wan口不需要定义Port实例，直接可以使用。
2， Vlan Port：基于Phy Port（lan口，暂不支持wan）的vlan ID构建子接口，只支持静态IP方式。

Port用途：
如上所述，通过底层资源，可以构建3种类型的Port实例，它们主要用途有2个：
1， 用于vap实例，有如下几种场景：
   a，基于 Phy Port 创建 Vlan 的vap实例
   b，基于 Phy Port 创建 Phy-VPN 的vap实例
   c，基于 Vlan Port 创建 Vlan-VPN 的vap实例 （暂不考虑）
2， 用于连接实例，有如下几种场景：
   a，基于 Phy Port 创建 Vlan 的connection实例
   b，基于 Phy Port 创建 Phy-VPN 的connection实例
   c，基于 Vlan Port 创建 Vlan-VPN 的connection实例

总结：
1，除Wan以外，需要基于接口创建connection或vap实例的，都必须先创建Port.
2，Port作为系统底层资源，其预留IP range必须为POP作用域内唯一，默认是一段/24地址。
**/

type PortConf struct {
	Id            string `json:"id"`            //(必填)
	PhyifName     string `json:"phyifName"`     //(必填)物理接口的逻辑名称，例如：wan0，lan1等，不可能是Mac地址
	VlanId        int    `json:"vlanId"`        //[选填]如果是vlanif，则必填，默认为0
	LocalAddress  string `json:"localAddress"`  //(必填)地址段必须保证POP域内唯一
	RemoteAddress string `json:"remoteAddress"` //(必填)对端地址
	Nexthop       string `json:"nexthop"`       //(必填)接口网关，必须与LocalAddress在同地址段内
	HealthCheck   bool   `json:"healthCheck"`   //[选填]健康检查，默认false（关闭）
	LogicPortName string `json:"logicPortName"` //{不填}Port接口的逻辑名称，例如：wan0，lan2，lan1.100等
}

func (conf *PortConf) Create(action int) error {

	var err error

	if conf.VlanId == 0 {
		conf.LogicPortName = conf.PhyifName
	} else {
		conf.LogicPortName = conf.PhyifName + "." + strconv.Itoa(conf.VlanId)

		/* create vlan port */
		if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) != 0 {
			/* 如果是wan的物理口创建的接口，则不需要执行 */
			err := public.CreateInterfaceTypeVlanif(conf.PhyifName, conf.LogicPortName, conf.VlanId)
			if err != nil {
				return err
			}
		}
	}

	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add RemoteAddress route */
		if conf.Nexthop == "" {
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.LogicPortName)
		} else {
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop)
		}
		if err != nil {
			return err
		}
	}

	if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) == 0 /* && conf.VlanId == 0 */ {
		/* 如果是wan的物理接口创建port对象，则直接反馈成功。比较时忽略大小写。 */
		return nil
	}

	/* set link up */
	err = public.SetInterfaceLinkUp(conf.LogicPortName)
	if err != nil {
		return err
	}

	/* flush port address */
	if err = network.AddrFlush(conf.LogicPortName); err != nil {
		agentLog.AgentLogger.Info("AddrFlush error ", conf.LogicPortName)
		return derr.Error{In: err.Error(), Out: "Destroy Port Error"}
	}

	/* set ip address */
	if conf.LocalAddress != "" {
		err = public.SetInterfaceAddress(false, conf.LogicPortName, conf.LocalAddress)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cfgCur *PortConf) Modify(cfgNew *PortConf) (error, bool) {

	var chg = false
	var err error

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
		chg = true
	}

	if cfgCur.RemoteAddress != cfgNew.RemoteAddress {

		if cfgCur.RemoteAddress != "" {
			/* (FRR) delete old RemoteAddress route */
			if cfgCur.Nexthop == "" {
				err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.LogicPortName)
			} else {
				err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Nexthop)
			}
			if err != nil {
				return derr.Error{In: err.Error(), Out: "PortModifyError"}, false
			}
		}

		if cfgNew.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			if cfgCur.Nexthop == "" {
				err = AddRoute(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.LogicPortName)
			} else {
				err = AddRoute(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.Nexthop)
			}
			if err != nil {
				return derr.Error{In: err.Error(), Out: "PortModifyError"}, false
			}
		}

		/* LocalAddress和Nexthop 不能改 */
		cfgCur.RemoteAddress = cfgNew.RemoteAddress
		chg = true
	}

	return nil, chg
}

func (conf *PortConf) Destroy() error {

	var err error
	if conf.RemoteAddress != "" {
		/* (FRR) delete RemoteAddress route */
		if conf.Nexthop == "" {
			err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.LogicPortName)
		} else {
			err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop)
		}

		if err != nil {
			return err
		}
	}

	if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) == 0 /* && conf.VlanId == 0 */ {
		/* 如果是wan的物理接口删除，则直接反馈成功。比较时忽略大小写。 */
		return nil
	}

	/* flush port address */
	if err = network.AddrFlush(conf.LogicPortName); err != nil {
		agentLog.AgentLogger.Info("AddrFlush error ", conf.LogicPortName)
		return derr.Error{In: err.Error(), Out: "Destroy Port Error"}
	}

	if conf.VlanId != 0 {
		/* destroy vlan port */
		if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) != 0 {
			/* 如果是wan的物理口创建的接口，则不需要执行 */
			err = public.DeleteInterface(conf.LogicPortName)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func GetPortInfoById(id string) (error, PortConf) {

	var find = false
	var port PortConf
	paths := []string{config.PortConfPath}
	ports, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.PortConfPath not found: ", err.Error())
	} else {
		for _, value := range ports {
			bytes := []byte(value)
			fp := &PortConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			port = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "PortNotFound"}, port
	}

	return nil, port
}
