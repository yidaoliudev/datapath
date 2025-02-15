package public

import (
	"bytes"
	"crypto/tls"
	"datapath/agentLog"
	"datapath/etcd"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"
)

type HeartBeatInfo struct {
	Version string `json:"version"`
}

/* etcd并发保护锁 */
type NsEtcdLock struct {
	// 并发写map会出错, 使用sync.Map,key是vapid, value是mutex
	NsLock  sync.Map
	bgpLock sync.Map
}

var (
	EtcdLock NsEtcdLock
)

const (
	CPEAGENT_PORT      = 8788
	LINK_TYPE_IPSEC    = "IPSECVPN"
	RULEPRIO_MAX       = 32766
	COREINFO_PATH      = "/mnt/agent/core_info.json"
	ETCD_VAPSTATUSPATH = "/vapstatus"
	VAPOFFLINE         = "OFFLINE"
	VAPNORMAL          = "NORMAL"
	VAPUNKNOWN         = "UNKNOWN"
	ACTION_ADD         = 0
	ACTION_RECOVER     = 1
)

var (
	G_HeartBeatInfo HeartBeatInfo
	G_coreConf      BootConfig
)

type BootConfig struct {
	Sn          string `json:"Sn"`
	Cid         string `json:"Cid"`
	CoreAddress string `json:"CoreAddress"`
	CoreProto   string `json:"CoreProto"`
	CorePort    int    `json:"CorePort"`
}

type PhyifInfo struct {
	Device  string `json:"device"`
	MacAddr string `json:"macAddr"`
}

type RouteInfo struct {
	Prefix   string `json:"prefix"`
	Protocol string `json:"protocol"`
	Metric   int    `json:"metric"`
	Distance int    `json:"distance"`
	Selected bool   `json:"selected"`
	Uptime   string `json:"uptime"`
	Nexthop  string `json:"nexthop"`
	Device   string `json:"device"`
	Active   bool   `json:"active"`
}

type DetailInfo struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type UnmarshalCallback func([]byte) (error, interface{})

func generatePrio(srcMask int, dstMask int, basePrio int) int {
	return RULEPRIO_MAX - ((srcMask+dstMask-1)<<8 + basePrio)
}

func GetRulePrio(from string, to string, basePref int) (int, error) {

	srcMask, err := strconv.Atoi(strings.Split(from, "/")[1])
	if err != nil {
		return -1, err
	}
	dstMask, err := strconv.Atoi(strings.Split(to, "/")[1])
	if err != nil {
		return -1, err
	}

	return generatePrio(srcMask, dstMask, basePref), nil
}

func GetConfFileData(path string, pf UnmarshalCallback) (error, interface{}) {
	if _, err := os.Stat(path); err != nil {
		//log.Error(path + " is not exist.")
		return err, nil
	} else {
		content, err := ioutil.ReadFile(path)
		if err != nil {
			return err, nil
		} else {
			return pf(content)
		}
	}
}

func ExecCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func ExecBashCmd(cmd string) error {
	return ExecCmd("/bin/bash", "-c", cmd)
}

func ExecBashCmdWithRet(cmd string) (error, string) {

	return ExecCmdWithRet("/bin/bash", "-c", cmd)
}

func ExecCmdWithRet(name string, args ...string) (error, string) {
	cmd := exec.Command(name, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String()), ""
	}
	return nil, out.String()
}
func ExecCmdContext(delay int, name string, args ...string) error {

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan error)

	go func() {
		var err error
		cmd := exec.CommandContext(ctx, name, args...)
		var out, errOutput bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOutput
		if err := cmd.Run(); err != nil {
			err = errors.New(errOutput.String())
		} else {
			err = nil
		}

		ch <- err
	}()

	ticker := time.NewTicker(time.Second * time.Duration(delay))

	for {
		select {
		case err := <-ch:
			return err
		case <-ticker.C:
			cancel()
			return errors.New("cmd run timeout.")
		}
	}

	return nil
}

func Set_nth_bit(x *uint64, n uint16) {
	*x = *x | (1 << (n - 1))
	return
}

func Clear_nth_bit(x *uint64, n uint16) {
	*x = *x & ^(1 << (n - 1))
	return
}

func Test_nth_bit(x uint64, n uint16) uint64 {
	return x & (1 << (n - 1))
}

// 判断文件/目录存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

func MakeDir(dir string) error {
	if !FileExists(dir) {
		if err := os.MkdirAll(dir, 0777); err != nil { //os.ModePerm
			fmt.Println("MakeDir failed:", err)
			return err
		}
	}
	return nil
}

func CopyDir(srcPath, desPath string) error {
	//检查目录是否正确
	if srcInfo, err := os.Stat(srcPath); err != nil {
		return err
	} else {
		if !srcInfo.IsDir() {
			return errors.New("源路径不是一个正确的目录！")
		}
	}

	if desInfo, err := os.Stat(desPath); err != nil {
		return err
	} else {
		if !desInfo.IsDir() {
			return errors.New("目标路径不是一个正确的目录！")
		}
	}

	if strings.TrimSpace(srcPath) == strings.TrimSpace(desPath) {
		return errors.New("源路径与目标路径不能相同！")
	}

	err := filepath.Walk(srcPath, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}

		//复制目录是将源目录中的子目录复制到目标路径中，不包含源目录本身
		if path == srcPath {
			return nil
		}

		//生成新路径
		destNewPath := strings.Replace(path, srcPath, desPath, -1)

		if !f.IsDir() {
			CopyFile(path, destNewPath)
		} else {
			if !FileExists(destNewPath) {
				return MakeDir(destNewPath)
			}
		}

		return nil
	})

	return err
}

func GetKernelVersion() (error, string) {
	err, vers := ExecCmdWithRet("uname", "-r")
	if err != nil {
		return errors.New("get kernel version failed: " + err.Error()), ""
	}

	info := strings.Split(vers, "-")
	if len(info) > 0 {
		return nil, info[0]
	}

	return errors.New("get kernel version failed."), ""
}

func CreatePath(filePath string) {
	path, _ := filepath.Split(filePath)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			panic(err)
		}
	}
}

func RmFile(name string) error {
	err := os.Remove(name)
	if err != nil {
		return err
	}
	return nil
}

/*cp src to dst，if dst exist，trunc it*/
func CopyFile(srcName, dstName string) (written int64, err error) {
	src, err := os.Open(srcName)
	if err != nil {
		return
	}
	defer src.Close()
	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

func Write2File(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	n, err := f.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return errors.New("write to " + path + "error. data : " + string(b[:]))
	}
	err = f.Sync()
	if err != nil {
		return err
	}
	return nil
}

// return s1 than s2 more or less
func SliceCompare(s1 []string, s2 []string) (more, less []string, same bool) {
	if len(s1) == 0 && len(s2) == 0 {
		return s1, s2, true
	}

	if len(s1) == 0 || len(s2) == 0 {
		return s1, s2, false
	}

	same = true
	for _, v1 := range s1 {
		var exist = false
		for _, v2 := range s2 {
			if v1 == v2 {
				exist = true
			}
		}
		if !exist {
			more = append(more, v1)
			same = false
		}
	}

	for _, v2 := range s2 {
		var exist = false
		for _, v1 := range s1 {
			if v1 == v2 {
				exist = true
			}
		}
		if !exist {
			less = append(more, v2)
			same = false
		}
	}

	return more, less, same
}

func RmfileWichCheck(path string) error {
	if FileExists(path) {
		return RmFile(path)
	}
	return nil
}

func GetBetweenStr(str, start, end string) string {
	n := strings.Index(str, start)
	if n == -1 {
		n = 0
	}
	str = string([]byte(str)[n:])
	m := strings.Index(str, end)
	if m == -1 {
		m = len(str)
	}
	str = string([]byte(str)[:m])
	return str
}

func GetCidrIpRange(cidr string) string {
	maskLen := 32
	ip := strings.Split(cidr, "/")[0]
	ipSegs := strings.Split(ip, ".")
	if strings.Contains(cidr, "/") {
		maskLen, _ = strconv.Atoi(strings.Split(cidr, "/")[1])
	}
	seg1MinIp := GetIpSeg1Range(ipSegs, maskLen)
	seg2MinIp := GetIpSeg2Range(ipSegs, maskLen)
	seg3MinIp := GetIpSeg3Range(ipSegs, maskLen)
	seg4MinIp := GetIpSeg4Range(ipSegs, maskLen)

	return strconv.Itoa(seg1MinIp) + "." + strconv.Itoa(seg2MinIp) + "." + strconv.Itoa(seg3MinIp) + "." + strconv.Itoa(seg4MinIp)
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

// 根据用户输入的基础IP地址和CIDR掩码计算一个IP片段的区间
func GetIpSegRange(userSegIp, offset uint8) int {
	var ipSegMax uint8 = 255
	netSegIp := ipSegMax << offset
	segMinIp := netSegIp & userSegIp
	return int(segMinIp)
}

func file_get_contents(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(f)

}

func Jsonfiletrans(path string, v interface{}) error {
	var content []byte
	var err error

	content, err = file_get_contents(path)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(content), v)
	if err != nil {
		return err
	}

	return err
}

func GetDataFromEtcd(path string, v interface{}) error {
	val, err := etcd.EtcdGetValue(path)
	if err != nil {
		return err
	}

	if err = json.Unmarshal([]byte(val), v); err != nil {
		return err
	}

	return nil
}

func NsExist(nsname string) (error bool) {
	cmdstr := "ls /var/run/netns/" + nsname
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		return false
	}
	return true
}

func VrfDeviceExist(namespaceId string, deviceName string) (error bool) {
	cmdstr := fmt.Sprintf("ip netns exec %s ifconfig %s", namespaceId, deviceName)
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		return false
	}
	return true
}

func Arrcmp(src []string, dest []string) ([]string, []string) {
	msrc := make(map[string]byte) //按源数组建索引
	mall := make(map[string]byte) //源+目所有元素建索引

	var set []string //交集

	//1.源数组建立map
	for _, v := range src {
		msrc[v] = 0
		mall[v] = 0
	}
	//2.目数组中，存不进去，即重复元素，所有存进去的集合就是并集
	for _, v := range dest {
		l := len(mall)
		mall[v] = 1
		if l == len(mall) { //长度没有变化，元素没有存进mall 即重复元素，交集。
			set = append(set, v)
		}
	}
	//3.遍历交集，在并集中找，找到就从并集中删，删完后就是补集（即并-交=所有变化的元素）
	for _, v := range set {
		delete(mall, v)
	}
	//4.此时，mall是补集，所有元素去源中找，找到就是删除的，找不到的必定能在目数组中找到，即新加的
	var added, deleted []string
	for v, _ := range mall {
		_, exist := msrc[v]
		if exist {
			deleted = append(deleted, v)
		} else {
			added = append(added, v)
		}
	}

	return added, deleted
}

func InetNtoA(ip int64) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}

func InetAtoN(ip string) int64 {
	ret := big.NewInt(0)
	ret.SetBytes(net.ParseIP(ip).To4())
	return ret.Int64()
}

func LenToSubNetMask(ipMaskLen string) string {
	var buff bytes.Buffer

	ipSegs := strings.Split(ipMaskLen, "/")

	ip := ipSegs[0]
	subnet, _ := strconv.Atoi(ipSegs[1])

	for i := 0; i < subnet; i++ {
		buff.WriteString("1")
	}
	for i := subnet; i < 32; i++ {
		buff.WriteString("0")
	}
	masker := buff.String()
	a, _ := strconv.ParseUint(masker[:8], 2, 64)
	b, _ := strconv.ParseUint(masker[8:16], 2, 64)
	c, _ := strconv.ParseUint(masker[16:24], 2, 64)
	d, _ := strconv.ParseUint(masker[24:32], 2, 64)
	resultMask := fmt.Sprintf("%v %v.%v.%v.%v", ip, a, b, c, d)

	return resultMask
}

func PopConfigInit() error {

	if err := Jsonfiletrans(COREINFO_PATH, &G_coreConf); err != nil {
		return err
	}

	return nil
}

/* Tc: ingress limit */
func VrfSetInterfaceIngressLimit(namespaceId string, deviceName string, limit int) error {
	return nil
}

func SetInterfaceIngressLimit(deviceName string, limit int) error {

	cmdstr := fmt.Sprintf("tc qdisc del dev %s ingress", deviceName)
	ExecBashCmdWithRet(cmdstr)

	/* if no bandwidth limit, return */
	if limit == 0 {
		return nil
	}

	cmdstr = fmt.Sprintf("tc qdisc add dev %s ingress handle ffff:", deviceName)
	if err, _ := ExecBashCmdWithRet(cmdstr); err != nil {
		agentLog.AgentLogger.Info("SetInterfaceIngressLimit error: ", err)
		return err
	}

	cmdstr = fmt.Sprintf("tc filter add dev %s parent ffff: protocol all prio 1 basic police rate %dMbit burst %dMbit mtu 65535 drop", deviceName, limit, limit)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceIngressLimit error: ", err)
		return err
	}
	return nil
}

/* Tc: egress limit */
func VrfSetInterfaceEgressLimit(namespaceId string, deviceName string, limit int) error {
	return nil
}

func SetInterfaceEgressLimit(deviceName string, limit int) error {

	cmdstr := fmt.Sprintf("tc qdisc del dev %s root", deviceName)
	ExecBashCmdWithRet(cmdstr)

	/* if no bandwidth limit, return */
	if limit == 0 {
		return nil
	}

	cmdstr = fmt.Sprintf("tc qdisc add dev %s root tbf rate %dMbit latency 50ms burst %dMbit", deviceName, limit, limit)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceEgressLimit error: ", err)
		return err
	}

	return nil
}

func VrfGetNatPolicyCount(namespaceId string, chain string) (error, string) {
	var cmdstr string
	cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -L %s --line-numbers | grep -c \"^[0-9]\"", namespaceId, chain)
	err, result_str := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("VrfGetNatPolicyCount error: ", err)
		return err, ""
	}

	return nil, strings.Replace(result_str, "\n", "", -1)
}

/* Interface Snat */
func VrfSetInterfaceSnat(undo bool, namespaceId string, deviceName string) error {
	var cmdstr string

	/* 先使用-C检查是否存在 */
	cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -C POSTROUTING -o %s -j MASQUERADE", namespaceId, deviceName)
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		/* not exist */
		if undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceSnat delete snat, but not exist, return: ", cmdstr)
			return nil
		}
	} else {
		/* exist */
		if !undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceSnat add snat, but exist, return: ", cmdstr)
			return nil
		}
	}

	if undo {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -D POSTROUTING -o %s -j MASQUERADE", namespaceId, deviceName)
	} else {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -A POSTROUTING -o %s -j MASQUERADE", namespaceId, deviceName)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceSnat error: ", err)
		return err
	}
	return nil
}

func VrfSetInterfaceSnatBySource(undo bool, namespaceId string, deviceName string, source string, destination string) error {
	var cmdstr string

	/* 先使用-C检查是否存在 */
	cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -C POSTROUTING -o %s -s %s -j SNAT --to-source %s", namespaceId, deviceName, source, destination)
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		/* not exist */
		if undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceSnatBySource delete snat, but not exist, return: ", cmdstr)
			return nil
		}
	} else {
		/* exist */
		if !undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceSnatBySource add snat, but exist, return: ", cmdstr)
			return nil
		}
	}

	if undo {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -D POSTROUTING -o %s -s %s -j SNAT --to-source %s", namespaceId, deviceName, source, destination)
	} else {
		if source == "0.0.0.0/0" {
			err, num := VrfGetNatPolicyCount(namespaceId, "POSTROUTING")
			if err != nil {
				return nil
			}
			if num == "0" {
				cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -I POSTROUTING -o %s -s %s -j SNAT --to-source %s", namespaceId, deviceName, source, destination)
			} else {
				/* 如果源为0.0.0.0/0，则将策略放置在chain倒数第二的位置上 */
				cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -I POSTROUTING %s -o %s -s %s -j SNAT --to-source %s", namespaceId, num, deviceName, source, destination)
			}
		} else {
			/* 如果源不是0.0.0.0/0，则将策略放置在chain的头部 */
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat -I POSTROUTING -o %s -s %s -j SNAT --to-source %s", namespaceId, deviceName, source, destination)
		}
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceSnatBySource error: ", err)
		return err
	}
	return nil
}

/* Interface Dnat */
func VrfSetInterfaceDnat(undo bool, namespaceId string, deviceName string, srcIp string, srcPort int, proto string, dstIp string, dstPort int) error {

	var cmdstr string
	var opt string

	/* 先使用-C检查是否存在 */
	if proto == "tcp" {
		if srcPort == dstPort {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -i %s -d %s -p tcp --dport %d -j DNAT --to-destination %s", namespaceId, deviceName, srcIp, srcPort, dstIp)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -i %s -d %s -p tcp --dport %d -j DNAT --to-destination %s:%d", namespaceId, deviceName, srcIp, srcPort, dstIp, dstPort)
		}
	} else if proto == "udp" {
		if srcPort == dstPort {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -i %s -d %s -p udp --dport %d -j DNAT --to-destination %s", namespaceId, deviceName, srcIp, srcPort, dstIp)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -i %s -d %s -p udp --dport %d -j DNAT --to-destination %s:%d", namespaceId, deviceName, srcIp, srcPort, dstIp, dstPort)
		}
	} else {
		//any
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -i %s -d %s -j DNAT --to-destination %s", namespaceId, deviceName, srcIp, dstIp)
	}
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		/* not exist */
		if undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceDnat delete dnat, but not exist, return: ", cmdstr)
			return nil
		}
	} else {
		/* exist */
		if !undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceDnat add dnat, but exist, return: ", cmdstr)
			return nil
		}
	}

	if undo {
		/* delete */
		opt = "-D"
	} else {
		/* add */
		if proto == "tcp" || proto == "udp" {
			opt = "-I"
		} else {
			opt = "-A"
		}
	}
	if proto == "tcp" {
		if srcPort == dstPort {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -i %s -d %s -p tcp --dport %d -j DNAT --to %s", namespaceId, opt, deviceName, srcIp, srcPort, dstIp)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -i %s -d %s -p tcp --dport %d -j DNAT --to %s:%d", namespaceId, opt, deviceName, srcIp, srcPort, dstIp, dstPort)
		}
	} else if proto == "udp" {
		if srcPort == dstPort {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -i %s -d %s -p udp --dport %d -j DNAT --to %s", namespaceId, opt, deviceName, srcIp, srcPort, dstIp)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -i %s -d %s -p udp --dport %d -j DNAT --to %s:%d", namespaceId, opt, deviceName, srcIp, srcPort, dstIp, dstPort)
		}
	} else {
		//any
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -i %s -d %s -j DNAT --to %s", namespaceId, opt, deviceName, srcIp, dstIp)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceDnat error: ", err)
		return err
	}
	return nil
}

func VrfSetInterfaceDnatRedirect(undo bool, namespaceId string, srcIp string, listenPort int, proto string, dstIp string, dstPort int) error {

	var cmdstr string
	var opt string

	/* 先使用-C检查是否存在 */
	if srcIp == "any" {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -p %s --dport %d -d %s -j REDIRECT --to-port %d", namespaceId, proto, dstPort, dstIp, listenPort)
	} else {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -t nat -C PREROUTING -p %s -s %s --dport %d  -d %s -j REDIRECT --to-port %d", namespaceId, proto, srcIp, dstPort, dstIp, listenPort)
	}
	err, _ := ExecBashCmdWithRet(cmdstr)
	if err != nil {
		/* not exist */
		if undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceDnatRedirect delete dnat, but not exist, return: ", cmdstr)
			return nil
		}
	} else {
		/* exist */
		if !undo {
			agentLog.AgentLogger.Info("VrfSetInterfaceDnatRedirect add dnat, but exist, return: ", cmdstr)
			return nil
		}
	}

	if undo {
		/* delete */
		opt = "-D"
	} else {
		/* add */
		opt = "-I"
	}

	if srcIp == "any" {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -p %s --dport %d -d %s -j REDIRECT --to-port %d", namespaceId, opt, proto, dstPort, dstIp, listenPort)
	} else {
		cmdstr = fmt.Sprintf("ip netns exec %s iptables -w -t nat %s PREROUTING -p %s -s %s --dport %d  -d %s -j REDIRECT --to-port %d", namespaceId, opt, proto, srcIp, dstPort, dstIp, listenPort)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceDnatRedirect error: ", err)
		return err
	}
	return nil
}

/* tcp */
func VrfSetTcpCongestionControl(namespaceId, control string) error {

	/* set tcp congestion control */
	cmdstr := fmt.Sprintf("ip netns exec %s sysctl -w net.ipv4.tcp_congestion_control=%s", namespaceId, control)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetTcpCongestionControl error: ", err)
		return err
	}

	return nil
}

/* Interface */
func VrfSetInterfaceLinkUp(namespaceId, deviceName string) error {

	/* set link up */
	cmdstr := fmt.Sprintf("ip netns exec %s ip link set %s up", namespaceId, deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceLinkUp error: ", err)
		return err
	}

	return nil
}

func SetInterfaceLinkUp(deviceName string) error {

	/* set link up */
	cmdstr := fmt.Sprintf("ip link set %s up", deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceLinkUp error: ", err)
		return err
	}
	return nil
}

func SetInterfaceLinkDown(deviceName string) error {

	/* set link down */
	cmdstr := fmt.Sprintf("ip link set %s down", deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceLinkDown error: ", err)
		return err
	}
	return nil
}

func VrfSetInterfaceAddress(undo bool, namespaceId, deviceName, addr string) error {

	/* set ip address */
	var cmdstr string
	if strings.Contains(addr, "/") {
		if undo {
			cmdstr = fmt.Sprintf("ip netns exec %s ip addr del %s dev %s", namespaceId, addr, deviceName)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s ip addr add %s dev %s", namespaceId, addr, deviceName)
		}

	} else {
		if undo {
			cmdstr = fmt.Sprintf("ip netns exec %s ip addr del %s dev %s", namespaceId, addr+"/32", deviceName)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s ip addr add %s dev %s", namespaceId, addr+"/32", deviceName)
		}
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceAddress error: ", err)
		return err
	}

	return nil
}

func SetInterfaceAddress(undo bool, deviceName, addr string) error {

	/* set ip address */
	var cmdstr string
	if strings.Contains(addr, "/") {
		if undo {
			cmdstr = fmt.Sprintf("ip addr del %s dev %s", addr, deviceName)
		} else {
			cmdstr = fmt.Sprintf("ip addr add %s dev %s", addr, deviceName)
		}
	} else {
		if undo {
			cmdstr = fmt.Sprintf("ip addr del %s dev %s", addr+"/32", deviceName)
		} else {
			cmdstr = fmt.Sprintf("ip addr add %s dev %s", addr+"/32", deviceName)
		}
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceAddress error: ", err)
		return err
	}
	return nil
}

func SetInterfaceNetns(netns, deviceName string) error {

	/* set interface to Netns*/
	cmdstr := fmt.Sprintf("ip link set %s netns %s", deviceName, netns)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceNetns error: ", err)
		return err
	}

	return nil
}

func VrfSetInterfaceNetns(namespaceId, netns, deviceName string) error {

	/* set interface to Netns*/
	cmdstr := fmt.Sprintf("ip netns exec %s ip link set %s netns %s", namespaceId, deviceName, netns)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetInterfaceNetns error: ", err)
		return err
	}

	return nil
}

func SetVrfInterfaceRootNs(namespaceId, deviceName string) error {

	/* set vrf interface to root */
	cmdstr := fmt.Sprintf("ip netns exec %s ip link set %s netns 1", namespaceId, deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("SetVrfInterfaceRootNs error: ", err)
		return err
	}

	return nil
}

func RenameInterface(deviceName, newName string) error {

	/* Rename interface */
	cmdstr := fmt.Sprintf("ip link set %s name %s", deviceName, newName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("RenameInterface error: ", err)
		return err
	}

	return nil
}

func CreateInterfaceTypeIpvlanL2(deviceName, phyIfName string) error {

	/* Create ipvlan L2 */
	cmdstr := fmt.Sprintf("ip link add %s link %s type ipvlan mode l2", deviceName, phyIfName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("CreateInterfaceTypeIpvlanL2 error: ", err)
		return err
	}
	return nil
}

func CreateInterfaceTypeVeth(deviceName, peerName string) error {

	/* Create veth tunnel */
	cmdstr := fmt.Sprintf("ip link add %s type veth peer name %s", deviceName, peerName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("CreateInterfaceTypeVeth error: ", err)
		return err
	}

	return nil
}

func CreateInterfaceTypeVlanif(phyIfName, deviceName string, vlanId int) error {

	/* Create vlanif */
	cmdstr := fmt.Sprintf("ip link add link %s name %s type vlan id %d", phyIfName, deviceName, vlanId)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("CreateInterfaceTypeVlanif error: ", err)
		return err
	}

	return nil
}

func CreateInterfaceTypeIpvlan(phyIfName, deviceName string) error {

	/* Create ipvlan */
	cmdstr := fmt.Sprintf("ip link add link %s %s type ipvlan mode l3", phyIfName, deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("CreateInterfaceTypeIpvlan error: ", err)
		return err
	}
	return nil
}

func CreateInterfaceTypeGre(deviceName, local, remote string, key int) error {

	/* Create gre tunnel */
	var cmdstr string
	if key == 0 {
		cmdstr = fmt.Sprintf("ip link add %s type gre local %s remote %s ttl 255", deviceName, local, remote)
	} else {
		cmdstr = fmt.Sprintf("ip link add %s type gre local %s remote %s ttl 255 key %d", deviceName, local, remote, key)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("CreateInterfaceTypeGre error: ", err)
		return err
	}

	return nil
}

func VrfCreateInterfaceTypeGre(namespaceId, deviceName, local, remote string, key int) error {

	/* Create gre tunnel */
	var cmdstr string
	if key == 0 {
		cmdstr = fmt.Sprintf("ip netns exec %s ip link add %s type gre local %s remote %s ttl 255", namespaceId, deviceName, local, remote)
	} else {
		cmdstr = fmt.Sprintf("ip netns exec %s ip link add %s type gre local %s remote %s ttl 255 key %d", namespaceId, deviceName, local, remote, key)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfCreateInterfaceTypeGre error: ", err)
		return err
	}

	return nil
}

func ModifyInterfaceTypeGre(deviceName, local, remote string, key int) error {

	var cmdstr string
	if key == 0 {
		cmdstr = fmt.Sprintf("ip link change %s type gre local %s remote %s ttl 255", deviceName, local, remote)
	} else {
		cmdstr = fmt.Sprintf("ip link change %s type gre local %s remote %s ttl 255 key %d", deviceName, local, remote, key)
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("ModifyInterfaceTypeGre error: ", err)
		return err
	}

	return nil
}

func VrfModifyInterfaceTypeGre(namespaceId, deviceName, local, remote string) error {

	cmdstr := fmt.Sprintf("ip netns exec %s ip link change %s type gre local %s remote %s", namespaceId, deviceName, local, remote)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfModifyInterfaceTypeGre error: ", err)
		return err
	}

	return nil
}

func DeleteInterface(deviceName string) error {

	cmdstr := fmt.Sprintf("ip link del %s", deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("DeleteInterface error: ", err)
		return err
	}

	return nil
}

func VrfDeleteInterface(namespaceId, deviceName string) error {

	cmdstr := fmt.Sprintf("ip netns exec %s ip link del %s", namespaceId, deviceName)
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfDeleteInterface error: ", err)
		return err
	}

	return nil
}

func VrfSetIpruleBySource(undo bool, namespaceId string, source string, tableId int, priority int) error {
	var cmdstr string

	if strings.Contains(source, "/") {
		if undo {
			cmdstr = fmt.Sprintf("ip netns exec %s ip rule del from %s table %d pref %d", namespaceId, source, tableId, priority)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s ip rule add from %s table %d pref %d", namespaceId, source, tableId, priority)
		}

	} else {
		if undo {
			cmdstr = fmt.Sprintf("ip netns exec %s ip rule del from %s table %d pref %d", namespaceId, source+"/32", tableId, priority)
		} else {
			cmdstr = fmt.Sprintf("ip netns exec %s ip rule add from %s table %d pref %d", namespaceId, source+"/32", tableId, priority)
		}
	}
	err, ret := ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd: ", cmdstr, ", ret: ", ret)
	if err != nil {
		agentLog.AgentLogger.Info("VrfSetIpruleBySource error: ", err)
		return err
	}

	return nil
}

func CheckSn(sn string) bool {
	if G_coreConf.Sn != sn {
		return false
	} else {
		return true
	}
}

func getUrl(ip string, port int, proto string, path string) (url string) {
	if proto == "https" {
		if port == 0 {
			url = "https://" + ip + path
		} else {
			url = "https://" + ip + ":" + strconv.Itoa(port) + path
		}
	} else {
		if port == 0 {
			port = 443
		}
		url = "http://" + ip + ":" + strconv.Itoa(port) + path
	}
	return
}

func RequestCore(res []byte, ip string, port int, proto string, path string) ([]byte, error) {
	var resp *http.Response = nil
	var err error = nil
	edgeTimeOut := 20
	timeout := time.Duration(edgeTimeOut) * time.Second

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}

	url := getUrl(ip, port, proto, path)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(res))
	if err != nil {
		agentLog.AgentLogger.Info("req ", req, "err", err, "url:", url)
		return nil, err
	}
	req.Header.Set("X-Request-Source", "admin-api")
	//req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)

	if nil != resp {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		agentLog.AgentLogger.Info("resp.StatusCode :", resp.StatusCode)
		err = errors.New("send to core fail")
		return nil, err
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBody, nil
}

func GetRequestCore(ip string, port int, proto string, path string) ([]byte, error) {
	var resp *http.Response = nil
	var err error = nil
	edgeTimeOut := 20
	timeout := time.Duration(edgeTimeOut) * time.Second

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := http.Client{
		Timeout:   timeout,
		Transport: tr,
	}

	url := getUrl(ip, port, proto, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		agentLog.AgentLogger.Info("get ", req, "err", err, "url:", url)
		return nil, err
	}
	req.Header.Set("X-Request-Source", "admin-api")
	//req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)

	if nil != resp {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		agentLog.AgentLogger.Info("resp.StatusCode :", resp.StatusCode)
		err = errors.New("send to core fail")
		return nil, err
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBody, nil
}
