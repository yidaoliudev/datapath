package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"
	"errors"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Conn Type: Conn类型 */
	ConnType_Eport = 0
	ConnType_Gre   = 1
	ConnType_Ipsec = 2
	ConnType_Nat   = 3
	ConnType_Nats  = 4
	ConnType_Ssl   = 5

	/* Conn 路由类型 */
	ConnRouteType_Bgp    = 0
	ConnRouteType_Static = 1
)

/*

概述：connection 作为企业分支到客户edge的数据通道，

connection分类：
1，Phy Port 直接互联
2，Phy Port 通过Vlan互联（独享Vlan Port）
3，Phy Port 通过VPN互联，支持IPsec 和Gre两种方式。(共享Phy Port，独享VPN Port)
4，Vlan Port 通过VPN互联，支持IPsec 和Gre两种方式。(共享Vlan Port，独享VPN Port)

connection路由方式：
1，BGP路由方式。
2，Static路由方式。

*/

type ConnStaticConf struct {
	Cidr []string `json:"cidr"`
}

type ConnBgpConf struct {
	KeepAlive int    `json:"keepAlive"` //(必填)
	HoldTime  int    `json:"holdTime"`  //(必填)
	Password  string `json:"password"`  //[选填]
	//LocalAs   uint64 `json:"localAs"`   //{不填}
	//PeerAs    uint64 `json:"peerAs"`    //{不填}
}

type ConnRouteConf struct {
	Type       int            `json:"type"`
	BgpInfo    ConnBgpConf    `json:"bgpInfo"`
	StaticInfo ConnStaticConf `json:"staticInfo"`
}

type ConnConf struct {
	Id            string        `json:"id"`            //(必填)连接Id，使用全局唯一的Id序号
	PortId        string        `json:"portId"`        //(必填)PortId
	EdgeId        string        `json:"edgeId"`        //(必填)Edge Id
	Type          int           `json:"type"`          //(必填)vlanif,gre,ipsec
	Bandwidth     int           `json:"bandwidth"`     //(必填)带宽限速，单位Mbps
	EportInfo     EportConf     `json:"eportInfo"`     //[选填]如果Type为eport
	GreInfo       GreConf       `json:"greInfo"`       //[选填]如果Type为gre
	IpsecInfo     IpsecConf     `json:"ipsecInfo"`     //[选填]如果Type为ipsec
	NatgwInfo     NatgwConf     `json:"natgwInfo"`     //[选填]如果Type为natgw
	NatgwsInfo    NatgwsConf    `json:"natgwsInfo"`    //[选填]如果Type为natgws（共享NAT）
	SslInfo       SslConf       `json:"sslInfo"`       //[选填]如果Type为ssl
	RouteInfo     ConnRouteConf `json:"routeInfo"`     //[必填]Conn路由配置信息
	LocalAddress  string        `json:"localAddress"`  //{不填}
	RemoteAddress string        `json:"remoteAddress"` //{不填}
	ConnName      string        `json:"connName"`      //{不填}
}

func (conf *ConnConf) GetConnStaticCidr() []string {

	var cidrs []string

	//遍历静态路由网段cidr，排除和remoteAddress相同的cidr
	if conf.RouteInfo.Type == ConnRouteType_Static {
		remoteAddr := strings.Split(conf.RemoteAddress, "/")[0] + "/32"
		for _, cidr := range conf.RouteInfo.StaticInfo.Cidr {
			if cidr == remoteAddr {
				continue
			}
			cidrs = append(cidrs, cidr)
		}
	}

	return cidrs
}

func (conf *ConnConf) Create(action int) error {

	var err error
	var port PortConf

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	/* 获取conn对应的Port信息 */
	if conf.Type == ConnType_Ipsec || conf.Type == ConnType_Gre || conf.Type == ConnType_Ssl {
		err, port = GetPortInfoById(conf.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn get Port info fail: ", conf.PortId)
			return err
		}
	}

	/* set ConnName */
	conf.ConnName = conf.Id

	switch conf.Type {
	case ConnType_Eport:
		/* create vlan port */
		eport := &conf.EportInfo
		eport.Name = conf.ConnName
		// 将conn的EdgeId设置为ws
		eport.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			eport.Bandwidth = conf.Bandwidth
		}
		err := eport.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create eport fail: ", eport.PhyifName, eport.VlanId)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(eport.LocalAddress, "/")[0]
		conf.RemoteAddress = eport.RemoteAddress
	case ConnType_Gre:
		/* create gre tunnel */
		gre := &conf.GreInfo
		gre.Name = conf.ConnName
		/* 设置conn关联的物理口LocalAddress  */
		gre.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		gre.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			gre.Bandwidth = conf.Bandwidth
		}
		err = gre.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create gre fail: ", conf.ConnName)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(gre.LocalAddress, "/")[0]
		conf.RemoteAddress = gre.RemoteAddress
	case ConnType_Ipsec:
		/* create ipsec tunnel */
		ipsec := &conf.IpsecInfo
		ipsec.Name = conf.ConnName
		/* 设置conn关联的物理口LocalAddress  */
		ipsec.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		ipsec.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			ipsec.Bandwidth = conf.Bandwidth
		}
		err := ipsec.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create ipsec fail: ", conf.ConnName)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(ipsec.LocalAddress, "/")[0]
		conf.RemoteAddress = ipsec.RemoteAddress
	case ConnType_Nat:
		/* create nat connection */
		nat := &conf.NatgwInfo
		nat.Name = conf.ConnName
		// 将conn的EdgeId设置为ws
		nat.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			nat.Bandwidth = conf.Bandwidth
		}
		err := nat.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create nat fail: ", conf.ConnName)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(nat.LocalAddress, "/")[0]
		conf.RemoteAddress = nat.RemoteAddress
	case ConnType_Nats:
		/* create nats connection */
		nats := &conf.NatgwsInfo
		nats.Name = conf.ConnName
		// 将conn的EdgeId设置为ws
		nats.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			nats.Bandwidth = conf.Bandwidth
		}
		err := nats.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create nats fail: ", conf.ConnName)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(nats.LocalAddress, "/")[0]
		conf.RemoteAddress = nats.RemoteAddress
	case ConnType_Ssl:
		ssl := &conf.SslInfo
		ssl.Name = conf.ConnName
		/* 设置conn关联的物理口LocalAddress  */
		ssl.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		ssl.EdgeId = conf.EdgeId
		if action == public.ACTION_ADD {
			ssl.Bandwidth = conf.Bandwidth
		}
		err := ssl.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create ssl fail: ", conf.ConnName)
			return err
		}
		/* 记录Conn本端和对端地址 */
		conf.LocalAddress = strings.Split(ssl.LocalAddress, "/")[0]
		conf.RemoteAddress = ssl.RemoteAddress
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	/* 如果是recover，则直接返回 */
	if action == public.ACTION_RECOVER {
		return nil
	}

	/* 获取conn对应的Edge信息 */
	err, edge := GetEdgeInfoById(conf.EdgeId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Conn get Edge info fail: ", conf.EdgeId)
		return err
	}

	/* 设置conn的router */
	switch conf.RouteInfo.Type {
	case ConnRouteType_Bgp:
		bgp := &conf.RouteInfo.BgpInfo
		/* 填充bgp实例配置，并创建 */
		bgpInfo := &BgpConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: conf.RemoteAddress, RouterId: conf.LocalAddress, KeepAlive: bgp.KeepAlive, HoldTime: bgp.HoldTime, Password: bgp.Password}
		err = bgpInfo.Create()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create bgp fail: ", conf.ConnName)
			return err
		}
		/* 查看edge关联的tunnel，并宣告tunnel的cidr */
		/* 获取所有tunnel的cidrpeer */
		cidrPeers := GetTunnelCidrsByEdgeId(edge.Id)
		if len(cidrPeers) != 0 {
			/* 宣告bgp cidrs */
			var cdirOld []string
			err, _ := bgpInfo.Announce(cdirOld, cidrPeers)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn create bgp, announce cidr fail: ", edge.Id)
				return err
			}
		}

	case ConnRouteType_Static:
		//static := &conf.RouteInfo.StaticInfo
		/* 填充static实例配置，并创建 */
		StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.ConnName, Cidr: conf.GetConnStaticCidr()}
		if conf.Type == ConnType_Nat && conf.NatgwInfo.Nexthop != "" {
			StaticInfo.Device = conf.NatgwInfo.Nexthop
			if conf.NatgwInfo.MacBind {
				err = StaticInfo.CreateOnlink()
			} else {
				err = StaticInfo.Create()
			}
		} else {
			if conf.Type == ConnType_Nats && conf.NatgwsInfo.Nexthop != "" {
				StaticInfo.Device = conf.NatgwsInfo.Nexthop
			}
			if conf.Type == ConnType_Eport && conf.EportInfo.Nexthop != "" {
				StaticInfo.Device = conf.EportInfo.Nexthop
			}
			err = StaticInfo.Create()
		}
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create static fail: ", conf.ConnName)
			return err
		}
	default:
		return derr.Error{In: err.Error(), Out: "ConnRouteTypeError"}
	}

	return nil
}

func (cfgCur *ConnConf) Modify(cfgNew *ConnConf) (error, bool) {

	var chg = false
	var rtChg = false
	var err error
	var port PortConf

	/* 获取conn对应的Port信息 */
	if cfgCur.Type == ConnType_Ipsec || cfgCur.Type == ConnType_Gre || cfgCur.Type == ConnType_Ssl {
		err, port = GetPortInfoById(cfgCur.PortId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn get Port info fail: ", cfgCur.PortId)
			return err, false
		}
	}

	switch cfgCur.Type {
	case ConnType_Eport:
		/* Modify eport port */
		eport := &cfgCur.EportInfo
		vlanifNew := &cfgNew.EportInfo
		vlanifNew.Name = eport.Name
		vlanifNew.Bandwidth = cfgNew.Bandwidth
		/* 设置conn关联的物理接口名称 */
		//vlanifNew.PhyifName = port.PhyifName
		// 将conn的EdgeId设置为ws
		vlanifNew.EdgeId = cfgCur.EdgeId
		err, chg = eport.Modify(vlanifNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify eport fail: ", vlanifNew.PhyifName, vlanifNew.VlanId)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(eport.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = eport.RemoteAddress
	case ConnType_Gre:
		/* Modify gre tunnel */
		gre := &cfgCur.GreInfo
		greNew := &cfgNew.GreInfo
		greNew.Name = gre.Name
		greNew.Bandwidth = cfgNew.Bandwidth
		/* 设置vap关联的物理口LocalAddress  */
		greNew.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		greNew.EdgeId = cfgCur.EdgeId
		err, chg = gre.Modify(greNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify gre fail: ", cfgCur.ConnName)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(gre.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = gre.RemoteAddress
	case ConnType_Ipsec:
		/* Modify ipsec tunnel */
		ipsec := &cfgCur.IpsecInfo
		ipsecNew := &cfgNew.IpsecInfo
		ipsecNew.Name = ipsec.Name
		ipsecNew.Bandwidth = cfgNew.Bandwidth
		/* 设置vap关联的物理口LocalAddress  */
		ipsecNew.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		ipsecNew.EdgeId = cfgCur.EdgeId
		err, chg = ipsec.Modify(ipsecNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify ipsec fail: ", cfgCur.ConnName)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(ipsec.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = ipsec.RemoteAddress
	case ConnType_Nat:
		/* Modify nat connection */
		nat := &cfgCur.NatgwInfo
		natNew := &cfgNew.NatgwInfo
		natNew.Name = nat.Name
		natNew.Bandwidth = cfgNew.Bandwidth
		natNew.EdgeId = cfgCur.EdgeId
		err, chg = nat.Modify(natNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify nat fail: ", cfgCur.ConnName)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(nat.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = nat.RemoteAddress
	case ConnType_Nats:
		/* Modify nats connection */
		nats := &cfgCur.NatgwsInfo
		natsNew := &cfgNew.NatgwsInfo
		natsNew.Name = nats.Name
		natsNew.Bandwidth = cfgNew.Bandwidth
		natsNew.EdgeId = cfgCur.EdgeId
		err, chg = nats.Modify(natsNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify nats fail: ", cfgCur.ConnName)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(nats.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = nats.RemoteAddress
	case ConnType_Ssl:
		/* Modify ssl tunnel */
		ssl := &cfgCur.SslInfo
		sslNew := &cfgNew.SslInfo
		sslNew.Name = ssl.Name
		sslNew.Bandwidth = cfgNew.Bandwidth
		/* 设置vap关联的物理口LocalAddress  */
		sslNew.TunnelSrc = strings.Split(port.LocalAddress, "/")[0]
		// 将conn的EdgeId设置为ws
		sslNew.EdgeId = cfgCur.EdgeId
		err, chg = ssl.Modify(sslNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify ssl fail: ", cfgCur.ConnName)
			return err, false
		}
		/* 记录Conn本端和对端地址 */
		cfgNew.LocalAddress = strings.Split(ssl.LocalAddress, "/")[0]
		cfgNew.RemoteAddress = ssl.RemoteAddress
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}, false
	}

	/* 获取conn对应的Edge信息 */
	err, edge := GetEdgeInfoById(cfgCur.EdgeId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Conn get Edge info fail: ", cfgCur.EdgeId)
		return err, false
	}
	/* 设置conn的router */
	if cfgCur.RouteInfo.Type == cfgNew.RouteInfo.Type {
		/* 路由参数修改 */
		if cfgCur.RouteInfo.Type == ConnRouteType_Bgp {
			bgpNew := &cfgNew.RouteInfo.BgpInfo
			bgpCur := &cfgCur.RouteInfo.BgpInfo
			/* 填充bgp实例配置，并创建 */
			bgpInfoNew := &BgpConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: cfgNew.RemoteAddress, RouterId: cfgNew.LocalAddress, KeepAlive: bgpNew.KeepAlive, HoldTime: bgpNew.HoldTime, Password: bgpNew.Password}
			bgpInfoCur := &BgpConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: cfgCur.RemoteAddress, RouterId: cfgCur.LocalAddress, KeepAlive: bgpCur.KeepAlive, HoldTime: bgpCur.HoldTime, Password: bgpCur.Password}
			err, rtChg = bgpInfoCur.Modify(bgpInfoNew)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn modify bgp fail: ", cfgCur.ConnName)
				return err, false
			}
			cfgCur.RouteInfo.BgpInfo = cfgNew.RouteInfo.BgpInfo
			/* conn不会修改PeerAs的，所以不需要重新宣告bgp network */

		} else if cfgCur.RouteInfo.Type == ConnRouteType_Static {
			//staticNew := &cfgNew.RouteInfo.StaticInfo
			//staticCur := &cfgCur.RouteInfo.StaticInfo
			/* 填充static实例配置，并创建 */
			StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgNew.GetConnStaticCidr()}
			StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgCur.GetConnStaticCidr()}
			if cfgCur.Type == ConnType_Nat && cfgCur.NatgwInfo.Nexthop != "" {
				StaticInfoNew.Device = cfgCur.NatgwInfo.Nexthop
				StaticInfoCur.Device = cfgCur.NatgwInfo.Nexthop
				if cfgCur.NatgwInfo.MacBind {
					err, rtChg = StaticInfoCur.ModifyOnlink(StaticInfoNew)
				} else {
					err, rtChg = StaticInfoCur.Modify(StaticInfoNew)
				}
			} else {
				if cfgCur.Type == ConnType_Nats && cfgCur.NatgwsInfo.Nexthop != "" {
					StaticInfoNew.Device = cfgCur.NatgwsInfo.Nexthop
					StaticInfoCur.Device = cfgCur.NatgwsInfo.Nexthop
				}
				if cfgCur.Type == ConnType_Eport && cfgCur.EportInfo.Nexthop != "" {
					StaticInfoNew.Device = cfgCur.EportInfo.Nexthop
					StaticInfoCur.Device = cfgCur.EportInfo.Nexthop
				}
				err, rtChg = StaticInfoCur.Modify(StaticInfoNew)
			}
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn modify static fail: ", cfgCur.ConnName)
				return err, false
			}
			cfgCur.RouteInfo.StaticInfo = cfgNew.RouteInfo.StaticInfo
		}

		if rtChg {
			chg = true
		}
	} else {
		/* 如果发生路由协议变化，则删除旧的，增加新的 */
		/* delete old router */
		if cfgCur.RouteInfo.Type == ConnRouteType_Bgp {
			bgpInfoCur := &BgpConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: cfgCur.RemoteAddress}
			err = bgpInfoCur.Destroy()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn Destroy bgp fail: ", cfgCur.ConnName)
				return err, false
			}
		} else if cfgCur.RouteInfo.Type == ConnRouteType_Static {
			//staticCur := &cfgCur.RouteInfo.StaticInfo
			StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgCur.GetConnStaticCidr()}
			if cfgCur.Type == ConnType_Nat && cfgCur.NatgwInfo.Nexthop != "" {
				StaticInfoCur.Device = cfgCur.NatgwInfo.Nexthop
				if cfgCur.NatgwInfo.MacBind {
					err = StaticInfoCur.DestroyOnlink()
				} else {
					err = StaticInfoCur.Destroy()
				}
			} else {
				if cfgCur.Type == ConnType_Nats && cfgCur.NatgwsInfo.Nexthop != "" {
					StaticInfoCur.Device = cfgCur.NatgwsInfo.Nexthop
				}
				if cfgCur.Type == ConnType_Eport && cfgCur.EportInfo.Nexthop != "" {
					StaticInfoCur.Device = cfgCur.EportInfo.Nexthop
				}
				err = StaticInfoCur.Destroy()
			}
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn Destroy static fail: ", cfgCur.ConnName)
				return err, false
			}
		} else {
			agentLog.AgentLogger.Info(err, "Conn Destroy Cur ConnRouteType error: ", cfgCur.RouteInfo.Type)
			return derr.Error{In: err.Error(), Out: "ConnRouteTypeError"}, false
		}

		/* add new router */
		if cfgNew.RouteInfo.Type == ConnRouteType_Bgp {
			bgpNew := &cfgNew.RouteInfo.BgpInfo
			bgpInfoNew := &BgpConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: cfgNew.RemoteAddress, RouterId: cfgNew.LocalAddress, KeepAlive: bgpNew.KeepAlive, HoldTime: bgpNew.HoldTime, Password: bgpNew.Password}
			err = bgpInfoNew.Create()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn Create bgp fail: ", cfgCur.ConnName)
				return err, false
			}

			/* 查看edge关联的tunnel，并宣告tunnel的cidr */
			/* 获取所有tunnel的cidrpeer */
			cidrPeers := GetTunnelCidrsByEdgeId(edge.Id)
			if len(cidrPeers) != 0 {
				/* 宣告bgp cidrs */
				var cdirOld []string
				err, _ := bgpInfoNew.Announce(cdirOld, cidrPeers)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Conn Create bgp, announce cidr fail: ", cfgCur.ConnName)
					return err, false
				}
			}

		} else if cfgNew.RouteInfo.Type == ConnRouteType_Static {
			//staticNew := &cfgNew.RouteInfo.StaticInfo
			StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgNew.GetConnStaticCidr()}
			if cfgCur.Type == ConnType_Nat && cfgCur.NatgwInfo.Nexthop != "" {
				StaticInfoNew.Device = cfgCur.NatgwInfo.Nexthop
				if cfgCur.NatgwInfo.MacBind {
					err = StaticInfoNew.CreateOnlink()
				} else {
					err = StaticInfoNew.Create()
				}
			} else {
				if cfgCur.Type == ConnType_Nats && cfgCur.NatgwsInfo.Nexthop != "" {
					StaticInfoNew.Device = cfgCur.NatgwsInfo.Nexthop
				}
				if cfgCur.Type == ConnType_Eport && cfgCur.EportInfo.Nexthop != "" {
					StaticInfoNew.Device = cfgCur.EportInfo.Nexthop
				}
				err = StaticInfoNew.Create()
			}
			if err != nil {
				agentLog.AgentLogger.Info(err, "Conn Create static fail: ", cfgCur.ConnName)
				return err, false
			}
		} else {
			agentLog.AgentLogger.Info(err, "Conn Create New ConnRouteType error: ", cfgNew.RouteInfo.Type)
			return derr.Error{In: err.Error(), Out: "ConnRouteTypeError"}, false
		}

		cfgCur.RouteInfo = cfgNew.RouteInfo
		chg = true
	}

	return nil, chg
}

func (cfgCur *ConnConf) ModifyStatic(cfgNew *ConnConf) (error, bool) {

	var chg = false

	/* 获取conn对应的Edge信息 */
	err, edge := GetEdgeInfoById(cfgCur.EdgeId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Conn get Edge info fail: ", cfgCur.EdgeId)
		return err, false
	}

	/* 继承就configure的配置 */
	cfgNew.RemoteAddress = cfgCur.RemoteAddress
	cfgNew.RouteInfo.Type = cfgCur.RouteInfo.Type

	if cfgCur.RouteInfo.Type == ConnRouteType_Static {
		/* 填充static实例配置，并创建 */
		StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgNew.GetConnStaticCidr()}
		StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.ConnName, Cidr: cfgCur.GetConnStaticCidr()}
		if cfgCur.Type == ConnType_Nat && cfgCur.NatgwInfo.Nexthop != "" {
			StaticInfoNew.Device = cfgCur.NatgwInfo.Nexthop
			StaticInfoCur.Device = cfgCur.NatgwInfo.Nexthop
			if cfgCur.NatgwInfo.MacBind {
				err, chg = StaticInfoCur.ModifyOnlink(StaticInfoNew)
			} else {
				err, chg = StaticInfoCur.Modify(StaticInfoNew)
			}
		} else {
			if cfgCur.Type == ConnType_Nats && cfgCur.NatgwsInfo.Nexthop != "" {
				StaticInfoNew.Device = cfgCur.NatgwsInfo.Nexthop
				StaticInfoCur.Device = cfgCur.NatgwsInfo.Nexthop
			}
			if cfgCur.Type == ConnType_Eport && cfgCur.EportInfo.Nexthop != "" {
				StaticInfoNew.Device = cfgCur.EportInfo.Nexthop
				StaticInfoCur.Device = cfgCur.EportInfo.Nexthop
			}
			err, chg = StaticInfoCur.Modify(StaticInfoNew)
		}
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn modify static fail: ", cfgCur.ConnName)
			return err, false
		}
		cfgCur.RouteInfo.StaticInfo = cfgNew.RouteInfo.StaticInfo
	}

	return nil, chg
}

func (conf *ConnConf) Destroy() error {

	/* 获取conn对应的Edge信息 */
	err, edge := GetEdgeInfoById(conf.EdgeId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Conn get Edge info fail: ", conf.EdgeId)
		return err
	}
	/* 设置conn的router */
	switch conf.RouteInfo.Type {
	case ConnRouteType_Bgp:
		/* 填充bgp实例配置，并删除 */
		bgpInfo := &BgpConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs, PeerAddress: conf.RemoteAddress}
		err = bgpInfo.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy bgp fail: ", conf.ConnName)
			return err
		}
	case ConnRouteType_Static:
		//static := &conf.RouteInfo.StaticInfo
		/* 填充static实例配置，并创建 */
		StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.ConnName, Cidr: conf.GetConnStaticCidr()}
		if conf.Type == ConnType_Nat && conf.NatgwInfo.Nexthop != "" {
			StaticInfo.Device = conf.NatgwInfo.Nexthop
			if conf.NatgwInfo.MacBind {
				err = StaticInfo.DestroyOnlink()
			} else {
				err = StaticInfo.Destroy()
			}
		} else {
			if conf.Type == ConnType_Nats && conf.NatgwsInfo.Nexthop != "" {
				StaticInfo.Device = conf.NatgwsInfo.Nexthop
			}
			if conf.Type == ConnType_Eport && conf.EportInfo.Nexthop != "" {
				StaticInfo.Device = conf.EportInfo.Nexthop
			}
			err = StaticInfo.Destroy()
		}
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy static fail: ", conf.ConnName)
			return err
		}
	default:
	}

	switch conf.Type {
	case ConnType_Eport:
		/* Destroy vlan port */
		eport := &conf.EportInfo
		err := eport.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy eport fail: ", conf.ConnName)
			return err
		}
	case ConnType_Gre:
		/* Destroy gre port */
		gre := &conf.GreInfo
		err := gre.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy gre fail: ", conf.ConnName)
			return err
		}
	case ConnType_Ipsec:
		/* Destroy ipsec port */
		ipsec := &conf.IpsecInfo
		err := ipsec.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy ipsec fail: ", conf.ConnName)
			return err
		}
	case ConnType_Nat:
		/* Destroy nat connection */
		nat := &conf.NatgwInfo
		err := nat.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy nat fail: ", conf.ConnName)
			return err
		}
	case ConnType_Nats:
		/* Destroy nats connection */
		nats := &conf.NatgwsInfo
		err := nats.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy nats fail: ", conf.ConnName)
			return err
		}
	case ConnType_Ssl:
		/* Destroy ssl port */
		ssl := &conf.SslInfo
		err := ssl.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy ssl fail: ", conf.ConnName)
			return err
		}
	default:
	}

	return nil
}

func (conf *ConnConf) InitIpsecSa() (error, string) {

	var resInfo string
	var err error

	switch conf.Type {
	case ConnType_Ipsec:
		/* init ipsec sa */
		ipsec := &conf.IpsecInfo
		err, resInfo = ipsec.InitIpsecSa()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Init conn ipsec sa fail: ", conf.ConnName)
			return err, "Init conn ipsec sa fail"
		}
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}, "ConnType Error"
	}

	return nil, resInfo
}

func (conf *ConnConf) GetIpsecSa() (error, string) {

	var resInfo string
	var err error

	switch conf.Type {
	case ConnType_Ipsec:
		/* get ipsec sa */
		ipsec := &conf.IpsecInfo
		err, resInfo = ipsec.GetIpsecSa()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Get conn ipsec sa fail: ", conf.ConnName)
			return err, "Get conn ipsec sa fail"
		}
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}, "ConnType Error"
	}

	return nil, resInfo
}

func (conf *ConnConf) ClearBgpNeigh() error {

	if conf.RouteInfo.Type != ConnRouteType_Bgp {
		return nil
	}

	cmd := BgpClear(conf.EdgeId, conf.RemoteAddress)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}

	return nil
}

func GetConnInfoById(id string) (error, ConnConf) {

	var find = false
	var conn ConnConf
	paths := []string{config.ConnConfPath}
	conns, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.ConnConfPath not found: ", err.Error())
	} else {
		for _, value := range conns {
			bytes := []byte(value)
			fp := &ConnConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			conn = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "ConnNotFound"}, conn
	}

	return nil, conn
}

func GetConnInfoByEdgeId(edgeId string) (bool, ConnConf) {

	var find = false
	var conn ConnConf
	paths := []string{config.ConnConfPath}
	conns, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range conns {
			bytes := []byte(value)
			fp := &ConnConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.EdgeId != edgeId {
				continue
			}

			conn = *fp
			find = true
			break
		}
	}

	return find, conn
}

func CheckConnCidrRemoteAddrExist(connId, remoteAddress string) bool {

	paths := []string{config.ConnConfPath}
	conns, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.ConnConfPath not found: ", err.Error())
	} else {
		for _, value := range conns {
			bytes := []byte(value)
			fp := &ConnConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id == connId {
				if fp.RouteInfo.Type == ConnRouteType_Static {
					remoteAddr := strings.Split(remoteAddress, "/")[0] + "/32"
					for _, cidr := range fp.RouteInfo.StaticInfo.Cidr {
						if cidr == remoteAddr {
							return true
						}
					}
				}
				break
			}
		}
	}

	return false
}
