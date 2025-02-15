package app

import (
	"datapath/public"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

/*
共享NAT GW
*/
type NatgwsConf struct {
	Name          string `json:"name"`          //[继承]Natgws名称
	EdgeId        string `json:"edgeId"`        //[继承]Edge Id
	NatshareId    string `json:"natshareId"`    //[继承]natshare网关，写Natshare Id
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	LocalAddress  string `json:"localAddress"`  //(必填)Natgws本端地址
	Nexthop       string `json:"nexthop"`       //(必填)Natgws网关地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Natgws对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
}

func (conf *NatgwsConf) Create(action int) error {

	var err error

	if conf.NatshareId == "" {
		return derr.Error{In: err.Error(), Out: "natTypeError"}
	}

	/* create veth */
	peerName := conf.Name + "_peer"
	peerAddress := strings.Split(conf.Nexthop, "/")[0] + "/30"
	err = public.CreateInterfaceTypeVeth(conf.Name, peerName)
	if err != nil {
		return err
	}

	////// set natshare configure.
	/* set port to netns */
	err = public.SetInterfaceNetns(conf.NatshareId, peerName)
	if err != nil {
		return err
	}

	/* set link up */
	err = public.VrfSetInterfaceLinkUp(conf.NatshareId, peerName)
	if err != nil {
		return err
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(false, conf.NatshareId, peerName, peerAddress)
	if err != nil {
		return err
	}

	////// set local natgws configure.
	localAddress := strings.Split(conf.LocalAddress, "/")[0] + "/30"
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
	err = public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.Name, localAddress)
	if err != nil {
		return err
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

	if action == public.ACTION_ADD {
		if conf.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			err = VrfAddRoute(false, localAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
			if err != nil {
				return err
			}
		}
	}

	/* set lo link up */
	err = public.VrfSetInterfaceLinkUp(conf.EdgeId, "lo")
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *NatgwsConf) Modify(cfgNew *NatgwsConf) (error, bool) {

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
		localAddress := strings.Split(cfgCur.LocalAddress, "/")[0] + "/30"
		if cfgCur.RemoteAddress != "" {
			err = VrfAddRoute(true, localAddress, cfgCur.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
			if err != nil {
				return err, false
			}
		}
		if cfgNew.RemoteAddress != "" {
			err = VrfAddRoute(false, localAddress, cfgNew.RemoteAddress, cfgCur.Nexthop, cfgCur.EdgeId)
			if err != nil {
				return err, false
			}
		}

		cfgCur.RemoteAddress = cfgNew.RemoteAddress
		chg = true
	}

	return nil, chg
}

func (conf *NatgwsConf) Destroy() error {

	var err error

	localAddress := strings.Split(conf.LocalAddress, "/")[0] + "/30"
	if conf.RemoteAddress != "" {
		/* (FRR) delete RemoteAddress route */
		err = VrfAddRoute(true, localAddress, conf.RemoteAddress, conf.Nexthop, conf.EdgeId)
		if err != nil {
			return err
		}
	}

	/* set iptables snat */
	err = public.VrfSetInterfaceSnat(true, conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(true, conf.EdgeId, conf.Name, localAddress)
	if err != nil {
		return err
	}

	if conf.NatshareId != "" {
		peerName := conf.Name + "_peer"
		peerAddress := strings.Split(conf.Nexthop, "/")[0] + "/30"
		/* delete address */
		err = public.VrfSetInterfaceAddress(true, conf.NatshareId, peerName, peerAddress)
		if err != nil {
			return err
		}
	}

	/* destroy interface */
	err = public.VrfDeleteInterface(conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	return nil
}
