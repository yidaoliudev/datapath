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

const ()

/*
概述：Edge作为租户在PoP上的工作站点，独占一个namespace，并提供分支接入的connection连接服务，以及与骨干互联的租户隧道网络。
*/

type EdgeConf struct {
	Id      string `json:"id"`      //(必填)以edge-0001 形式定义id
	LocalAs uint64 `json:"localAs"` //(必填)Edge本端BGP As Number
	PeerAs  uint64 `json:"peerAs"`  //(必填)Edge对端端BGP As Number
}

func (conf *EdgeConf) Create(action int) error {

	/* 创建ns */
	cmdstr := fmt.Sprintf("ip netns add %s", conf.Id)
	err, _ := public.ExecBashCmdWithRet(cmdstr)
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

	return nil
}

func (cfgCur *EdgeConf) Modify(cfgNew *EdgeConf) (error, bool) {

	var chg = false

	/* 获取Edge是否存在Conn */
	find, conn := GetConnInfoByEdgeId(cfgCur.Id)

	if !find || conn.RouteInfo.Type == ConnRouteType_Static {
		/* 如果edge没有conn实例，或者关联的conn实例的路由方式为staic，则直接更新AS配置即可。 */
		if cfgCur.LocalAs != cfgNew.LocalAs || cfgCur.PeerAs != cfgNew.PeerAs {
			cfgCur.LocalAs = cfgNew.LocalAs
			cfgCur.PeerAs = cfgNew.PeerAs
			chg = true
		}
	} else {
		/* 如果edge关联的conn实例为bgp方式，则需要重新创建bgp邻居，并宣告所有tunnel的cidr信息。 */
		if cfgCur.LocalAs != cfgNew.LocalAs || cfgCur.PeerAs != cfgNew.PeerAs {
			bgpCur := &conn.RouteInfo.BgpInfo
			/* 填充bgp实例配置，修改bgp邻居 */
			bgpInfoNew := &BgpConf{Id: conn.Id, Vrf: cfgCur.Id, LocalAs: cfgNew.LocalAs, PeerAs: cfgNew.PeerAs, PeerAddress: conn.RemoteAddress, RouterId: conn.LocalAddress, KeepAlive: bgpCur.KeepAlive, HoldTime: bgpCur.HoldTime, Password: bgpCur.Password}
			bgpInfoCur := &BgpConf{Id: conn.Id, Vrf: cfgCur.Id, LocalAs: cfgCur.LocalAs, PeerAs: cfgCur.PeerAs, PeerAddress: conn.RemoteAddress, RouterId: conn.LocalAddress, KeepAlive: bgpCur.KeepAlive, HoldTime: bgpCur.HoldTime, Password: bgpCur.Password}
			err, _ := bgpInfoCur.Modify(bgpInfoNew)
			if err != nil {
				agentLog.AgentLogger.Info(err, "edge Modify bgp fail: ", cfgCur.Id)
				return err, false
			}

			/* 获取所有tunnel的cidrpeer */
			cidrPeers := GetTunnelCidrsByEdgeId(cfgCur.Id)
			if len(cidrPeers) != 0 {
				/* 宣告bgp cidrs */
				var cdirOld []string
				err, _ := bgpInfoNew.Announce(cdirOld, cidrPeers)
				if err != nil {
					agentLog.AgentLogger.Info(err, "edge Announce bgp fail: ", cfgCur.Id)
					return err, false
				}
			}

			cfgCur.LocalAs = cfgNew.LocalAs
			cfgCur.PeerAs = cfgNew.PeerAs
			chg = true
		}
	}

	return nil, chg
}

func (conf *EdgeConf) Destroy() error {

	/* del namespace */
	cmdstr := fmt.Sprintf("ip netns del %s", conf.Id)
	err, _ := public.ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("Destroy edge cmd: ", cmdstr, err)
	if err != nil {
		return err
	}

	cmd_nht := set_nht(true, conf.Id)
	err = public.ExecBashCmd(strings.Join(cmd_nht, " "))
	agentLog.AgentLogger.Info("Destroy edge cmd_nht: ", cmd_nht, err)
	if err != nil {
		return err
	}
	return nil
}

func GetEdgeInfoById(id string) (error, EdgeConf) {

	var find = false
	var edge EdgeConf
	paths := []string{config.EdgeConfPath}
	edges, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.EdgeConfPath not found: ", err.Error())
	} else {
		for _, value := range edges {
			bytes := []byte(value)
			fp := &EdgeConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			edge = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "EdgeNotFound"}, edge
	}

	return nil, edge
}
