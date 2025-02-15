package app

import (
	"datapath/agentLog"
	"datapath/public"
	"fmt"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

type NatgwConf struct {
	Name          string `json:"name"`          //[继承]Natgw名称
	EdgeId        string `json:"edgeId"`        //[继承]Edge Id
	NatId         string `json:"natId"`         //[继承]Nat Id
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	LocalAddress  string `json:"localAddress"`  //(必填)Natgw本端地址
	Nexthop       string `json:"nexthop"`       //(必填)Natgw网关地址
	NexthopMac    string `json:"nexthopMac"`    //(必填)Natgw网关Mac地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Natgw对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
	MacBind       bool   `json:"macBind"`       //{不填} 如果网关地址不在本端地址范围内，则需要绑定Mac
}

func (conf *NatgwConf) Create(action int) error {
	var err error

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
	err = public.CreateInterfaceTypeIpvlanL2(conf.Name, device)
	if err != nil {
		return err
	}

	/* set port to netns */
	err = public.SetInterfaceNetns(conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	/* set link up */
	err = public.VrfSetInterfaceLinkUp(conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.Name, conf.LocalAddress)
	if err != nil {
		return err
	}

	if conf.MacBind {
		/* bind nexthop arp */
		cmdstr := fmt.Sprintf("ip netns exec %s ip route add %s/32 dev %s", conf.EdgeId, conf.Nexthop, conf.Name)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		cmdstr = fmt.Sprintf("ip netns exec %s arp -s %s %s", conf.EdgeId, conf.Nexthop, conf.NexthopMac)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			//return err
		}
	}

	/* set tc limit */
	err = public.VrfSetInterfaceIngressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
	if err != nil {
		return err
	}

	/* set iptables snat */
	err = public.VrfSetInterfaceSnat(false, conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add RemoteAddress route */
		if conf.MacBind {
			err = VrfAddRouteOnlink(false, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.Name, conf.EdgeId)
		} else {
			err = VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
		}
		if err != nil {
			return err
		}
	}

	/* set lo link up */
	err = public.VrfSetInterfaceLinkUp(conf.EdgeId, "lo")
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *NatgwConf) Modify(cfgNew *NatgwConf) (error, bool) {

	var err error
	chg := false
	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		err = public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
		if err != nil {
			return err, false
		}
		cfgCur.Bandwidth = cfgNew.Bandwidth
		chg = true
	}

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
		chg = true
	}

	if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
		if cfgCur.RemoteAddress != "" {
			if cfgCur.MacBind {
				err = VrfAddRouteOnlink(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Nexthop, cfgCur.Name, cfgCur.EdgeId)
			} else {
				err = VrfAddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
			}
			if err != nil {
				return err, false
			}
		}
		if cfgNew.RemoteAddress != "" {
			if cfgCur.MacBind {
				err = VrfAddRouteOnlink(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.Nexthop, cfgCur.Name, cfgCur.EdgeId)
			} else {
				err = VrfAddRoute(false, cfgNew.LocalAddress, cfgNew.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
			}
			if err != nil {
				return err, false
			}
		}

		cfgCur.RemoteAddress = cfgNew.RemoteAddress
		chg = true
	}

	return nil, chg
}

func (conf *NatgwConf) Destroy() error {

	var err error

	if conf.RemoteAddress != "" {
		/* (FRR) add RemoteAddress route */
		if conf.MacBind {
			err = VrfAddRouteOnlink(true, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.Name, conf.EdgeId)
		} else {
			err = VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
		}
		if err != nil {
			return err
		}
	}

	if conf.MacBind {
		/* unbind nexthop arp */
		cmdstr := fmt.Sprintf("ip netns exec %s ip route del %s/32 dev %s", conf.EdgeId, conf.Nexthop, conf.Name)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Error(cmdstr, err)
			return err
		}
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(true, conf.EdgeId, conf.Name, conf.LocalAddress)
	if err != nil {
		return err
	}

	/* set iptables snat */
	err = public.VrfSetInterfaceSnat(true, conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	/* destroy interface */
	err = public.VrfDeleteInterface(conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	return nil
}
