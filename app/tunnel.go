package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (

	/* Tunnel Type: Tunnel类型 */
	TunnelType_Veth = 0 /* 在同一个Pop内的两个edge互通tunnel。 */
	TunnelType_Gre  = 1 /* 租户共享Link骨干时，基于gre tunnel互联. */
	TunnelType_Vpl  = 2 /* 租户独享Link骨干时，基于gre tunnel互联. */
)

/*

概述：Tunnel 是edge到edge的租户骨干隧道。

Tunnel使用约定：
1，edge to edge可能存在多条隧道，但两点之间最多同时启用2条，并设置主备Tunnel。
2，Tunnel使用BGP协议传递edge路由。
3，如果两个edge都在同一个PoP上，则底层使用veth-pair实现；如果在不同PoP上，则使用Gre tunnel实现。
4，每个tunnel将获取一组/30内部管理地址。由控制器分配，可回收使用。

Tunnel主备约定：
1，Full-mesh型不支持tunnel主备，另外2种都可支持主备tunnel。
2，主tunnel默认使用bfd探测，可以取消。
3，备tunnel使用bgp as-paths增加as路径实现。

Tunnel类型：
1，两个edge在同一个PoP上，使用veth-pair
2，两个edge在不同的PoP上，使用共享Link时，使用Gre tunnel。

*/

type TunnelConf struct {
	Id         string   `json:"id"`         //(必填)，Id全局唯一
	Type       int      `json:"type"`       //(必填)类型，veth（0），gre（1)
	Bandwidth  int      `json:"bandwidth"`  //(必填)带宽限速，单位Mbps，默认0
	EdgeId     string   `json:"edgeId"`     //(必填)PoP上的本端Edge ID
	EdgeIdPeer string   `json:"edgeIdPeer"` //[选填]如果是同PoP模式，此参数必填。同PoP模式，只需调用一次API即可
	LinkEndpId string   `json:"linkEndpId"` //[选填]如果是异PoP模式，使用link时，此参数必填。
	VplEndpId  string   `json:"vplEndpId"`  //[选填]如果是异PoP模式，使用vpl时，此参数必填。
	VethInfo   VethConf `json:"vethInfo"`   //[选填]如果Type为veth
	GreInfo    GreConf  `json:"greInfo"`    //[选填]如果Type为Gre
	Cidr       []string `json:"cidr"`       //[选填]如果Type为veth，本端子网信息
	CidrPeer   []string `json:"cidrPeer"`   //(必填)对端子网信息
	Pbr        bool     `json:"pbr"`        //[选填]如果Tunnel为PBR策略，并且是A端，则为true
	Priority   int      `json:"priority"`   //[选填]如果Tunnel为PBR策略，并且是A端，则需要填写，范围：1-256
	TableId    int      `json:"tableId"`    //[选填]如果Tunnel为PBR策略，并且是A端，则需要填写，范围：1-1024，Edge内所有Tunnel不能重复
	TunnelName string   `json:"tunnelName"` //{不填}
}

func (conf *TunnelConf) Create(action int) error {

	var err error
	/* set TunnelName */
	conf.TunnelName = conf.Id

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	switch conf.Type {
	case TunnelType_Veth:
		/* 创建互联veth tunnel */
		veth := &conf.VethInfo
		veth.Name = conf.TunnelName
		veth.EdgeId = conf.EdgeId
		veth.EdgeIdPeer = conf.EdgeIdPeer
		if action == public.ACTION_ADD {
			veth.Bandwidth = conf.Bandwidth
			/* 默认开启 */
			veth.HealthCheck = true
		}
		err = veth.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Create veth tunnel fail: ", conf.Id)
			return err
		}
	case TunnelType_Gre:
		/* 查询tunnel生效的linkEndp */
		err, linkEndp := GetLinkEndpInfoById(conf.LinkEndpId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "get linkEndp info fail: ", conf.LinkEndpId)
			return err
		}
		/* 创建互联gre tunnel */
		gre := &conf.GreInfo
		gre.Name = conf.TunnelName
		gre.EdgeId = conf.EdgeId
		gre.TunnelSrc = strings.Split(linkEndp.LocalAddress, "/")[0]
		gre.TunnelDst = strings.Split(linkEndp.RemoteAddress, "/")[0]
		gre.GreKey, _ = strconv.Atoi(strings.Split(conf.Id, "-")[1])
		if action == public.ACTION_ADD {
			gre.Bandwidth = conf.Bandwidth
			/* 默认开启 */
			gre.HealthCheck = true
		}
		err = gre.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Create gre tunnel fail: ", conf.Id)
			return err
		}
	case TunnelType_Vpl:
		/* 查询tunnel生效的vpl endpoint */
		err, vplEndp := GetVplEndpInfoById(conf.VplEndpId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "get vplEndp info fail: ", conf.VplEndpId)
			return err
		}
		/* 创建互联gre tunnel */
		gre := &conf.GreInfo
		gre.Name = conf.TunnelName
		gre.EdgeId = conf.EdgeId
		gre.TunnelSrc = strings.Split(vplEndp.LocalAddress, "/")[0]
		gre.TunnelDst = strings.Split(vplEndp.RemoteAddress, "/")[0]
		gre.GreKey, _ = strconv.Atoi(strings.Split(conf.Id, "-")[1])
		/* 如果是共享tunnel，则在vplEndp中创建gre */
		gre.RootId = vplEndp.Id
		if action == public.ACTION_ADD {
			gre.Bandwidth = conf.Bandwidth
			/* 默认开启 */
			gre.HealthCheck = true
		}
		err = gre.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Create gre tunnel fail: ", conf.Id)
			return err
		}
	default:
		return derr.Error{In: err.Error(), Out: "TunnelTypeError"}
	}

	if action != public.ACTION_ADD {
		if conf.Pbr {
			for _, cidr := range conf.Cidr {
				err = public.VrfSetIpruleBySource(false, conf.EdgeId, cidr, conf.TableId, conf.Priority)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel recovery iprule fail: ", conf.TunnelName)
					return err
				}
			}
		}
		return nil
	}

	/* 设置Cidr路由 */
	switch conf.Type {
	case TunnelType_Veth:
		/* 获取tunnel对应的Edge信息 */
		err, edge := GetEdgeInfoById(conf.EdgeId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", conf.EdgeId)
			return err
		}

		if conf.Pbr {
			/* 如果是PBR模式，则下发基于源地址段的策略路由 */
			err, _ := VrfIpRouteWithTable(false, "0.0.0.0/0", conf.VethInfo.RemoteAddress, conf.TableId, edge.Id)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel create vrfIpRouteTable fail: ", conf.TunnelName)
				return err
			}

			for _, cidr := range conf.Cidr {
				err = public.VrfSetIpruleBySource(false, conf.EdgeId, cidr, conf.TableId, conf.Priority)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel create iprule fail: ", conf.TunnelName)
					return err
				}
			}
		} else {
			/* 填充static实例配置，并创建 */
			StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.VethInfo.RemoteAddress /* 使用对端地址作为nexthop */, Cidr: conf.CidrPeer}
			err = StaticInfo.Create()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel create static fail: ", conf.TunnelName)
				return err
			}
		}

		/* 获取Edge是否存在Conn */
		find, conn := GetConnInfoByEdgeId(conf.EdgeId)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
			if len(conf.CidrPeer) != 0 {
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, conf.CidrPeer)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}

		/*获取tunnel对应的EdgePeer信息*/
		err, edgePeer := GetEdgeInfoById(conf.EdgeIdPeer)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get EdgePeer info fail: ", conf.EdgeIdPeer)
			return err
		}

		/* 填充static实例配置，并创建 */
		StaticInfo := &StaticConf{Id: conf.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, Device: conf.VethInfo.LocalAddress /* 使用对端地址作为nexthop */, Cidr: conf.Cidr}
		err = StaticInfo.Create()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel create static fail: ", conf.TunnelName)
			return err
		}

		/* 获取EdgePeer是否存在Conn */
		find, conn = GetConnInfoByEdgeId(conf.EdgeIdPeer)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, PeerAs: edgePeer.PeerAs}
			if len(conf.Cidr) != 0 {
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, conf.Cidr)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}

	case TunnelType_Gre, TunnelType_Vpl:
		/* 获取tunnel对应的Edge信息 */
		err, edge := GetEdgeInfoById(conf.EdgeId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", conf.EdgeId)
			return err
		}

		if conf.Pbr {
			/* 如果是PBR模式，则下发基于源地址段的策略路由 */
			err, _ := VrfIpRouteWithTable(false, "0.0.0.0/0", conf.TunnelName, conf.TableId, edge.Id)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel create vrfIpRouteTable fail: ", conf.TunnelName)
				return err
			}

			for _, cidr := range conf.Cidr {
				err = public.VrfSetIpruleBySource(false, conf.EdgeId, cidr, conf.TableId, conf.Priority)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel create iprule fail: ", conf.TunnelName)
					return err
				}
			}
		} else {
			/* 填充static实例配置，并创建 */
			StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.TunnelName, Cidr: conf.CidrPeer}
			err = StaticInfo.Create()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel create static fail: ", conf.TunnelName)
				return err
			}
		}

		/* 获取Edge是否存在Conn */
		find, conn := GetConnInfoByEdgeId(conf.EdgeId)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
			if len(conf.CidrPeer) != 0 {
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, conf.CidrPeer)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}
	default:
		return derr.Error{In: err.Error(), Out: "TunnelTypeError"}
	}

	return nil
}

func (cfgCur *TunnelConf) Modify(cfgNew *TunnelConf) (error, bool) {

	var chg = false
	var err error
	var rebuild = false

	/* 首选需要比较tunnel得Type是否变化 */
	if cfgCur.Type != cfgNew.Type {
		if cfgCur.Type == TunnelType_Veth || cfgNew.Type == TunnelType_Veth {
			/* veth类型的tunnel不能切换成其它类型 */
			return derr.Error{In: err.Error(), Out: "TunnelTypeError"}, false
		}

		/* link与vpl类型之间切换，则先删除，再创建 */
		rebuild = true
	} else if cfgCur.Type == TunnelType_Vpl && cfgCur.VplEndpId != cfgNew.VplEndpId {
		/* vpl与vpl同类型之间切换，则先删除，再创建 */
		rebuild = true
	}

	if rebuild {
		err = cfgCur.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify tunnel in destory old fail: ", cfgCur.Id)
			return err, false
		}

		err = cfgNew.Create(public.ACTION_ADD)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify tunnel in create new fail: ", cfgNew.Id)
			return err, false
		}

		/* 复制 */
		cfgCur.Type = cfgNew.Type
		cfgCur.GreInfo = cfgNew.GreInfo
		cfgCur.Bandwidth = cfgNew.Bandwidth
		cfgCur.LinkEndpId = cfgNew.LinkEndpId
		cfgCur.VplEndpId = cfgNew.VplEndpId
		cfgCur.Cidr = cfgNew.Cidr
		cfgCur.CidrPeer = cfgNew.CidrPeer
		cfgCur.Pbr = cfgNew.Pbr
		cfgCur.Priority = cfgNew.Priority
		cfgCur.TableId = cfgNew.TableId
		return nil, true
	}

	switch cfgCur.Type {
	case TunnelType_Veth:
		veth := &cfgCur.VethInfo
		vethNew := &cfgNew.VethInfo
		vethNew.Name = veth.Name
		vethNew.EdgeId = veth.EdgeId
		vethNew.EdgeIdPeer = veth.EdgeIdPeer
		vethNew.Bandwidth = cfgNew.Bandwidth
		/* 默认开启 */
		vethNew.HealthCheck = true
		err, chg = veth.Modify(vethNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify veth tunnel fail: ", cfgCur.Id)
			return err, false
		}
	case TunnelType_Gre:
		/* 如果LinkEndpId修改 */
		if cfgCur.LinkEndpId != cfgNew.LinkEndpId {
			cfgCur.LinkEndpId = cfgNew.LinkEndpId
			chg = true
		}
		/* 查询tunnel生效的linkEndp */
		err, linkEndp := GetLinkEndpInfoById(cfgNew.LinkEndpId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify gre get linkEndp info fail: ", cfgNew.LinkEndpId)
			return err, false
		}
		gre := &cfgCur.GreInfo
		greNew := &cfgNew.GreInfo
		greNew.Name = gre.Name
		greNew.EdgeId = gre.EdgeId
		greNew.Bandwidth = cfgNew.Bandwidth
		greNew.TunnelSrc = strings.Split(linkEndp.LocalAddress, "/")[0]
		greNew.TunnelDst = strings.Split(linkEndp.RemoteAddress, "/")[0]
		greNew.GreKey, _ = strconv.Atoi(strings.Split(cfgNew.Id, "-")[1])
		/* 默认开启 */
		greNew.HealthCheck = true
		err, chg = gre.Modify(greNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify gre tunnel fail: ", cfgCur.Id)
			return err, false
		}
	case TunnelType_Vpl:
		/* 如果LinkEndpId修改 */
		if cfgCur.VplEndpId != cfgNew.VplEndpId {
			cfgCur.VplEndpId = cfgNew.VplEndpId
			chg = true
		}
		/* 查询tunnel生效的vplEndp */
		err, vplEndp := GetVplEndpInfoById(cfgNew.VplEndpId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify gre get vpl endpoint info fail: ", cfgNew.VplEndpId)
			return err, false
		}
		gre := &cfgCur.GreInfo
		greNew := &cfgNew.GreInfo
		greNew.Name = gre.Name
		greNew.EdgeId = gre.EdgeId
		greNew.Bandwidth = cfgNew.Bandwidth
		greNew.TunnelSrc = strings.Split(vplEndp.LocalAddress, "/")[0]
		greNew.TunnelDst = strings.Split(vplEndp.RemoteAddress, "/")[0]
		greNew.GreKey, _ = strconv.Atoi(strings.Split(cfgNew.Id, "-")[1])
		/* 如果是共享tunnel，则在vplEndp中创建gre */
		greNew.RootId = vplEndp.Id
		/* 默认开启 */
		greNew.HealthCheck = true
		err, chg = gre.Modify(greNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify gre tunnel fail: ", cfgCur.Id)
			return err, false
		}
	default:
		return derr.Error{In: err.Error(), Out: "TunnelTypeError"}, false
	}

	/* 如果pbr的优先级变化 */
	if cfgCur.Pbr && cfgCur.Priority != cfgNew.Priority {
		/* 如果是PBR模式，则下发基于源地址段的策略路由 */
		for _, cidr := range cfgCur.Cidr {
			err = public.VrfSetIpruleBySource(true, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgCur.Priority)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel delete iprule fail: ", cfgCur.TunnelName)
				return err, false
			}
		}

		for _, cidr := range cfgNew.Cidr {
			err = public.VrfSetIpruleBySource(false, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgNew.Priority)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel create iprule fail: ", cfgCur.TunnelName)
				return err, false
			}
		}
		cfgCur.Priority = cfgNew.Priority
		chg = true
	}

	/* 设置Cidr路由 */
	switch cfgCur.Type {
	case TunnelType_Veth:
		/* 比较edgeId 和 edgePeerId, 不能被修改 */
		if cfgCur.EdgeId != cfgNew.EdgeId {
			agentLog.AgentLogger.Info(err, "Tunnel Edge ID cannot change.", cfgCur.EdgeId)
			return err, false
		} else if cfgCur.EdgeIdPeer != cfgNew.EdgeIdPeer {
			agentLog.AgentLogger.Info(err, "Tunnel EdgePeer ID cannot change.", cfgCur.EdgeIdPeer)
			return err, false
		}

		/* 比较Cidr是否变化，如果变化，则更新EdgePeer配置信息 */
		add, delete := public.Arrcmp(cfgCur.Cidr, cfgNew.Cidr)
		if len(add) != 0 || len(delete) != 0 {
			/*获取tunnel对应的EdgePeer信息*/
			err, edgePeer := GetEdgeInfoById(cfgCur.EdgeIdPeer)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel get EdgePeer info fail: ", cfgCur.EdgeIdPeer)
				return err, false
			}

			if cfgCur.Pbr {
				/* 如果是PBR模式，则下发基于源地址段的策略路由 */
				for _, cidr := range delete {
					err = public.VrfSetIpruleBySource(true, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgCur.Priority)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel delete iprule fail: ", cfgCur.TunnelName)
						return err, false
					}
				}

				for _, cidr := range add {
					err = public.VrfSetIpruleBySource(false, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgNew.Priority)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel create iprule fail: ", cfgCur.TunnelName)
						return err, false
					}
				}
			} else {
				/* 填充static实例配置，并创建 */
				StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, Device: cfgCur.VethInfo.LocalAddress /* 使用对端地址作为nexthop */, Cidr: cfgNew.Cidr}
				StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, Device: cfgCur.VethInfo.LocalAddress /* 使用对端地址作为nexthop */, Cidr: cfgCur.Cidr}
				err, _ = StaticInfoCur.Modify(StaticInfoNew)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel modify static fail: ", cfgCur.TunnelName)
					return err, false
				}
			}
			/* 获取EdgePeer是否存在Conn */
			find, conn := GetConnInfoByEdgeId(cfgCur.EdgeIdPeer)
			if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
				/* 填充bgp实例配置，并向connection宣告bgp */
				bgpInfo := &BgpConf{Id: conn.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, PeerAs: edgePeer.PeerAs}
				err, _ = bgpInfo.Announce(cfgCur.Cidr, cfgNew.Cidr)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", cfgCur.TunnelName)
					return err, false
				}

				/* 获取除掉self的所有tunnel的cidrpeer */
				exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edgePeer.Id, cfgCur.Id)
				if len(exculeselfCidrs) != 0 {
					/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
					var cdirOld []string
					err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", cfgCur.TunnelName)
						return err, false
					}
				}
			}
			chg = true
			cfgCur.Cidr = cfgNew.Cidr
		}

		/* 比较CidrPeer是否变化，如果变化，则更新Edge配置信息 */
		add, delete = public.Arrcmp(cfgCur.CidrPeer, cfgNew.CidrPeer)
		if len(add) != 0 || len(delete) != 0 {
			/*获取tunnel对应的Edge信息*/
			err, edge := GetEdgeInfoById(cfgCur.EdgeId)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", cfgCur.EdgeId)
				return err, false
			}

			/* 填充static实例配置，并创建 */
			StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.VethInfo.RemoteAddress /* 使用对端地址作为nexthop */, Cidr: cfgNew.CidrPeer}
			StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.VethInfo.RemoteAddress /* 使用对端地址作为nexthop */, Cidr: cfgCur.CidrPeer}
			err, _ = StaticInfoCur.Modify(StaticInfoNew)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel modify static fail: ", cfgCur.TunnelName)
				return err, false
			}

			/* 获取Edge是否存在Conn */
			find, conn := GetConnInfoByEdgeId(cfgCur.EdgeId)
			if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
				/* 填充bgp实例配置，并向connection宣告bgp */
				bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
				err, _ = bgpInfo.Announce(cfgCur.CidrPeer, cfgNew.CidrPeer)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", cfgCur.TunnelName)
					return err, false
				}

				/* 获取除掉self的所有tunnel的cidrpeer */
				exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edge.Id, cfgCur.Id)
				if len(exculeselfCidrs) != 0 {
					/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
					var cdirOld []string
					err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", cfgCur.TunnelName)
						return err, false
					}
				}
			}
			chg = true
			cfgCur.CidrPeer = cfgNew.CidrPeer
		}
	case TunnelType_Gre, TunnelType_Vpl:
		/* 获取conn对应的Edge信息 */
		err, edge := GetEdgeInfoById(cfgCur.EdgeId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", cfgCur.EdgeId)
			return err, false
		}

		/* 比较Cidr是否变化，如果变化，则更新Cidr配置信息 */
		add, delete := public.Arrcmp(cfgCur.Cidr, cfgNew.Cidr)
		if len(add) != 0 || len(delete) != 0 {
			if cfgCur.Pbr {
				/* 如果是PBR模式，则下发基于源地址段的策略路由 */
				for _, cidr := range delete {
					err = public.VrfSetIpruleBySource(true, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgCur.Priority)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel delete iprule fail: ", cfgCur.TunnelName)
						return err, false
					}
				}

				for _, cidr := range add {
					err = public.VrfSetIpruleBySource(false, cfgCur.EdgeId, cidr, cfgCur.TableId, cfgNew.Priority)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel create iprule fail: ", cfgCur.TunnelName)
						return err, false
					}
				}
			}
			chg = true
			cfgCur.Cidr = cfgNew.Cidr
		}

		add, delete = public.Arrcmp(cfgCur.CidrPeer, cfgNew.CidrPeer)
		if len(add) != 0 || len(delete) != 0 {
			if !cfgCur.Pbr {
				/* 比较CidrPeer是否变化，如果变化，则更新tunnel路由策略 */
				StaticInfoNew := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.TunnelName, Cidr: cfgNew.CidrPeer}
				StaticInfoCur := &StaticConf{Id: cfgCur.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: cfgCur.TunnelName, Cidr: cfgCur.CidrPeer}
				err, _ := StaticInfoCur.Modify(StaticInfoNew)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel modify static fail: ", cfgCur.TunnelName)
					return err, false
				}
			}

			/* 获取Edge是否存在Conn */
			find, conn := GetConnInfoByEdgeId(cfgCur.EdgeId)
			if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
				/* 填充bgp实例配置，并向connection宣告bgp */
				bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
				err, _ = bgpInfo.Announce(cfgCur.CidrPeer, cfgNew.CidrPeer)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", cfgCur.TunnelName)
					return err, false
				}

				/* 获取除掉self的所有tunnel的cidrpeer */
				exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edge.Id, cfgCur.Id)
				if len(exculeselfCidrs) != 0 {
					/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
					var cdirOld []string
					err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
					if err != nil {
						agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", cfgCur.TunnelName)
						return err, false
					}
				}
			}
			chg = true
			cfgCur.CidrPeer = cfgNew.CidrPeer
		}
	default:
		return derr.Error{In: err.Error(), Out: "TunnelTypeError"}, false
	}

	return nil, chg
}

func (conf *TunnelConf) Destroy() error {

	var err error

	/* 删除Cdir路由 */
	switch conf.Type {
	case TunnelType_Veth:
		/* 获取tunnel对应的Edge信息 */
		err, edge := GetEdgeInfoById(conf.EdgeId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", conf.EdgeId)
			return err
		}

		if conf.Pbr {
			/* 如果是PBR模式，则删除基于源地址段的策略路由 */
			for _, cidr := range conf.Cidr {
				err = public.VrfSetIpruleBySource(true, conf.EdgeId, cidr, conf.TableId, conf.Priority)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel delete iprule fail: ", conf.TunnelName)
					return err
				}
			}

			err, _ := VrfIpRouteWithTable(true, "0.0.0.0/0", conf.VethInfo.RemoteAddress, conf.TableId, edge.Id)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel delete vrfIpRouteTable fail: ", conf.TunnelName)
				return err
			}
		} else {
			/* 填充static实例配置，并删除 */
			StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.VethInfo.RemoteAddress /* 使用对端地址作为nexthop */, Cidr: conf.CidrPeer}
			err = StaticInfo.Destroy()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel destroy static fail: ", conf.TunnelName)
				return err
			}
		}

		/* 获取Edge是否存在Conn */
		find, conn := GetConnInfoByEdgeId(conf.EdgeId)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
			if len(conf.CidrPeer) != 0 {
				var cdirNew []string
				err, _ = bgpInfo.Announce(conf.CidrPeer, cdirNew)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}

			/* 获取除掉self的所有tunnel的cidrpeer */
			exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edge.Id, conf.Id)
			if len(exculeselfCidrs) != 0 {
				/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}

		/* 获取tunnel对应的EdgePeer信息 */
		err, edgePeer := GetEdgeInfoById(conf.EdgeIdPeer)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get EdgePeer info fail: ", conf.EdgeIdPeer)
			return err
		}

		/* 填充static实例配置，并删除 */
		StaticInfo := &StaticConf{Id: conf.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, Device: conf.VethInfo.LocalAddress /* 使用对端地址作为nexthop */, Cidr: conf.Cidr}
		err = StaticInfo.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel destroy static fail: ", conf.TunnelName)
			return err
		}

		/* 获取EdgePeer是否存在Conn */
		find, conn = GetConnInfoByEdgeId(conf.EdgeIdPeer)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edgePeer.Id, LocalAs: edgePeer.LocalAs, PeerAs: edgePeer.PeerAs}
			if len(conf.Cidr) != 0 {
				var cdirNew []string
				err, _ = bgpInfo.Announce(conf.Cidr, cdirNew)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}

			/* 获取除掉self的所有tunnel的cidrpeer */
			exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edgePeer.Id, conf.Id)
			if len(exculeselfCidrs) != 0 {
				/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}
	case TunnelType_Gre, TunnelType_Vpl:
		/* 获取tunnel对应的Edge信息 */
		err, edge := GetEdgeInfoById(conf.EdgeId)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel get Edge info fail: ", conf.EdgeId)
			return err
		}

		if conf.Pbr {
			/* 如果是PBR模式，则删除基于源地址段的策略路由 */
			for _, cidr := range conf.Cidr {
				err = public.VrfSetIpruleBySource(true, conf.EdgeId, cidr, conf.TableId, conf.Priority)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel delete iprule fail: ", conf.TunnelName)
					return err
				}
			}
			err, _ := VrfIpRouteWithTable(true, "0.0.0.0/0", conf.TunnelName, conf.TableId, edge.Id)
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel delete vrfIpRouteTable fail: ", conf.TunnelName)
				return err
			}
		} else {
			/* 填充static实例配置，并删除 */
			StaticInfo := &StaticConf{Id: conf.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, Device: conf.TunnelName, Cidr: conf.CidrPeer}
			err = StaticInfo.Destroy()
			if err != nil {
				agentLog.AgentLogger.Info(err, "Tunnel destroy static fail: ", conf.TunnelName)
				return err
			}
		}

		/* 获取Edge是否存在Conn */
		find, conn := GetConnInfoByEdgeId(conf.EdgeId)
		if find && conn.RouteInfo.Type == ConnRouteType_Bgp {
			/* 填充bgp实例配置，并向connection宣告bgp */
			bgpInfo := &BgpConf{Id: conn.Id, Vrf: edge.Id, LocalAs: edge.LocalAs, PeerAs: edge.PeerAs}
			if len(conf.CidrPeer) != 0 {
				var cdirNew []string
				err, _ = bgpInfo.Announce(conf.CidrPeer, cdirNew)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce bgp fail: ", conf.TunnelName)
					return err
				}
			}

			/* 获取除掉self的所有tunnel的cidrpeer */
			exculeselfCidrs := GetTunnelCidrsByEdgeIdExculeself(edge.Id, conf.Id)
			if len(exculeselfCidrs) != 0 {
				/* 宣告bgp exculeselfCidrs,补齐其它Tunnel重叠的Cidrs */
				var cdirOld []string
				err, _ = bgpInfo.Announce(cdirOld, exculeselfCidrs)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Tunnel announce exculeself bgp fail: ", conf.TunnelName)
					return err
				}
			}
		}
	default:
		return derr.Error{In: err.Error(), Out: "TunnelTypeError"}
	}

	/* 删除tunnel实例 */
	switch conf.Type {
	case TunnelType_Veth:
		veth := &conf.VethInfo
		err := veth.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel veth Destroy fail: ", conf.Id)
			return err
		}
	case TunnelType_Gre, TunnelType_Vpl:
		gre := &conf.GreInfo
		err := gre.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Tunnel gre Destroy fail: ", conf.Id)
			return err
		}
	default:
	}

	return nil
}

func GetTunnelInfoById(id string) (error, TunnelConf) {

	var find = false
	var tunn TunnelConf
	paths := []string{config.TunnelConfPath}
	tunns, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.TunnelConfPath not found: ", err.Error())
	} else {
		for _, value := range tunns {
			bytes := []byte(value)
			fp := &TunnelConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			tunn = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "TunnelotFound"}, tunn
	}

	return nil, tunn
}

func GetTunnelCidrsByEdgeId(edgeId string) []string {

	var cidrs []string
	paths := []string{config.TunnelConfPath}
	tunnels, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range tunnels {
			bytes := []byte(value)
			fp := &TunnelConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.EdgeId == edgeId {
				for _, cidr := range fp.CidrPeer {
					cidrs = append(cidrs, cidr)
				}
			} else if fp.EdgeIdPeer == edgeId {
				for _, cidr := range fp.Cidr {
					cidrs = append(cidrs, cidr)
				}
			}
		}
	}

	return cidrs
}

func GetTunnelCidrsByEdgeIdExculeself(edgeId, tunnelId string) []string {

	var cidrs []string
	paths := []string{config.TunnelConfPath}
	tunnels, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range tunnels {
			bytes := []byte(value)
			fp := &TunnelConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id == tunnelId {
				continue
			}

			if fp.EdgeId == edgeId {
				for _, cidr := range fp.CidrPeer {
					cidrs = append(cidrs, cidr)
				}
			} else if fp.EdgeIdPeer == edgeId {
				for _, cidr := range fp.Cidr {
					cidrs = append(cidrs, cidr)
				}
			}
		}
	}

	return cidrs
}
