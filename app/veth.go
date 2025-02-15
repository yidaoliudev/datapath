package app

import (
	"datapath/public"
	"strings"
)

const (
	VethPeerSuffix = "_peer"
)

/*
 veth pair方式的Tunnel。用于连接在同一个Pop上的两个edge。
1，veth方式的Tunnel流量不经过vap转发。
2，veth方式的Tunnel无需underlay 源和目的地址。
*/

type VethConf struct {
	Name          string `json:"name"`          //[继承]veth名称
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	EdgeId        string `json:"edgeId"`        //[继承]如果EdgeId非空，则表示Edge内部veth
	EdgeIdPeer    string `json:"edgeIdPeer"`    //[继承]如果EdgeId非空，则表示Edge内部veth
	LocalAddress  string `json:"localAddress"`  //(必填)veth隧道本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)veth隧道对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
}

func (conf *VethConf) Create(action int) error {

	/* 创建互联veth tunnel */
	peerName := conf.Name + VethPeerSuffix
	err := public.CreateInterfaceTypeVeth(conf.Name, peerName)
	if err != nil {
		return err
	}

	/* set local edge veth*/
	/* set link to netns */
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
	err = public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.Name, strings.Split(conf.LocalAddress, "/")[0]+"/30")
	if err != nil {
		return err
	}

	/* set tc limit */
	err = public.VrfSetInterfaceEgressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
	if err != nil {
		return err
	}

	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add RemoteAddress route */
		err = VrfAddRoute(false, strings.Split(conf.LocalAddress, "/")[0]+"/30", conf.RemoteAddress, conf.Name, conf.EdgeId)
		if err != nil {
			return err
		}
	}

	/* set peer edge veth*/
	/* set link to netns */
	err = public.SetInterfaceNetns(conf.EdgeIdPeer, peerName)
	if err != nil {
		return err
	}

	/* set link up */
	err = public.VrfSetInterfaceLinkUp(conf.EdgeIdPeer, peerName)
	if err != nil {
		return err
	}

	/* set link ip address */
	err = public.VrfSetInterfaceAddress(false, conf.EdgeIdPeer, peerName, strings.Split(conf.RemoteAddress, "/")[0]+"/30")
	if err != nil {
		return err
	}

	/* set tc limit */
	err = public.VrfSetInterfaceEgressLimit(conf.EdgeIdPeer, peerName, conf.Bandwidth)
	if err != nil {
		return err
	}

	if action == public.ACTION_ADD && conf.LocalAddress != "" {
		/* (FRR) add RemoteAddress route */
		err = VrfAddRoute(false, strings.Split(conf.RemoteAddress, "/")[0]+"/30", conf.LocalAddress, peerName, conf.EdgeIdPeer)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cfgCur *VethConf) Modify(cfgNew *VethConf) (error, bool) {

	var chg = false

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
		chg = true
	}

	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		/* set tc limit */
		err := public.VrfSetInterfaceEgressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
		if err != nil {
			return err, false
		}

		err = public.VrfSetInterfaceEgressLimit(cfgCur.EdgeIdPeer, cfgCur.Name+VethPeerSuffix, cfgNew.Bandwidth)
		if err != nil {
			return err, false
		}
		cfgCur.Bandwidth = cfgNew.Bandwidth
		chg = true
	}

	return nil, chg
}

func (conf *VethConf) Destroy() error {
	/* (FRR) delete gre RemoteAddress route */
	err := VrfAddRoute(true, strings.Split(conf.LocalAddress, "/")[0]+"/30", conf.RemoteAddress, conf.Name, conf.EdgeId)
	if err != nil {
		return err
	}

	err = VrfAddRoute(true, strings.Split(conf.RemoteAddress, "/")[0]+"/30", conf.LocalAddress, conf.Name+VethPeerSuffix, conf.EdgeIdPeer)
	if err != nil {
		return err
	}

	/* destroy veth */
	err = public.VrfDeleteInterface(conf.EdgeId, conf.Name)
	if err != nil {
		return err
	}

	return nil
}
