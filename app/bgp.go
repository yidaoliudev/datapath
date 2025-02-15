package app

import (
	"context"
	"datapath/agentLog"
	"datapath/config"
	mceetcd "datapath/etcd"
	"datapath/public"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/log"
)

var (
	bgpMonitorInterval int
)

const (
	maxRcvPrefix               = 1000
	cmd_vtysh                  = "vtysh"
	cmd_vtysh_op_c             = "-c"
	cmd_reverse                = "no"
	BgpAttribute_Med           = "Med"
	BgpAttribute_As_Path       = "AsPaths"
	RouteMapActionPermit       = "permit"
	RouteMapActionDeny         = "deny"
	routemap_type_root         = "root"
	routemap_type_default      = "default"
	routemap_type_user         = "user"
	bgpPolicyDirectOut         = "out"
	bgpPolicyDirectIn          = "in"
	cmd_GotoVrf                = "vrf %s"
	cmd_SetIpNht               = "ip nht resolve-via-default"
	cmd_showVrfNeigh           = "show ip bgp vrf %s neighbors %s json"
	cmd_showVrfRcvRoutes       = "show ip bgp vrf %s neighbors %s routes json"
	cmd_showroute              = "show ip bgp json"
	cmd_showBgpSumm            = "show ip bgp summary json"
	cmd_showRoute              = "show ip route json"
	cmd_showVrfRoute           = "show ip route vrf %s json"
	cmd_configTerm             = "configure terminal"
	cmd_familyipv4             = "address-family ipv4 unicast"
	cmd_routercreate           = "router bgp %d"
	cmd_routerVrfCreate        = "router bgp %v vrf %s"
	cmd_routerid               = "bgp router-id %s"
	cmd_clusterid              = "bgp cluster-id %s"
	cmd_neighadd               = "neighbor %s remote-as %d"
	cmd_neighdes               = "neighbor %s description %s"
	cmd_neighPass              = "neighbor %s password %s"
	cmd_neighHoldTimeKeepAlive = "neighbor %s timers %d %d"
	cmd_neighreflector         = "neighbor %s route-reflector-client"
	cmd_dowrite                = "do write"
	cmd_neighMaxPrefix         = "neighbor %s maximum-prefix %d"
	cmd_networkPrefix          = "network %s"
	cmd_neighRouteMap          = "neighbor %s route-map %s %s"
	cmd_ebgpMultihop           = "neighbor %s ebgp-multihop %d" // config ebgp-multihop to 255
	cmd_redistributeKennel     = "redistribute kernel"
	cmd_RouteMap               = "route-map %s %s %d"
	cmd_callRouteMap           = "call %s"
	cmd_OnMathNext             = "on-match next"
	cmd_PrependAsPath          = "set as-path prepend"
	cmd_setMetric              = "set metric %s"
	cmd_setMetricNone          = "no set metric"
	cmd_vrfIpRouteAdd          = "ip route %s %s vrf %s"
	cmd_vrfIpRouteOnlinkAdd    = "ip route %s %s %s onlink vrf %s"
	cmd_vrfIpRouteWithTableAdd = "ip route %s %s table %d vrf %s"
	cmd_IpRouteAdd             = "ip route %s %s"
	cmd_IpRouteWithTableAdd    = "ip route %s %s table %d"
	cmd_EbgpRequirePolicy      = "bgp ebgp-requires-policy"
	cmd_SuppressDuplicates     = "bgp suppress-duplicates"
	cmd_NetworkImportCheck     = "bgp network import-check"
	cmd_NeighborRouteMapOut    = "neighbor %s route-map %s out"
	cmd_NeighborBfd            = "neighbor %s bfd profile %s"
	cmd_NeighClear             = "clear ip bgp vrf %s %s"
)

const (
	BgpType_Conn   = 0
	BgpType_Tunnel = 1
)

/*
只有edge内才会使用bgp路由。
使用bgp功能模块有2种服务场景：
1， connection（gre/ipsec/eport）
2， tunnel （veth，gre）
*/

type BgpConf struct {
	Id          string `json:"id"`          //(必填)Id全局唯一
	Vrf         string `json:"vrf"`         //(必填)vrf Id
	LocalAs     uint64 `json:"localAs"`     //(必填)
	PeerAs      uint64 `json:"peerAs"`      //(必填)
	PeerAddress string `json:"peerAddress"` //(必填)
	RouterId    string `json:"routerId"`    //[选填]
	EbgpMutihop int    `json:"ebgpMutihop"` //[选填]
	KeepAlive   int    `json:"keepAlive"`   //[选填]
	HoldTime    int    `json:"holdTime"`    //[选填]
	MaxPerfix   int    `json:"maxPerfix"`   //[选填]
	Password    string `json:"password"`    //[选填]
}

type neighborStatus struct {
	peer    *peerInfo
	routes  *routesInfo
	ageFlag bool
}

type peerInfo struct {
	bgpConfId string
	remoteAs  uint64
	remoteIp  string
	BgpState  string `json:"bgpState"`
	chgFlag   bool
}

type routesInfo struct {
	VrfName string                   `json:"vrfName"`
	Routes  map[string][]routeEffect `json:"routes"`
	chgFlag bool
}

type reportBgpInfo struct {
	///	BgpConfId       string       `json:"id"`
	Status          string       `json:"status"`
	BgpRouteDetails []routerinfo `json:"details"`
}

type routerinfo struct {
	Network string `json:"prefix"`
	Status  string `json:"valid"`
	NextHop string `json:"nextHop"`
	AsPath  string `json:"asPath"`
	Metric  int    `json:"metric"`
}

type routeEffect struct {
	Valid    bool           `json:"valid"`
	Bestpath bool           `json:"bestpath"`
	Metric   int            `json:"metric"`
	Network  string         `json:"network"`
	Aspath   string         `json:"path"`
	Nexthops []nexthopsInfo `json:"nexthops"`
}

type nexthopsInfo struct {
	Ip string `json:"ip"`
}

func cmdrNeighClear(vrf, peer string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(fmt.Sprintf(cmd_NeighClear, vrf, peer), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdrouterVrfcreate(undo bool, as uint64, vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_routerVrfCreate, as, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdEbgpRequirePolicy(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_EbgpRequirePolicy), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}
func cmdSuppressDuplicates(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_SuppressDuplicates), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}
func cmdNetworkImportCheck(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_NetworkImportCheck), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighNetworkRoutes(undo bool, prefix string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_networkPrefix, prefix), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdfamilyipv4() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_familyipv4, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdGotoVrf(vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(fmt.Sprintf(cmd_GotoVrf, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdSetIpNht(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_SetIpNht), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func set_nht(undo bool, namespaceId string) []string {
	cmd_nht := []string{cmd_vtysh}
	cmd_nht = append(cmd_nht, cmdconfigTerm()...)
	cmd_nht = append(cmd_nht, cmdGotoVrf(namespaceId)...)
	cmd_nht = append(cmd_nht, cmdSetIpNht(undo)...)
	cmd_nht = append(cmd_nht, cmddowrite()...)

	return cmd_nht
}

func cmdneighRouteMapOut(undo bool, neighbor string, routeMap string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_NeighborRouteMapOut, neighbor, routeMap), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighBfd(undo bool, neighbor string, routeMap string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_NeighborBfd, neighbor, routeMap), " ")

	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighadd(undo bool, ip string, as uint64) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighadd, ip, as), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdRouterid(undo bool, routerid string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_routerid, routerid), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdEbgpMultiHop(undo bool, ip string, ebgpMultihop int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_ebgpMultihop, ip, ebgpMultihop), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighPasswd(undo bool, ip string, passwd string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighPass, ip, passwd), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighKeepAliveHoldTime(ip string, keepAlive int, holdTime int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(fmt.Sprintf(cmd_neighHoldTimeKeepAlive, ip, keepAlive, holdTime), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd

}

func cmdneighMaxPrefix(undo bool, ip string, maxPrefix int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighMaxPrefix, ip, maxPrefix), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdconfigTerm() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_configTerm, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmddowrite() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_dowrite, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func CmdShowBgpNeigh(vrf string, neigh string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "" {
		cmd = append(cmd, cmd_showBgpSumm)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfNeigh, vrf, neigh))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func CmdShowRoute(vrf string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "default" {
		cmd = append(cmd, cmd_showRoute)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfRoute, vrf))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func cmdShowBgpRoutes(vrf string, neigh string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "" {
		cmd = append(cmd, cmd_showroute)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfRcvRoutes, vrf, neigh))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func cmdIpRouteWithTable(undo bool, prefix string, device string, tableId int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_IpRouteWithTableAdd, prefix, device, tableId), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdIpRoute(undo bool, prefix string, device string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_IpRouteAdd, prefix, device), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdVrfIpRouteWithTable(undo bool, prefix string, device string, tableId int, vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_vrfIpRouteWithTableAdd, prefix, device, tableId, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdVrfIpRoute(undo bool, prefix string, device string, vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_vrfIpRouteAdd, prefix, device, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdVrfIpRouteOnlink(undo bool, prefix string, nexthop string, device string, vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_vrfIpRouteOnlinkAdd, prefix, nexthop, device, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func BgpAsVrfConf(undo bool, loalAs uint64, vrf string) error {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdrouterVrfcreate(undo, loalAs, vrf)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("BgpAsVrfConf cmd: ", cmd, err)
	if err != nil {
		return err
	}
	return nil
}

func cmdSetStaticBgp(conf *StaticConf, undo bool) []string {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	for _, cidr := range conf.Cidr {
		cmd = append(cmd, cmdVrfIpRoute(undo, cidr, conf.Device, conf.Vrf)...)
	}
	/*
		cmd = append(cmd, cmdrouterVrfcreate(false, conf.LocalAs, conf.Vrf)...)
		cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
		cmd = append(cmd, cmdSuppressDuplicates(true)...)
		cmd = append(cmd, cmdNetworkImportCheck(true)...)
		cmd = append(cmd, cmdfamilyipv4()...)

		//不宣告
		for _, cidr := range conf.Cidr {
			cmd = append(cmd, cmdneighNetworkRoutes(undo, cidr)...)
		}
	*/
	cmd = append(cmd, cmddowrite()...)

	return cmd
}

func cmdSetStaticOnlink(conf *StaticConf, undo bool) []string {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	for _, cidr := range conf.Cidr {
		cmd = append(cmd, cmdVrfIpRouteOnlink(undo, cidr, conf.Device, conf.Id, conf.Vrf)...)
	}

	cmd = append(cmd, cmddowrite()...)

	return cmd
}

func cmdAnnounceBgpNetwork(conf *BgpConf, add, delete []string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	cmd = append(cmd, cmdrouterVrfcreate(false, conf.LocalAs, conf.Vrf)...)
	cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
	cmd = append(cmd, cmdSuppressDuplicates(true)...)
	cmd = append(cmd, cmdNetworkImportCheck(true)...)
	cmd = append(cmd, cmdfamilyipv4()...)

	//宣告网段
	for _, cidr := range delete {
		cmd = append(cmd, cmdneighNetworkRoutes(true, cidr)...)
	}

	for _, cidr := range add {
		cmd = append(cmd, cmdneighNetworkRoutes(false, cidr)...)
	}

	cmd = append(cmd, cmddowrite()...)

	return cmd
}

func cmdConfigBgpNeigh(conf *BgpConf, undo bool) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdrouterVrfcreate(false, conf.LocalAs, conf.Vrf)...)
	cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
	cmd = append(cmd, cmdSuppressDuplicates(true)...)
	cmd = append(cmd, cmdNetworkImportCheck(true)...)
	cmd = append(cmd, cmdneighadd(undo, conf.PeerAddress, conf.PeerAs)...)

	if conf.RouterId != "" {
		cmd = append(cmd, cmdRouterid(undo, conf.RouterId)...)
	}
	if conf.EbgpMutihop != 0 {
		cmd = append(cmd, cmdEbgpMultiHop(undo, conf.PeerAddress, conf.EbgpMutihop)...)
	} else {
		cmd = append(cmd, cmdEbgpMultiHop(undo, conf.PeerAddress, 255)...)
	}
	if conf.Password != "" {
		cmd = append(cmd, cmdneighPasswd(undo, conf.PeerAddress, conf.Password)...)
	}
	if conf.KeepAlive != 0 && conf.HoldTime != 0 {
		cmd = append(cmd, cmdneighKeepAliveHoldTime(conf.PeerAddress, conf.KeepAlive, conf.HoldTime)...)
	}

	cmd = append(cmd, cmdfamilyipv4()...)
	if conf.MaxPerfix != 0 {
		cmd = append(cmd, cmdneighMaxPrefix(undo, conf.PeerAddress, conf.MaxPerfix)...)
	} else {
		cmd = append(cmd, cmdneighMaxPrefix(undo, conf.PeerAddress, maxRcvPrefix)...)
	}

	cmd = append(cmd, cmddowrite()...)

	return cmd
}

func ModifyBgpConf(cfgCur *BgpConf, cfgNew *BgpConf) (bool, []string) {

	var chg = false
	var localasChg = false
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	if cfgCur.LocalAs != cfgNew.LocalAs {
		localasChg = true
		cmd = append(cmd, cmdrouterVrfcreate(true, cfgCur.LocalAs, cfgCur.Vrf)...)
	}
	cmd = append(cmd, cmdrouterVrfcreate(false, cfgNew.LocalAs, cfgCur.Vrf)...)
	cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
	cmd = append(cmd, cmdSuppressDuplicates(true)...)
	cmd = append(cmd, cmdNetworkImportCheck(true)...)

	/* 邻居地址改变 将旧的邻居删除，然后配置新配置*/
	if localasChg || cfgCur.PeerAddress != cfgNew.PeerAddress {
		chg = true
		if !localasChg {
			cmd = append(cmd, cmdneighadd(true, cfgCur.PeerAddress, cfgCur.PeerAs)...)
		}
		cmd = append(cmd, cmdneighadd(false, cfgNew.PeerAddress, cfgNew.PeerAs)...)

		if cfgNew.RouterId != "" {
			cmd = append(cmd, cmdRouterid(false, cfgNew.RouterId)...)
		}

		if cfgNew.EbgpMutihop != 0 {
			cmd = append(cmd, cmdEbgpMultiHop(false, cfgNew.PeerAddress, cfgNew.EbgpMutihop)...)
		} else {
			cmd = append(cmd, cmdEbgpMultiHop(false, cfgNew.PeerAddress, 255)...)
		}

		if cfgNew.Password != "" {
			cmd = append(cmd, cmdneighPasswd(false, cfgNew.PeerAddress, cfgNew.Password)...)
		}

		if cfgNew.KeepAlive != 0 && cfgNew.HoldTime != 0 {
			cmd = append(cmd, cmdneighKeepAliveHoldTime(cfgNew.PeerAddress, cfgNew.KeepAlive, cfgNew.HoldTime)...)
		}

		if cfgNew.MaxPerfix != 0 {
			cmd = append(cmd, cmdneighMaxPrefix(false, cfgNew.PeerAddress, cfgNew.MaxPerfix)...)
		} else {
			cmd = append(cmd, cmdneighMaxPrefix(false, cfgNew.PeerAddress, maxRcvPrefix)...)
		}

	} else {
		if cfgCur.RouterId != cfgNew.RouterId {
			chg = true
			if cfgCur.RouterId != "" {
				cmd = append(cmd, cmdRouterid(true, cfgNew.RouterId)...)
			}

			if cfgNew.RouterId != "" {
				cmd = append(cmd, cmdRouterid(false, cfgNew.RouterId)...)
			}
		}

		if cfgCur.PeerAs != cfgNew.PeerAs {
			chg = true
			cmd = append(cmd, cmdneighadd(false, cfgCur.PeerAddress, cfgNew.PeerAs)...)
		}

		if cfgCur.EbgpMutihop != cfgNew.EbgpMutihop {
			chg = true
			cmd = append(cmd, cmdEbgpMultiHop(false, cfgNew.PeerAddress, cfgNew.EbgpMutihop)...)
		}

		if cfgCur.Password != cfgNew.Password {
			chg = true
			if cfgCur.Password != "" {
				cmd = append(cmd, cmdneighPasswd(true, cfgNew.PeerAddress, cfgNew.Password)...)
			}

			if cfgNew.Password != "" {
				cmd = append(cmd, cmdneighPasswd(false, cfgNew.PeerAddress, cfgNew.Password)...)
			}
		}

		if cfgCur.KeepAlive != cfgNew.KeepAlive || cfgCur.HoldTime != cfgNew.HoldTime {
			chg = true
			cmd = append(cmd, cmdneighKeepAliveHoldTime(cfgNew.PeerAddress, cfgNew.KeepAlive, cfgNew.HoldTime)...)
		}

		if cfgCur.MaxPerfix != cfgNew.MaxPerfix {
			chg = true
			if cfgCur.MaxPerfix != 0 {
				cmd = append(cmd, cmdneighMaxPrefix(true, cfgCur.PeerAddress, cfgCur.MaxPerfix)...)
			} else {
				cmd = append(cmd, cmdneighMaxPrefix(true, cfgCur.PeerAddress, maxRcvPrefix)...)
			}

			if cfgNew.MaxPerfix != 0 {
				cmd = append(cmd, cmdneighMaxPrefix(false, cfgNew.PeerAddress, cfgNew.MaxPerfix)...)
			} else {
				cmd = append(cmd, cmdneighMaxPrefix(false, cfgNew.PeerAddress, maxRcvPrefix)...)
			}
		}
	}

	cmd = append(cmd, cmddowrite()...)

	return chg, cmd
}

func BgpClear(vrf, peer string) []string {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdrNeighClear(vrf, peer)...)

	return cmd
}

/***
** Update bgp status to core.
** Start.
****/

func neighsStatusAgeStart(neighsStatus map[string]*neighborStatus) {
	for _, neighS := range neighsStatus {
		neighS.ageFlag = true
	}
}

func neighsStatusAgeEnd(neighsStatus map[string]*neighborStatus) {
	// 不考虑邻居的老化上报，配置层面邻居被删除认为是控制器删除的
	for bgpConfId, neighS := range neighsStatus {
		if neighS.ageFlag {
			delete(neighsStatus, bgpConfId)
		}
	}
}

func neighsStatusUpdate(connCfg *ConnConf, neighsStatus map[string]*neighborStatus, neighout string, routeout string) error {
	// 去除不需要的信息，上报最小单位信息
	neighStatus := make(map[string]peerInfo)
	if err := json.Unmarshal([]byte(neighout), &neighStatus); err != nil {
		return fmt.Errorf("unmarshal neighout %s to neighStatus %v err: %v", neighout, neighStatus, err)
	}

	routes := &routesInfo{}
	if err := json.Unmarshal([]byte(routeout), routes); err != nil {
		return fmt.Errorf("unmarshal routeout %s to routes %v err: %v", routeout, routes, err)
	}

	// 刷新当前状态信息
	for peerIp, peer := range neighStatus {

		peer.remoteIp = peerIp
		peer.bgpConfId = connCfg.Id
		///peer.remoteAs = connCfg.RouteInfo.BgpInfo.PeerAs
		curNeigh := neighsStatus[connCfg.Id]
		if curNeigh != nil {
			if curNeigh.peer.BgpState != peer.BgpState || curNeigh.peer.remoteIp != peer.remoteIp {
				curNeigh.peer.BgpState = peer.BgpState
				curNeigh.peer.remoteIp = peer.remoteIp
				curNeigh.peer.chgFlag = true
			}
		} else {
			neighsStatus[connCfg.Id] =
				&neighborStatus{&peer, &routesInfo{}, false}
			neighsStatus[connCfg.Id].peer.chgFlag = true
		}
		neighsStatus[connCfg.Id].ageFlag = false
	}

	curNeigh := neighsStatus[connCfg.Id]
	if curNeigh != nil {
		if curNeigh.routes != nil {
			if !reflect.DeepEqual(curNeigh.routes, routes) {
				curNeigh.routes = routes
				curNeigh.routes.chgFlag = true
			}
		} else {
			curNeigh.routes.chgFlag = true
			curNeigh.routes = routes
		}
		neighsStatus[connCfg.Id].ageFlag = false
	}

	return nil
}

func neighStatusReportCore(neighsStatus map[string]*neighborStatus) error {

	for bgpConfId, status := range neighsStatus {
		if status.peer.chgFlag || status.routes.chgFlag {
			var routes_tpm = &routerinfo{}
			var routes = make([]routerinfo, 0)
			for _, route := range status.routes.Routes {
				for _, tmp := range route {
					/* 如果Aspath过长，则取其中一段 */
					if len(tmp.Aspath) < 32 {
						routes_tpm.AsPath = tmp.Aspath
					} else {
						routes_tpm.AsPath = tmp.Aspath[:26] + "..."
					}
					routes_tpm.Network = tmp.Network
					routes_tpm.Metric = tmp.Metric
					routes_tpm.NextHop = tmp.Nexthops[0].Ip
					routes_tpm.Status = ""
					if tmp.Valid {
						routes_tpm.Status = routes_tpm.Status + "valid,"
					}

					if tmp.Bestpath {
						routes_tpm.Status = routes_tpm.Status + "best,"
					}

				}
				/* 去掉字符末尾逗号 */
				if len(routes_tpm.Status) > 0 {
					routes_tpm.Status = routes_tpm.Status[:len(routes_tpm.Status)-1]
				}

				routes = append(routes, *routes_tpm)
			}

			rptBgpInfo := reportBgpInfo{ /*bgpConfId,*/ status.peer.BgpState, routes}
			bytedata, err := json.Marshal(rptBgpInfo)
			if err != nil {
				agentLog.AgentLogger.Error("Marshal post core err:", err)
				return err
			}

			url := fmt.Sprintf("/api/dpConfig/connections/%s/bgp/status", bgpConfId)
			_, err = public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
			if err != nil {
				///agentLog.AgentLogger.Error("Marshal post core err:", err, rptBgpInfo)
				log.Warning("BGP neighbor status change, requestCore err:", err, rptBgpInfo)
			} else {
				status.peer.chgFlag = false
				status.routes.chgFlag = false
				agentLog.AgentLogger.Info("BGP neighbor status change, requestCore: ", bgpConfId, ", BgpInfo: ", rptBgpInfo)
			}
		}
	}

	return nil
}

func monitorBgpInfo(ctx context.Context) error {

	neighsStatus := make(map[string]*neighborStatus)

	tick := time.NewTicker(time.Duration(bgpMonitorInterval) * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			// 假如一次获取所有的vrf的bgp邻居和路由可能上报了额外的邻居状态和本地发起的路由，不使用本地数据库信息的话不好做区分
			var keys = []string{config.ConnConfPath}
			connEtcdMap, _, err := mceetcd.EtcdGetValuesWithCheck(keys)
			if err != nil {
				agentLog.AgentLogger.Error("monitorBgpInfo read vap etcd config failed: %v", err.Error())
				continue
			}

			neighsStatusAgeStart(neighsStatus)

			for _, connData := range connEtcdMap {

				connCfg := &ConnConf{}
				if err = json.Unmarshal([]byte(connData), connCfg); err != nil {
					agentLog.AgentLogger.Error("Unmarshal bgpData err: %v ", err)
					continue
				}

				if connCfg.RouteInfo.Type != ConnRouteType_Bgp {
					continue
				}

				idneigh := strings.Join(CmdShowBgpNeigh(connCfg.EdgeId, connCfg.RemoteAddress), " ")
				idroute := strings.Join(cmdShowBgpRoutes(connCfg.EdgeId, connCfg.RemoteAddress), " ")
				err, neighout := public.ExecBashCmdWithRet(idneigh)
				if err != nil {
					agentLog.AgentLogger.Error("show neigh exec attach err: %s", err)
					continue
				}

				err, routeout := public.ExecBashCmdWithRet(idroute)
				if err != nil {
					agentLog.AgentLogger.Error("show route exec attach err: %s", err)
					continue
				}

				if err := neighsStatusUpdate(connCfg, neighsStatus, neighout, routeout); err != nil {
					agentLog.AgentLogger.Error("neighsStatusUpdate err : %v", err, "Ns: ", connCfg.EdgeId, "ID: ", connCfg.Id, "peerIP: ", connCfg.RemoteAddress)
					continue
				}
			}
			neighsStatusAgeEnd(neighsStatus)
			// 上报控制器状态变化
			neighStatusReportCore(neighsStatus)
		case <-ctx.Done():
			// 必须返回成功
			return nil
		}
	}
}

func InitBgpMonitor() error {
	// 上报bgp邻居状态和路由
	ctx, _ := context.WithCancel(context.Background())
	go func() {
		for {
			defer func() {
				if err := recover(); err != nil {
					agentLog.AgentLogger.Error("bgp monitor panic err: %v", err)
				}
			}()

			if err := monitorBgpInfo(ctx); err == nil {
				return
			} else {
				agentLog.AgentLogger.Error("bgp monitor err :%v ", err)
			}
		}
	}()

	return nil
}

func SetBgpPara(monitorInterval int) {
	bgpMonitorInterval = monitorInterval
}

/***
** Update bgp status to core.
** END.
****/

func (conf *BgpConf) Create() error {

	var err error
	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist")
	}

	cmd := cmdConfigBgpNeigh(conf, false)
	err = public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("Create bgp cmd: ", cmd, err)
	if err != nil {
		return err
	}

	cmd_nht := set_nht(false, conf.Vrf)
	err = public.ExecBashCmd(strings.Join(cmd_nht, " "))
	agentLog.AgentLogger.Info("Create bgp cmd_nht: ", cmd_nht, err)
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *BgpConf) Modify(cfgNew *BgpConf) (error, bool) {

	if !public.NsExist(cfgCur.Vrf) {
		return errors.New("VrfNotExist"), false
	}

	chg, cmd := ModifyBgpConf(cfgCur, cfgNew)
	if chg {
		err := public.ExecBashCmd(strings.Join(cmd, " "))
		agentLog.AgentLogger.Info("Modify bgp cmd: ", cmd, err)
		if err != nil {
			agentLog.AgentLogger.Info("Modify bgp error")
			return err, false
		}
		return nil, true
	}

	return nil, false
}

func (conf *BgpConf) Destroy() error {

	err := BgpAsVrfConf(true, conf.LocalAs, conf.Vrf)
	if err != nil {
		return err
	}

	return nil
}

func (conf *BgpConf) Announce(cdirCur, cidrNew []string) (error, bool) {

	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist"), false
	}

	add, delete := public.Arrcmp(cdirCur, cidrNew)
	if len(add) == 0 && len(delete) == 0 {
		return nil, false
	}

	cmd := cmdAnnounceBgpNetwork(conf, add, delete)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("Announce bgp cmd: ", cmd, err)
	if err != nil {
		agentLog.AgentLogger.Info("Announce bgp error")
		return err, false
	}
	return nil, true

}
