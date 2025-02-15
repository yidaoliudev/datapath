package app

import (
	"datapath/agentLog"
	"datapath/public"
	"strings"

	"gitlab.daho.tech/gdaho/network"
	"gitlab.daho.tech/gdaho/util/derr"
)

/*
描述： 定义一个扩展Port实例，用于Vap或者Connection.
*/
type EportConf struct {
	Name          string `json:"name"`          //[继承]eport名称
	EdgeId        string `json:"edgeId"`        //[继承]如果EdgeId非空，则表示Edge内eport
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	PhyifName     string `json:"phyifName"`     //(必填)物理接口的逻辑名称，例如：wan0，lan1等，不可能是Mac地址
	VlanId        int    `json:"vlanId"`        //[选填]如果是vlanif，则必填，默认为0
	LocalAddress  string `json:"localAddress"`  //(必填)本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)对端地址
	Nexthop       string `json:"nexthop"`       //(必填)接口网关，必须与LocalAddress在同地址段内
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
	Device        string `json:"device"`        //{不填}记录物理接口的逻辑名称
}

func (conf *EportConf) Create(action int) error {

	var err error

	if conf.VlanId == 0 {
		/* 如果不是wan接口，则在rename之前set down. */
		if action == public.ACTION_ADD && strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) != 0 {
			err = public.SetInterfaceLinkDown(conf.PhyifName)
			if err != nil {
				return err
			}
		}

		/* rename interface */
		conf.Device = conf.PhyifName
		err = public.RenameInterface(conf.PhyifName, conf.Name)
		if err != nil {
			return err
		}
	} else {
		/* create vlan port */
		err := public.CreateInterfaceTypeVlanif(conf.PhyifName, conf.Name, conf.VlanId)
		if err != nil {
			return err
		}
	}

	if conf.EdgeId == "" {
		/* set link up */
		err = public.SetInterfaceLinkUp(conf.Name)
		if err != nil {
			return err
		}

		if action == public.ACTION_ADD && conf.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			if conf.Nexthop == "" {
				err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			} else {
				err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop)
			}
			if err != nil {
				return err
			}
		}

		if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) == 0 && conf.VlanId == 0 {
			/* 如果是wan的物理接口创建port对象，则直接反馈成功。比较时忽略大小写。 */
			return nil
		}

		/* flush PhyVap address */
		if err = network.AddrFlush(conf.Name); err != nil {
			agentLog.AgentLogger.Info("AddrFlush error ", conf.Name)
			return derr.Error{In: err.Error(), Out: "AddrFlush eport Error"}
		}

		/* set ip address */
		if conf.LocalAddress != "" {
			err = public.SetInterfaceAddress(false, conf.Name, conf.LocalAddress)
			if err != nil {
				return err
			}
		}

		/* set tc limit */
		err = public.SetInterfaceIngressLimit(conf.Name, conf.Bandwidth)
		if err != nil {
			return err
		}
	} else {
		/* set gre to Netns*/
		err = public.SetInterfaceNetns(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}

		/* set link up */
		err = public.VrfSetInterfaceLinkUp(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}

		if action == public.ACTION_ADD && conf.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			if conf.Nexthop == "" {
				err = VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			} else {
				err = VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
			}
			if err != nil {
				return err
			}
		}

		if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) == 0 && conf.VlanId == 0 {
			/* 如果是wan的物理接口创建port对象，则直接反馈成功。比较时忽略大小写。 */
			return nil
		}

		/* set ip address */
		if conf.LocalAddress != "" {
			err = public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.Name, conf.LocalAddress)
			if err != nil {
				return err
			}
		}

		/* set tc limit */
		err = public.VrfSetInterfaceIngressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cfgCur *EportConf) Modify(cfgNew *EportConf) (error, bool) {

	var chg = false
	var err error

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
		chg = true
	}

	if cfgCur.EdgeId == "" {
		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			err = public.SetInterfaceIngressLimit(cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return err, false
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress {
			/* delete old ip address */
			err = public.SetInterfaceAddress(true, cfgCur.Name, cfgCur.LocalAddress)
			if err != nil {
				return err, false
			}

			/* add new ip address */
			err = public.SetInterfaceAddress(false, cfgCur.Name, cfgNew.LocalAddress)
			if err != nil {
				return err, false
			}

			cfgCur.LocalAddress = cfgNew.LocalAddress
			chg = true
		}

		if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
			if cfgCur.RemoteAddress != "" {
				find := false
				/* 如果是vap，需要检查vap关联linkendp实例的RemoteAddress地址是否和vap旧的RemoteAddress地址相同，如果相同，则不能删除 */
				if strings.Contains(cfgCur.Name, "vap") {
					find = CheckLinkEndpRemoteAddrExist(cfgCur.Name, cfgCur.RemoteAddress)
				}
				if !find {
					/* (FRR) delete old RemoteAddress route */
					if cfgCur.Nexthop == "" {
						err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name)
					} else {
						err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Nexthop)
					}
					if err != nil {
						return derr.Error{In: err.Error(), Out: "EportModifyError"}, false
					}
				}
			}

			if cfgNew.RemoteAddress != "" {
				/* (FRR) add RemoteAddress route */
				if cfgCur.Nexthop == "" {
					err = AddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgCur.Name)
				} else {
					err = AddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgCur.Nexthop)
				}
				if err != nil {
					return derr.Error{In: err.Error(), Out: "EportModifyError"}, false
				}
			}

			cfgCur.RemoteAddress = cfgNew.RemoteAddress
			chg = true
		}
	} else {
		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			err = public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return err, false
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			chg = true
		}

		if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
			if cfgCur.RemoteAddress != "" {
				/* (FRR) delete old RemoteAddress route */
				if cfgCur.Nexthop == "" {
					err = VrfAddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name, cfgCur.EdgeId)
				} else {
					err = VrfAddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
				}
				if err != nil {
					return derr.Error{In: err.Error(), Out: "EportModifyError"}, false
				}
			}

			if cfgNew.RemoteAddress != "" {
				/* (FRR) add RemoteAddress route */
				if cfgCur.Nexthop == "" {
					err = VrfAddRoute(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.Name, cfgCur.EdgeId)
				} else {
					err = VrfAddRoute(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
				}
				if err != nil {
					return derr.Error{In: err.Error(), Out: "EportModifyError"}, false
				}
			}

			/* LocalAddress和Nexthop 不能改 */
			cfgCur.RemoteAddress = cfgNew.RemoteAddress
			chg = true
		}
	}

	return nil, chg
}

func (conf *EportConf) Destroy() error {

	var err error

	if conf.EdgeId == "" {
		if conf.RemoteAddress != "" {
			find := false
			/* 如果是vap，需要检查vap关联linkendp实例的RemoteAddress地址是否和vap旧的RemoteAddress地址相同，如果相同，则不能删除 */
			if strings.Contains(conf.Name, "vap") {
				find = CheckLinkEndpRemoteAddrExist(conf.Name, conf.RemoteAddress)
			}
			if !find {
				/* (FRR) delete RemoteAddress route */
				if conf.Nexthop == "" {
					err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name)
				} else {
					err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop)
				}
				if err != nil {
					return err
				}
			}
		}
	} else {
		if conf.RemoteAddress != "" {
			/* (FRR) delete RemoteAddress route */
			if conf.Nexthop == "" {
				err = VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			} else {
				err = VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
			}
			if err != nil {
				return err
			}
		}
	}

	if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) == 0 && conf.VlanId == 0 {
		/* 如果是wan的物理接口删除，则直接反馈成功。比较时忽略大小写。 */
		return nil
	}

	if conf.EdgeId == "" {
		err = public.SetInterfaceIngressLimit(conf.Name, 0)
		if err != nil {
			return err
		}

		/* flush port address */
		if err = network.AddrFlush(conf.Name); err != nil {
			agentLog.AgentLogger.Info("AddrFlush error ", conf.Name)
			return derr.Error{In: err.Error(), Out: "Destroy eport Error"}
		}
	} else {
		err = public.VrfSetInterfaceIngressLimit(conf.EdgeId, conf.Name, 0)
		if err != nil {
			return err
		}

		/* set gre to root namespace */
		err = public.SetVrfInterfaceRootNs(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}
	}

	if conf.VlanId != 0 {
		/* destroy vlan port */
		err = public.DeleteInterface(conf.Name)
		if err != nil {
			return err
		}
	} else {
		/* 如果不是wan接口，则在rename之前set down. */
		if strings.Compare(strings.ToLower(conf.PhyifName), strings.ToLower("wan0")) != 0 {
			err = public.SetInterfaceLinkDown(conf.Name)
			if err != nil {
				return err
			}
		}

		/* rename interface */
		err := public.RenameInterface(conf.Name, conf.Device)
		if err != nil {
			return err
		}
	}

	return nil
}
