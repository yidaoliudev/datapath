package app

import (
	"datapath/agentLog"
	"datapath/public"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type StaticConf struct {
	Id      string   `json:"id"`      //(必填)，Id全局唯一
	Vrf     string   `json:"vrf"`     //(必填)vrf Id
	Cidr    []string `json:"cidr"`    //(必填)，静态路由网段
	LocalAs uint64   `json:"localAs"` //{不填}
	Device  string   `json:"device"`  //{不填}
}

type routeInfo struct {
	Prefix   string        `json:"prefix"`
	Protocol string        `json:"protocol"`
	Selected bool          `json:"selected"`
	Distance int           `json:"distance"`
	Metric   int           `json:"metric"`
	Uptime   string        `json:"uptime"`
	Nexthops []nexthopInfo `json:"nexthops"`
}

type nexthopInfo struct {
	Nexthop string `json:"ip"`
	Device  string `json:"interfaceName"`
	Active  bool   `json:"active"`
}

func IpRouteWithTable(undo bool, destination string, device string, tableId int) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRouteWithTable(undo, destination, device, tableId)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteWithTable: ", cmd, " err:", err)
	return err, cmd
}

func IpRoute(undo bool, destination string, device string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRoute(undo, destination, device)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRoute: ", cmd)
	return err, cmd
}

func VrfIpRouteWithTable(undo bool, destination string, device string, tableId int, vrf string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdVrfIpRouteWithTable(undo, destination, device, tableId, vrf)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("VrfIpRouteWithTable: ", cmd, " err:", err)
	return err, cmd
}

func VrfIpRoute(undo bool, destination string, device string, vrf string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdVrfIpRoute(undo, destination, device, vrf)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("VrfIpRoute: ", cmd)
	return err, cmd
}

func VrfIpRouteOnlink(undo bool, destination string, nexthop string, device string, vrf string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdVrfIpRouteOnlink(undo, destination, nexthop, device, vrf)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("VrfIpRouteOnlink: ", cmd)
	return err, cmd
}

//根据用户输入的基础IP地址和CIDR掩码计算一个IP片段的区间
func GetIpSegRange(userSegIp, offset uint8) int {
	var ipSegMax uint8 = 255
	netSegIp := ipSegMax << offset
	segMinIp := netSegIp & userSegIp
	return int(segMinIp)
}

func GetIpSeg1Range(ipSegs []string, maskLen int) int {
	if maskLen > 8 {
		segIp, _ := strconv.Atoi(ipSegs[0])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[0])
	return GetIpSegRange(uint8(ipSeg), uint8(8-maskLen))
}

func GetIpSeg2Range(ipSegs []string, maskLen int) int {
	if maskLen > 16 {
		segIp, _ := strconv.Atoi(ipSegs[1])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[1])
	return GetIpSegRange(uint8(ipSeg), uint8(16-maskLen))
}

func GetIpSeg3Range(ipSegs []string, maskLen int) int {
	if maskLen > 24 {
		segIp, _ := strconv.Atoi(ipSegs[2])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[2])
	return GetIpSegRange(uint8(ipSeg), uint8(24-maskLen))
}

func GetIpSeg4Range(ipSegs []string, maskLen int) int {
	ipSeg, _ := strconv.Atoi(ipSegs[3])
	segMinIp := GetIpSegRange(uint8(ipSeg), uint8(32-maskLen))
	return segMinIp
}

func GetCidrIpRange(cidr string) string {
	ip := strings.Split(cidr, "/")[0]
	ipSegs := strings.Split(ip, ".")
	maskLen, _ := strconv.Atoi(strings.Split(cidr, "/")[1])
	seg1MinIp := GetIpSeg1Range(ipSegs, maskLen)
	seg2MinIp := GetIpSeg2Range(ipSegs, maskLen)
	seg3MinIp := GetIpSeg3Range(ipSegs, maskLen)
	seg4MinIp := GetIpSeg4Range(ipSegs, maskLen)

	return strconv.Itoa(seg1MinIp) + "." + strconv.Itoa(seg2MinIp) + "." + strconv.Itoa(seg3MinIp) + "." + strconv.Itoa(seg4MinIp)
}

func AddRoute(undo bool, local string, destination string, device string) error {

	lanMaskLen := 32
	dstMaskLen := 32
	dstMaskLen2 := 32
	var dstAddrcidr string
	var dstAddrcidr2 string

	localAddrcidr := public.GetCidrIpRange(local)
	if strings.Contains(local, "/") {
		lanMaskLen, _ = strconv.Atoi(strings.Split(local, "/")[1])
	}

	if strings.Contains(destination, "/") {
		dstMaskLen, _ = strconv.Atoi(strings.Split(destination, "/")[1])
		dstAddrcidr = public.GetCidrIpRange(destination)
		dstMaskLen2 = dstMaskLen
		dstAddrcidr2 = dstAddrcidr
	} else {
		dstMaskLen = lanMaskLen
		dstAddrcidr = public.GetCidrIpRange(fmt.Sprintf("%s/%d", destination, dstMaskLen))
		dstMaskLen2 = 32
		dstAddrcidr2 = public.GetCidrIpRange(destination)
	}

	if localAddrcidr != dstAddrcidr || lanMaskLen != dstMaskLen {
		err, _ := IpRoute(undo, fmt.Sprintf("%s/%d", dstAddrcidr2, dstMaskLen2), device)
		if err != nil {
			agentLog.AgentLogger.Info("AddRoute error")
			return err
		}
	}

	return nil
}

func VrfAddRoute(undo bool, local string, destination string, device string, vrf string) error {

	lanMaskLen := 32
	dstMaskLen := 32
	dstMaskLen2 := 32
	var dstAddrcidr string
	var dstAddrcidr2 string

	localAddrcidr := public.GetCidrIpRange(local)
	if strings.Contains(local, "/") {
		lanMaskLen, _ = strconv.Atoi(strings.Split(local, "/")[1])
	}

	if strings.Contains(destination, "/") {
		dstMaskLen, _ = strconv.Atoi(strings.Split(destination, "/")[1])
		dstAddrcidr = public.GetCidrIpRange(destination)
		dstMaskLen2 = dstMaskLen
		dstAddrcidr2 = dstAddrcidr
	} else {
		dstMaskLen = lanMaskLen
		dstAddrcidr = public.GetCidrIpRange(fmt.Sprintf("%s/%d", destination, dstMaskLen))
		dstMaskLen2 = 32
		dstAddrcidr2 = public.GetCidrIpRange(destination)
	}

	if localAddrcidr != dstAddrcidr || lanMaskLen != dstMaskLen {
		err, _ := VrfIpRoute(undo, fmt.Sprintf("%s/%d", dstAddrcidr2, dstMaskLen2), device, vrf)
		if err != nil {
			agentLog.AgentLogger.Info("VrfAddRoute error")
			return err
		}
	}

	return nil
}

func VrfAddRouteOnlink(undo bool, local string, destination string, nexthop string, device string, vrf string) error {

	lanMaskLen := 32
	dstMaskLen := 32
	dstMaskLen2 := 32
	var dstAddrcidr string
	var dstAddrcidr2 string

	localAddrcidr := public.GetCidrIpRange(local)
	if strings.Contains(local, "/") {
		lanMaskLen, _ = strconv.Atoi(strings.Split(local, "/")[1])
	}

	if strings.Contains(destination, "/") {
		dstMaskLen, _ = strconv.Atoi(strings.Split(destination, "/")[1])
		dstAddrcidr = public.GetCidrIpRange(destination)
		dstMaskLen2 = dstMaskLen
		dstAddrcidr2 = dstAddrcidr
	} else {
		dstMaskLen = lanMaskLen
		dstAddrcidr = public.GetCidrIpRange(fmt.Sprintf("%s/%d", destination, dstMaskLen))
		dstMaskLen2 = 32
		dstAddrcidr2 = public.GetCidrIpRange(destination)
	}

	if localAddrcidr != dstAddrcidr || lanMaskLen != dstMaskLen {
		err, _ := VrfIpRouteOnlink(undo, fmt.Sprintf("%s/%d", dstAddrcidr2, dstMaskLen2), nexthop, device, vrf)
		if err != nil {
			agentLog.AgentLogger.Info("VrfAddRouteOnlink error")
			return err
		}
	}

	return nil
}

func networkMod(conf *StaticConf, addArr []string, delArr []string) error {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	for _, add := range addArr {
		cmd = append(cmd, cmdVrfIpRoute(false, add, conf.Device, conf.Vrf)...)
	}

	for _, del := range delArr {
		cmd = append(cmd, cmdVrfIpRoute(true, del, conf.Device, conf.Vrf)...)
	}
	/*
		cmd = append(cmd, cmdrouterVrfcreate(false, conf.LocalAs, conf.Vrf)...)
		cmd = append(cmd, cmdfamilyipv4()...)

			//Conn路由不宣告
			for _, add := range addArr {
				cmd = append(cmd, cmdneighNetworkRoutes(false, add)...)
			}

			for _, del := range delArr {
				cmd = append(cmd, cmdneighNetworkRoutes(true, del)...)
			}
	*/
	cmd = append(cmd, cmddowrite()...)

	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("networkMod", cmd, err)
	if err != nil {
		return err
	}

	return nil
}

func networkModOnlink(conf *StaticConf, addArr []string, delArr []string) error {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)

	for _, add := range addArr {
		cmd = append(cmd, cmdVrfIpRouteOnlink(false, add, conf.Device, conf.Id, conf.Vrf)...)
	}

	for _, del := range delArr {
		cmd = append(cmd, cmdVrfIpRouteOnlink(true, del, conf.Device, conf.Id, conf.Vrf)...)
	}

	cmd = append(cmd, cmddowrite()...)

	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("networkModOnlink", cmd, err)
	if err != nil {
		return err
	}

	return nil
}

func (conf *StaticConf) Create() error {

	var err error
	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist")
	}

	cmd := cmdSetStaticBgp(conf, false)
	agentLog.AgentLogger.Info("cmdSetStaticBgp cmd:", cmd)
	err = public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}

	cmd_nht := set_nht(false, conf.Vrf)
	err = public.ExecBashCmd(strings.Join(cmd_nht, " "))
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *StaticConf) Modify(cfgNew *StaticConf) (error, bool) {

	if !public.NsExist(cfgCur.Vrf) {
		return errors.New("VrfNotExist"), false
	}

	add, delete := public.Arrcmp(cfgCur.Cidr, cfgNew.Cidr)
	if len(add) != 0 || len(delete) != 0 {
		/* change */
		if err := networkMod(cfgCur, add, delete); err != nil {
			return err, true
		}

		return nil, true
	}

	return nil, false
}

func (conf *StaticConf) Destroy() error {

	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist")
	}

	cmd := cmdSetStaticBgp(conf, true)
	agentLog.AgentLogger.Info("cmdSetStaticBgp cmd:", cmd)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}
	return nil
}

func (conf *StaticConf) CreateOnlink() error {

	var err error
	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist")
	}

	cmd := cmdSetStaticOnlink(conf, false)
	agentLog.AgentLogger.Info("cmdSetStaticOnlink cmd:", cmd)
	err = public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}

	cmd_nht := set_nht(false, conf.Vrf)
	err = public.ExecBashCmd(strings.Join(cmd_nht, " "))
	if err != nil {
		return err
	}

	return nil
}

func (cfgCur *StaticConf) ModifyOnlink(cfgNew *StaticConf) (error, bool) {

	if !public.NsExist(cfgCur.Vrf) {
		return errors.New("VrfNotExist"), false
	}

	add, delete := public.Arrcmp(cfgCur.Cidr, cfgNew.Cidr)
	if len(add) != 0 || len(delete) != 0 {
		/* change */
		if err := networkModOnlink(cfgCur, add, delete); err != nil {
			return err, true
		}

		return nil, true
	}

	return nil, false
}

func (conf *StaticConf) DestroyOnlink() error {

	if !public.NsExist(conf.Vrf) {
		return errors.New("VrfNotExist")
	}

	cmd := cmdSetStaticOnlink(conf, true)
	agentLog.AgentLogger.Info("cmdSetStaticOnlink cmd:", cmd)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}
	return nil
}

func GetRouterInfo(info string) []public.RouteInfo {

	var route_tpm = &public.RouteInfo{}
	var routes = make([]public.RouteInfo, 0)

	routeInfos := make(map[string][]routeInfo)
	if err := json.Unmarshal([]byte(info), &routeInfos); err != nil {
		return routes
	}

	for _, route := range routeInfos {
		for _, member := range route {
			route_tpm.Prefix = member.Prefix
			route_tpm.Protocol = member.Protocol
			route_tpm.Metric = member.Metric
			route_tpm.Distance = member.Distance
			route_tpm.Selected = member.Selected
			route_tpm.Uptime = member.Uptime

			route_tpm.Device = member.Nexthops[0].Device
			route_tpm.Nexthop = member.Nexthops[0].Nexthop
			route_tpm.Active = member.Nexthops[0].Active
			routes = append(routes, *route_tpm)
		}
	}

	return routes
}
