package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"fmt"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

type NatshareConf struct {
	Id           string `json:"id"`           //(必填)共享Nat网关ID
	NatId        string `json:"natId"`        //[继承]Nat Id
	Bandwidth    int    `json:"bandwidth"`    //[继承]共享带宽限速，单位Mbps，默认0
	LocalAddress string `json:"localAddress"` //(必填)Natgw本端地址
	Nexthop      string `json:"nexthop"`      //(必填)Natgw网关地址
	NexthopMac   string `json:"nexthopMac"`   //(必填)Natgw网关Mac地址
	MacBind      bool   `json:"macBind"`      //{不填} 如果网关地址不在本端地址范围内，则需要绑定Mac
	NatshareName string `json:"natshareName"` //{不填}
}

func (conf *NatshareConf) Create(action int) error {

	var err error

	/* 设置Name */
	conf.NatshareName = conf.Id

	/* 判断网关地址是否在本端地址范围内 */
	conf.MacBind = false
	localcidr := public.GetCidrIpRange(conf.LocalAddress)
	nexthopcidr := public.GetCidrIpRange(conf.Nexthop + "/" + strings.Split(conf.LocalAddress, "/")[1])
	if localcidr != nexthopcidr {
		if conf.NexthopMac == "" {
			return derr.Error{In: err.Error(), Out: "NoNexthopMacError"}
		}
		conf.MacBind = true
	}

	/* get nat device */
	err, device := GetNatDeviceById(conf.NatId)
	if err != nil {
		return err
	}

	/* create ipvlan l2 interface */
	err = public.CreateInterfaceTypeIpvlanL2(conf.Id, device)
	if err != nil {
		return err
	}

	/* 创建ns */
	cmdstr := fmt.Sprintf("ip netns add %s", conf.Id)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* 配置转发开发 */
	cmdstr = fmt.Sprintf("ip netns exec %s sysctl -w net.ipv4.ip_forward=1", conf.Id)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* 设置TCP MSS  */
	cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1300", conf.Id)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* set port to netns */
	err = public.SetInterfaceNetns(conf.Id, conf.Id)
	if err != nil {
		return err
	}

	/* set link up */
	err = public.VrfSetInterfaceLinkUp(conf.Id, conf.Id)
	if err != nil {
		return err
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(false, conf.Id, conf.Id, conf.LocalAddress)
	if err != nil {
		return err
	}

	if conf.MacBind {
		/* bind nexthop arp */
		cmdstr = fmt.Sprintf("ip netns exec %s ip route add %s/32 dev %s", conf.Id, conf.Nexthop, conf.Id)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		cmdstr = fmt.Sprintf("ip netns exec %s arp -s %s %s", conf.Id, conf.Nexthop, conf.NexthopMac)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			//return err
		}
	}

	/* set tc limit */
	err = public.VrfSetInterfaceIngressLimit(conf.Id, conf.Id, conf.Bandwidth)
	if err != nil {
		return err
	}

	/* set iptables snat */
	err = public.VrfSetInterfaceSnat(false, conf.Id, conf.Id)
	if err != nil {
		return err
	}

	if action == public.ACTION_ADD {
		/* (FRR) add default route */
		if conf.MacBind {
			err = VrfAddRouteOnlink(false, conf.LocalAddress, "0.0.0.0/0", conf.Nexthop, conf.Id, conf.Id)
		} else {
			err = VrfAddRoute(false, conf.LocalAddress, "0.0.0.0/0", conf.Nexthop, conf.Id)
		}
		if err != nil {
			return err
		}
	}

	/* set lo link up */
	err = public.VrfSetInterfaceLinkUp(conf.Id, "lo")
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *NatshareConf) Modify(cfgNew *NatshareConf) (error, bool) {

	var err error
	chg := false

	/* 判断新网关地址是否在新本端地址范围内 */
	cfgNew.MacBind = false
	localcidr := public.GetCidrIpRange(cfgNew.LocalAddress)
	nexthopcidr := public.GetCidrIpRange(cfgCur.Nexthop + "/" + strings.Split(cfgNew.LocalAddress, "/")[1])
	if localcidr != nexthopcidr {
		if cfgNew.NexthopMac == "" {
			return derr.Error{In: err.Error(), Out: "NoNexthopMacError"}, false
		}
		cfgNew.MacBind = true
	}

	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		err = public.VrfSetInterfaceIngressLimit(cfgCur.Id, cfgCur.Id, cfgNew.Bandwidth)
		if err != nil {
			return err, false
		}
		cfgCur.Bandwidth = cfgNew.Bandwidth
		chg = true
	}

	if cfgCur.LocalAddress != cfgNew.LocalAddress {
		/* 先判断地址Bind是否变化 */
		if cfgCur.MacBind != cfgNew.MacBind {
			if cfgCur.MacBind {
				/* unbind nexthop arp */
				cmdstr := fmt.Sprintf("ip netns exec %s ip route del %s/32 dev %s", cfgCur.Id, cfgCur.Nexthop, cfgCur.Id)
				err, _ = public.ExecBashCmdWithRet(cmdstr)
				if err != nil {
					agentLog.AgentLogger.Error(cmdstr, err)
					return err, false
				}
				err = VrfAddRouteOnlink(true, cfgCur.LocalAddress, "0.0.0.0/0", cfgCur.Nexthop, cfgCur.Id, cfgCur.Id)
				if err != nil {
					return err, false
				}
				err = VrfAddRoute(false, cfgNew.LocalAddress, "0.0.0.0/0", cfgCur.Nexthop, cfgCur.Id)
				if err != nil {
					return err, false
				}
			} else {
				/* bind nexthop arp */
				cmdstr := fmt.Sprintf("ip netns exec %s ip route add %s/32 dev %s", cfgCur.Id, cfgCur.Nexthop, cfgCur.Id)
				err, _ = public.ExecBashCmdWithRet(cmdstr)
				if err != nil {
					agentLog.AgentLogger.Error(cmdstr, err)
					return err, false
				}
				cmdstr = fmt.Sprintf("ip netns exec %s arp -s %s %s", cfgCur.Id, cfgCur.Nexthop, cfgNew.NexthopMac)
				err, _ = public.ExecBashCmdWithRet(cmdstr)
				if err != nil {
					agentLog.AgentLogger.Info(cmdstr, err)
					//return err
				}
				err = VrfAddRoute(true, cfgCur.LocalAddress, "0.0.0.0/0", cfgCur.Nexthop, cfgCur.Id)
				if err != nil {
					return err, false
				}
				err = VrfAddRouteOnlink(false, cfgNew.LocalAddress, "0.0.0.0/0", cfgCur.Nexthop, cfgCur.Id, cfgCur.Id)
				if err != nil {
					return err, false
				}
			}

			cfgCur.MacBind = cfgNew.MacBind
			cfgCur.NexthopMac = cfgNew.NexthopMac
		}

		err = public.VrfSetInterfaceAddress(false, cfgCur.Id, cfgCur.Id, cfgNew.LocalAddress)
		if err != nil {
			return err, false
		}
		err = public.VrfSetInterfaceAddress(true, cfgCur.Id, cfgCur.Id, cfgCur.LocalAddress)
		if err != nil {
			return err, false
		}

		cfgCur.LocalAddress = cfgNew.LocalAddress
		chg = true
	}

	return nil, chg
}

func (conf *NatshareConf) Destroy() error {

	var err error

	if !public.NsExist(conf.Id) {
		return errors.New("VrfNotExist")
	}

	/* (FRR) delete default route */
	if conf.MacBind {
		err = VrfAddRouteOnlink(true, conf.LocalAddress, "0.0.0.0/0", conf.Nexthop, conf.Id, conf.Id)
	} else {
		err = VrfAddRoute(true, conf.LocalAddress, "0.0.0.0/0", conf.Nexthop, conf.Id)
	}
	if err != nil {
		return err
	}

	if conf.MacBind {
		/* unbind nexthop arp */
		cmdstr := fmt.Sprintf("ip netns exec %s ip route del %s/32 dev %s", conf.Id, conf.Nexthop, conf.Id)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Error(cmdstr, err)
			return err
		}
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(true, conf.Id, conf.Id, conf.LocalAddress)
	if err != nil {
		return err
	}

	/* set iptables snat */
	err = public.VrfSetInterfaceSnat(true, conf.Id, conf.Id)
	if err != nil {
		return err
	}

	/* destroy interface */
	err = public.VrfDeleteInterface(conf.Id, conf.Id)
	if err != nil {
		return err
	}

	/* destroy ns */
	cmdstr := fmt.Sprintf("ip netns del %s", conf.Id)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Error(cmdstr, err)
		return err
	}

	return nil
}
