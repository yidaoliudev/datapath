package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"
	"fmt"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

/*
vpl Endpoint实例。
*/
type VplEndpConf struct {
	Id            string `json:"id"`            //(必填)
	LinkId        string `json:"linkId"`        //(必填)vpl对应LinkId
	VapId         string `json:"vapId"`         //(必填)本端Vap Id
	Bandwidth     int    `json:"bandwidth"`     //(必填)带宽限速，单位Mbps，默认0
	LocalAddress  string `json:"localAddress"`  //(必填)本端vpl地址（配置在vap实例的ipvlan接口上）
	RemoteAddress string `json:"remoteAddress"` //(必填)对端vpl地址
	HealthCheck   bool   `json:"healthCheck"`   //[选填]vpl健康检查，默认true(启动)，探测vpl对端地址的时延，丢包等质量。
	VapNexthop    string `json:"vapNexthop"`    //[选填]如果是L2 Network，需要指定vap的nexthop
	NexthopMac    string `json:"nexthopMac"`    //[选填]如果是L3 Network，需要填写vap网关的mac地址
	MacBind       bool   `json:"macBind"`       //{不填} 如果网关地址不在本端地址范围内，则需要绑定Mac
}

/*
VplEndpConf
*/
func (conf *VplEndpConf) Create(action int) error {

	var err error

	/* 获取vpl对应的Vap信息 */
	err, vap := GetVapInfoById(conf.VapId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "get Vap info fail: ", conf.VapId)
		return err
	}

	if vap.Type != VapType_Eport {
		/* 只有扩展port类型的vap支持vpl */
		return derr.Error{In: err.Error(), Out: "VplEndpVapTypeError"}
	}

	/* 如果vap关联L3 network，则使用vap的Nexthop */
	if conf.VapNexthop == "" {
		conf.VapNexthop = vap.VapNexthop
	}

	/* 判断网关地址是否在本端地址范围内 */
	conf.MacBind = false
	localcidr := public.GetCidrIpRange(conf.LocalAddress)
	nexthopcidr := public.GetCidrIpRange(conf.VapNexthop + "/" + strings.Split(conf.LocalAddress, "/")[1])
	if localcidr != nexthopcidr {
		if conf.NexthopMac == "" {
			return derr.Error{In: err.Error(), Out: "NoNexthopMacError"}
		}
		conf.MacBind = true
	}

	if conf.LocalAddress == vap.LocalAddress {
		agentLog.AgentLogger.Info(err, "VplEndp localAddress error. ", conf.LocalAddress)
		return derr.Error{In: err.Error(), Out: "VplEndpLocalAddressError"}
	}

	/* create netns */
	cmdstr := fmt.Sprintf("ip netns add %s", conf.Id)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	/* create ipvlan l2 interface */
	err = public.CreateInterfaceTypeIpvlanL2(conf.Id, conf.VapId)
	if err != nil {
		return err
	}

	/* set port to netns */
	err = public.SetInterfaceNetns(conf.Id, conf.Id)
	if err != nil {
		return err
	}

	/* set vpl up */
	err = public.VrfSetInterfaceLinkUp(conf.Id, conf.Id)
	if err != nil {
		return err
	}

	if conf.MacBind {
		/* bind nexthop arp */
		cmdstr = fmt.Sprintf("ip netns exec %s ip route add %s/32 dev %s", conf.Id, conf.VapNexthop, conf.Id)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		cmdstr = fmt.Sprintf("ip netns exec %s arp -s %s %s", conf.Id, conf.VapNexthop, conf.NexthopMac)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			//return err
		}
	}

	/* set vpl ip address */
	err = public.VrfSetInterfaceAddress(false, conf.Id, conf.Id, conf.LocalAddress)
	if err != nil {
		return err
	}

	/* set tc limit */
	err = public.VrfSetInterfaceEgressLimit(conf.Id, conf.Id, conf.Bandwidth)
	if err != nil {
		return err
	}

	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add route RemoteAddress via vap */
		if conf.MacBind {
			err = VrfAddRouteOnlink(false, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop, conf.Id, conf.Id)
		} else {
			err = VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop, conf.Id)
		}
		if err != nil {
			return err
		}
	}

	/* 默认开启 */
	conf.HealthCheck = true
	return nil
}

func (cfgCur *VplEndpConf) Modify(cfgNew *VplEndpConf) (error, bool) {

	var err error
	chg := false

	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		/* set tc limit */
		err = public.VrfSetInterfaceEgressLimit(cfgCur.Id, cfgCur.Id, cfgNew.Bandwidth)
		if err != nil {
			return err, false
		}

		cfgCur.Bandwidth = cfgNew.Bandwidth
		chg = true
	}

	if cfgCur.LinkId != cfgNew.LinkId {
		cfgCur.LinkId = cfgNew.LinkId
		chg = true
	}

	return nil, chg
}

func (conf *VplEndpConf) Destroy() error {

	var err error

	if conf.MacBind {
		/* unbind nexthop arp */
		cmdstr := fmt.Sprintf("ip netns exec %s ip route del %s/32 dev %s", conf.Id, conf.VapNexthop, conf.Id)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Error(cmdstr, err)
			return err
		}
	}

	if conf.RemoteAddress != "" {
		/* (FRR) delelte route RemoteAddress via vap */
		if conf.MacBind {
			err = VrfAddRouteOnlink(true, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop, conf.Id, conf.Id)
		} else {
			err = VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop, conf.Id)
		}
		if err != nil {
			return err
		}
	}

	/* flush vpl ip address */
	err = public.VrfSetInterfaceAddress(true, conf.Id, conf.Id, conf.LocalAddress)
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

func GetVplEndpInfoById(id string) (error, VplEndpConf) {

	var find = false
	var vplEndp VplEndpConf
	paths := []string{config.VplEndpConfPath}
	vplEndps, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.VplEndpConfPath not found: ", err.Error())
	} else {
		for _, value := range vplEndps {
			bytes := []byte(value)
			fp := &VplEndpConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			vplEndp = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "VplEndpNotFound"}, vplEndp
	}

	return nil, vplEndp
}
