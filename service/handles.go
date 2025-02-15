package main

import (
	"datapath/agentLog"
	"datapath/app"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"gitlab.daho.tech/gdaho/network"
)

type Res struct {
	Message string `json:"msg"`
	Code    string `json:"code"`
	Success bool   `json:"success"`
}

type IfsRes struct {
	Message string             `json:"msg"`
	Code    string             `json:"code"`
	Success bool               `json:"success"`
	Data    []public.PhyifInfo `json:"data"`
}

type RtsRes struct {
	Message string             `json:"msg"`
	Code    string             `json:"code"`
	Success bool               `json:"success"`
	Data    []public.RouteInfo `json:"data"`
}

type DetailRes struct {
	Message string            `json:"msg"`
	Code    string            `json:"code"`
	Success bool              `json:"success"`
	Data    public.DetailInfo `json:"data"`
}

type DataStrRes struct {
	Message string `json:"msg"`
	Code    string `json:"code"`
	Success bool   `json:"success"`
	Data    string `json:"data"`
}

type Msg struct {
	res Res
	Err error `json:"err"`
}

// 常规 Handler
type PostHandler func(params map[string]string, data []byte, vapType string) *Res
type PutHandler func(params map[string]string, data []byte, vapType string) *Res
type DelHandler func(params map[string]string, vapType string) *Res
type GetHandler func(params map[string]string, vapType string) *Res

// 扩展 Handler
type GetIfsHandler func(params map[string]string, vapType string) *IfsRes
type GetRtsHandler func(params map[string]string, vapType string) *RtsRes
type GetDetailHandler func(params map[string]string, vapType string) *DetailRes
type GetDataStrHandler func(params map[string]string, vapType string) *DataStrRes

func GetPopIfsInfo(params map[string]string, vapType string) (result *IfsRes) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("GetPopIfsInfo Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &IfsRes{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	/* sn 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(sn, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	cmdstr := "ip addr | grep link/ether -B 1 | grep BROADCAST | awk '{print $2}' | awk -F ':' '{print $1}' | grep  -v '@'| awk BEGIN{RS=EOF}'{gsub(/\\n/,\",\");print}'"
	err, ifsInfo := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "ifsInfo", ifsInfo)
		result = &IfsRes{Success: false, Message: ""}
		return
	}

	ifsInfo = strings.Replace(ifsInfo, "\n", "", -1)
	arr := strings.Split(ifsInfo, ",")
	var phyif_tpm = &public.PhyifInfo{}
	var phyifs = make([]public.PhyifInfo, 0)
	for i := 0; i < len(arr); i++ {
		cmdstr = fmt.Sprintf(`cat /sys/class/net/%s/address`, arr[i])
		err, result_str := public.ExecBashCmdWithRet(cmdstr)
		if err == nil {
			phyif_tpm.MacAddr = strings.Replace(result_str, "\n", "", -1)
			phyif_tpm.Device = arr[i]

			phyifs = append(phyifs, *phyif_tpm)
		}
	}

	result = &IfsRes{Success: true, Message: "", Data: phyifs}
	return
}

func InitVapIpsec(params map[string]string, vapType string) (result *DetailRes) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("InitVapIpsec Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("InitVapIpsec Get ID " + id)

	dataInfo := public.DetailInfo{Status: "Idle", Detail: ""}
	if !public.CheckSn(sn) {
		result = &DetailRes{Success: false, Code: "500", Message: CheckSnError, Data: dataInfo}
		return
	}

	/* vap lock */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VapConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.VapConfPath + id)
	}()

	_, isExist, err := etcd.EtcdGetValueWithCheck(config.VapConfPath + id)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("vap not Exists")
		result = &DetailRes{Success: false, Message: "ConfIsNotExists", Data: dataInfo}
		return
	}

	path := config.VapConfPath + id
	fp := &app.VapConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err, saInfo := fp.InitIpsecSa()
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "saInfo", saInfo)
		result = &DetailRes{Success: false, Message: "", Data: dataInfo}
		return
	}

	dataInfo.Detail = saInfo
	if strings.Contains(saInfo, fp.Id) &&
		strings.Contains(saInfo, "successfully") {
		dataInfo.Status = "Established"
	}
	result = &DetailRes{Success: true, Message: "", Data: dataInfo}
	return
}

func GetVapIpsecSaInfo(params map[string]string, vapType string) (result *DetailRes) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("InitVapIpsec Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("InitVapIpsec Get ID " + id)

	dataInfo := public.DetailInfo{Status: "Idle", Detail: ""}
	if !public.CheckSn(sn) {
		result = &DetailRes{Success: false, Code: "500", Message: CheckSnError, Data: dataInfo}
		return
	}

	/* vap lock */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VapConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.VapConfPath + id)
	}()

	_, isExist, err := etcd.EtcdGetValueWithCheck(config.VapConfPath + id)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("vap not Exists")
		result = &DetailRes{Success: false, Message: "ConfIsNotExists", Data: dataInfo}
		return
	}

	path := config.VapConfPath + id
	fp := &app.VapConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err, saInfo := fp.GetIpsecSa()
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "saInfo", saInfo)
		result = &DetailRes{Success: false, Message: "", Data: dataInfo}
		return
	}

	dataInfo.Detail = saInfo
	if strings.Contains(saInfo, fp.Id) &&
		strings.Contains(saInfo, "ESTABLISHED") &&
		strings.Contains(saInfo, "INSTALLED") {
		dataInfo.Status = "Established"
	}
	result = &DetailRes{Success: true, Message: "", Data: dataInfo}
	return
}

func InitConnIpsec(params map[string]string, vapType string) (result *DetailRes) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("InitConnIpsec Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("InitConnIpsec Get ID " + id)

	dataInfo := public.DetailInfo{Status: "Idle", Detail: ""}
	if !public.CheckSn(sn) {
		result = &DetailRes{Success: false, Code: "500", Message: CheckSnError, Data: dataInfo}
		return
	}

	/* conn lock */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.ConnConfPath + id)
	}()

	_, isExist, err := etcd.EtcdGetValueWithCheck(config.ConnConfPath + id)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("conn not Exists")
		result = &DetailRes{Success: false, Message: "ConfIsNotExists", Data: dataInfo}
		return
	}

	path := config.ConnConfPath + id
	fp := &app.ConnConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err, saInfo := fp.InitIpsecSa()
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "saInfo", saInfo)
		result = &DetailRes{Success: false, Message: "", Data: dataInfo}
		return
	}

	dataInfo.Detail = saInfo
	if strings.Contains(saInfo, fp.Id) &&
		strings.Contains(saInfo, "successfully") {
		dataInfo.Status = "Established"
	}

	result = &DetailRes{Success: true, Message: "", Data: dataInfo}
	return
}

func GetConnIpsecSaInfo(params map[string]string, vapType string) (result *DetailRes) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("GetConnIpsecSaInfo Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("GetConnIpsecSaInfo Get ID " + id)

	dataInfo := public.DetailInfo{Status: "Idle", Detail: ""}
	if !public.CheckSn(sn) {
		result = &DetailRes{Success: false, Code: "500", Message: CheckSnError, Data: dataInfo}
		return
	}

	/* conn lock */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.ConnConfPath + id)
	}()

	_, isExist, err := etcd.EtcdGetValueWithCheck(config.ConnConfPath + id)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("conn not Exists")
		result = &DetailRes{Success: false, Message: "ConfIsNotExists", Data: dataInfo}
		return
	}

	path := config.ConnConfPath + id
	fp := &app.ConnConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err, saInfo := fp.GetIpsecSa()
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "saInfo", saInfo)
		result = &DetailRes{Success: false, Message: "", Data: dataInfo}
		return
	}

	dataInfo.Detail = saInfo
	if strings.Contains(saInfo, fp.Id) &&
		strings.Contains(saInfo, "ESTABLISHED") &&
		strings.Contains(saInfo, "INSTALLED") {
		dataInfo.Status = "Established"
	}
	result = &DetailRes{Success: true, Message: "", Data: dataInfo}
	return
}

func ClearConnBgp(params map[string]string, vapType string) (result *Res) {

	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ClearConnBgp Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("ClearConnBgp Get ID " + id)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	/* bgp lock */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.ConnConfPath + id)
	}()

	_, isExist, err := etcd.EtcdGetValueWithCheck(config.ConnConfPath + id)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("bgp not Exists")
		result = &Res{Success: false, Message: "ConfIsNotExists"}
		return
	}

	path := config.ConnConfPath + id
	fp := &app.ConnConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.ClearBgpNeigh()
	if err != nil {
		result = &Res{Success: false, Message: "ClearBgpNeigh fail"}
		return
	}

	result = &Res{Success: true, Message: "ClearBgpNeigh success"}
	return
}

func CheckToolPing(params map[string]string, vapType string) (result *Res) {
	var cmdstr string
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CheckToolPing Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("CheckToolPing Get ID " + id)

	target, _ := getValue(params, "target")
	agentLog.AgentLogger.Info("CheckToolPing Get Target " + target)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	/* Id (edge)保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	if id == "default" {
		cmdstr = fmt.Sprintf("ping -s 1000 -f -c 10 -W 5 %s", target)
	} else {
		_, isExist, err := etcd.EtcdGetValueWithCheck(config.EdgeConfPath + id)
		if err != nil {
			errPanic("EtcdGetError", "EtcdGetError", err)
		}
		if !isExist {
			agentLog.AgentLogger.Info("edge not exists")
			result = &Res{Success: false, Message: ""}
			return
		}
		cmdstr = fmt.Sprintf("ip netns exec %s ping -s 1000 -f -c 10 -W 5 %s", id, target)
	}

	err, resInfo := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "resInfo", resInfo)
		result = &Res{Success: false, Message: ""}
		return
	}

	result = &Res{Success: true, Message: resInfo}
	return
}

func CheckToolTcping(params map[string]string, vapType string) (result *Res) {
	var cmdstr string
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CheckToolTcping Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("CheckToolTcping Get ID " + id)

	target, _ := getValue(params, "target")
	agentLog.AgentLogger.Info("CheckToolTcping target: " + target)

	port, _ := getValue(params, "port")
	agentLog.AgentLogger.Info("CheckToolTcping port: " + port)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	/* Id (edge)保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	if id == "default" {
		cmdstr = fmt.Sprintf("tcping -v4 -t 1 %s %s", target, port)
	} else {
		_, isExist, err := etcd.EtcdGetValueWithCheck(config.EdgeConfPath + id)
		if err != nil {
			errPanic("EtcdGetError", "EtcdGetError", err)
		}
		if !isExist {
			agentLog.AgentLogger.Info("edge not exists")
			result = &Res{Success: false, Message: ""}
			return
		}
		cmdstr = fmt.Sprintf("ip netns exec %s tcping -v4 -t 1 %s %s", id, target, port)
	}

	err, resInfo := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "resInfo", resInfo)
		result = &Res{Success: false, Message: ""}
		return
	}

	result = &Res{Success: true, Message: resInfo}
	return
}

func CheckToolRoutes(params map[string]string, vapType string) (result *RtsRes) {

	var cmdstr string
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CheckToolRoutes Get SN " + sn)

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("CheckToolRoutes Get ID " + id)
	if !public.CheckSn(sn) {
		result = &RtsRes{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	/* Id (edge)保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	cmdstr = strings.Join(app.CmdShowRoute(id), " ")
	err, resInfo := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Error("err: ", err, "resInfo", resInfo)
		result = &RtsRes{Success: false, Message: ""}
		return
	}

	routes := app.GetRouterInfo(resInfo)
	result = &RtsRes{Success: true, Message: "", Data: routes}
	return
}

func OpsEcho(params map[string]string, vapType string) (result *DataStrRes) {

	result = &DataStrRes{Success: true, Message: "", Data: public.G_coreConf.Sn}
	return
}

/* iperf3 tools */
func StartToolIperf3(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("StartToolIperf3 Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("StartToolIperf3:" + string(data[:]))
	conf := &app.ToolIperf3Conf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* iperf3 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.Iperf3ConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.Iperf3ConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("iperf3 EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.StartService(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.Iperf3ConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.Iperf3ConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func StopToolIperf3(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("StopToolIperf3 id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.Iperf3ConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.Iperf3ConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.Iperf3ConfPath + id)
	agentLog.AgentLogger.Info("oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Id not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.Iperf3ConfPath + id
	fp := &app.ToolIperf3Conf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.StopService()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del iperf3 etcd data
	err = etcd.EtcdDelValue(config.Iperf3ConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

func GetToolIperf3Log(params map[string]string, vapType string) (result *DataStrRes) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("GetToolIperf3Log Get SN " + sn)

	edgeId, _ := getValue(params, "edgeId")
	agentLog.AgentLogger.Info("GetToolIperf3Log Get edgeId " + edgeId)

	if !public.CheckSn(sn) {
		result = &DataStrRes{Success: false, Code: "500", Message: CheckSnError, Data: ""}
		return
	}

	dirPath := fmt.Sprintf("/home/iperf3/%s", edgeId)
	if public.FileExists(dirPath) {
		// 读取目录内容
		files, err := ioutil.ReadDir(dirPath)
		if err != nil {
			result = &DataStrRes{Success: true, Message: "", Data: ""}
			return
		}

		// 遍历目录条目并打印文件名称
		for _, file := range files {
			// 判断是否是文件
			filePath := fmt.Sprintf("/home/iperf3/%s/%s", edgeId, file.Name())
			///agentLog.AgentLogger.Info("GetToolIperf3Log filePath:" + filePath + ", file:" + file.Name())
			if !file.IsDir() {
				data, err := ioutil.ReadFile(filePath)
				if err != nil {
					result = &DataStrRes{Success: true, Message: "", Data: ""}
					return
				}

				///agentLog.AgentLogger.Info("GetToolIperf3Log date:" + string(data))
				result = &DataStrRes{Success: true, Message: "", Data: string(data)}
				return
			}
		}
	}

	result = &DataStrRes{Success: true, Message: "", Data: ""}
	return
}

func ClearToolIperf3Log(params map[string]string, vapType string) (result *Res) {
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ClearToolIperf3Log Get SN " + sn)

	edgeId, _ := getValue(params, "edgeId")
	agentLog.AgentLogger.Info("ClearToolIperf3Log Get edgeId " + edgeId)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	dirPath := fmt.Sprintf("/home/iperf3/%s", edgeId)
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	result = &Res{Success: true, Message: ""}
	return
}

/* Phyif */
func CreatePhyif(params map[string]string, data []byte, vapType string) (result *Res) {

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreatePhyif Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreatePhyif:" + string(data[:]))
	conf := &app.PhyifConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* port 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PhyifConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.PhyifConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("phyif EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.PhyifConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.PhyifConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func DelPhyif(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete phyif id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PhyifConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.PhyifConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.PhyifConfPath + id)
	agentLog.AgentLogger.Info("phyif oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("phyif not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.PhyifConfPath + id
	fp := &app.PhyifConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del phyif etcd data
	err = etcd.EtcdDelValue(config.PhyifConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Port */
func CreatePort(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreatePort Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreatePort:" + string(data[:]))
	conf := &app.PortConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* port 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.PortConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("port EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.PortConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.PortConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModPort(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModPort Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModPort:" + string(data[:]))
	confNew := &app.PortConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Port Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.PortConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.PortConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.PortConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelPort(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete port id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.PortConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.PortConfPath + id)
	agentLog.AgentLogger.Info("port oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("port not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.PortConfPath + id
	fp := &app.PortConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del port etcd data
	err = etcd.EtcdDelValue(config.PortConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Vap */
func CreateVap(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateVap Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateVap:" + string(data[:]))
	conf := &app.VapConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* vap 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VapConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.VapConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("vap EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.VapConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.VapConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModVap(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModVap Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModVap:" + string(data[:]))
	confNew := &app.VapConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Vap Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VapConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.VapConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.VapConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.VapConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelVap(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete vap id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VapConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.VapConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.VapConfPath + id)
	agentLog.AgentLogger.Info("vap oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("vap not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.VapConfPath + id
	fp := &app.VapConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del vap etcd data
	err = etcd.EtcdDelValue(config.VapConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* LinkEndp */
func CreateLinkEndp(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateLinkEndp Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateLinkEndp:" + string(data[:]))
	conf := &app.LinkEndpConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* link 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkEndpConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.LinkEndpConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("linkEndp EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.LinkEndpConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.LinkEndpConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModLinkEndp(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModLinkEndp Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModLinkEndp:" + string(data[:]))
	confNew := &app.LinkEndpConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("LinkEndp Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkEndpConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.LinkEndpConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.LinkEndpConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.LinkEndpConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelLinkEndp(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete link id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkEndpConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.LinkEndpConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.LinkEndpConfPath + id)
	agentLog.AgentLogger.Info("link oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("link not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.LinkEndpConfPath + id
	fp := &app.LinkEndpConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del link etcd data
	err = etcd.EtcdDelValue(config.LinkEndpConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* LinkTran */
func CreateLinkTran(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateLinkTran Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateLinkTran:" + string(data[:]))
	conf := &app.LinkTranConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* link 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkTranConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.LinkTranConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("linkTran EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.LinkTranConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.LinkTranConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModLinkTran(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModLinkTran Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModLinkTran:" + string(data[:]))
	confNew := &app.LinkTranConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("LinkTran Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkTranConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.LinkTranConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.LinkTranConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.LinkTranConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelLinkTran(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete link id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.LinkTranConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.LinkTranConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.LinkTranConfPath + id)
	agentLog.AgentLogger.Info("link oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("link not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.LinkTranConfPath + id
	fp := &app.LinkTranConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del LinkTran etcd data
	err = etcd.EtcdDelValue(config.LinkTranConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* vpl Endpoint */
func CreateVplEndp(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateVplEndp Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateVplEndp:" + string(data[:]))
	conf := &app.VplEndpConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* link 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplEndpConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.VplEndpConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("vplEndp EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.VplEndpConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.VplEndpConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModVplEndp(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModVplEndp Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModVplEndp:" + string(data[:]))
	confNew := &app.VplEndpConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("vpl endpoint Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplEndpConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.VplEndpConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.VplEndpConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.VplEndpConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelVplEndp(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete vpl endpoint id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplEndpConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.VplEndpConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.VplEndpConfPath + id)
	agentLog.AgentLogger.Info("vpl endpoint oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("vpl endpoint not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.VplEndpConfPath + id
	fp := &app.VplEndpConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del vplEndp etcd data
	err = etcd.EtcdDelValue(config.VplEndpConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* VplTran */
func CreateVplTran(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateVplTran Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateVplTran:" + string(data[:]))
	conf := &app.LinkTranConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* vpl 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplTranConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.VplTranConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("vplTran EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.VplTranConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.VplTranConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModVplTran(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModVplTran Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModVplTran:" + string(data[:]))
	confNew := &app.LinkTranConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("VplTran Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplTranConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.VplTranConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.LinkTranConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.VplTranConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelVplTran(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete vpl id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.VplTranConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.VplTranConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.VplTranConfPath + id)
	agentLog.AgentLogger.Info("vpl oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("vpl not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.VplTranConfPath + id
	fp := &app.LinkTranConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del VplTran etcd data
	err = etcd.EtcdDelValue(config.VplTranConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Nat */
func CreateNat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateNat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateNat:" + string(data[:]))
	conf := &app.NatConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* link 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.NatConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("nat EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.NatConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.NatConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModNat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModNat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModNat:" + string(data[:]))
	confNew := &app.NatConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Nat Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.NatConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.NatConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.NatConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelNat(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Nat id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.NatConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.NatConfPath + id)
	agentLog.AgentLogger.Info("Nat oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Nat not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.NatConfPath + id
	fp := &app.NatConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del Nat etcd data
	err = etcd.EtcdDelValue(config.NatConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Natshare */
func CreateNatshare(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateNatshare Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateNatshare:" + string(data[:]))
	conf := &app.NatshareConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* link 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatshareConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.NatshareConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("natShare EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.NatshareConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.NatshareConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModNatshare(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModNatshare Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModNatshare:" + string(data[:]))
	confNew := &app.NatshareConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Natshare Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatshareConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.NatshareConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.NatshareConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.NatshareConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelNatshare(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Natshare id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.NatshareConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.NatshareConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.NatshareConfPath + id)
	agentLog.AgentLogger.Info("Natshare oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Natshare not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.NatshareConfPath + id
	fp := &app.NatshareConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del Nat etcd data
	err = etcd.EtcdDelValue(config.NatshareConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Dnat */
func CreateDnat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("createDnat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("createDnat Get connId " + connId)

	agentLog.AgentLogger.Info("createDnat:" + string(data[:]))
	conf := &app.DnatConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	conf.ConnId = connId
	/* dnat 保护锁 */
	etcdPath := config.DnatConfPath + conf.ConnId + "_" + conf.Id
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(etcdPath)
	if err == nil {
		agentLog.AgentLogger.Info("dnat EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(etcdPath, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(etcdPath)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModDnat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModDnat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("ModDnat Get connId " + connId)

	agentLog.AgentLogger.Info("ModDnat:" + string(data[:]))
	confNew := &app.DnatConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	confNew.ConnId = connId
	agentLog.AgentLogger.Info("Dnat Id:", confNew.Id)
	etcdPath := config.DnatConfPath + confNew.ConnId + "_" + confNew.Id
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(etcdPath)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.DnatConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(etcdPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelDnat(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("DelDnat Get connId " + connId)

	dnatId, _ := getValue(params, "target")
	agentLog.AgentLogger.Info("Delete Dnat id: " + dnatId)

	etcdPath := config.DnatConfPath + connId + "_" + dnatId
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(etcdPath)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(etcdPath)
	agentLog.AgentLogger.Info("dnat oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("dnat not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	fp := &app.DnatConf{}
	if err = public.GetDataFromEtcd(etcdPath, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del dnat etcd data
	err = etcd.EtcdDelValue(etcdPath)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(dnatId + "delete success.")
	return
}

func DelDnatAll(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("DelDnatAll Get connId " + connId)

	paths := []string{config.DnatConfPath}
	dnats, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("dnats not found: ", err.Error())
		result = &Res{Success: true}
		return
	}

	for _, value := range dnats {

		bytes := []byte(value)
		fp := &app.DnatConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Error("dnat data unmarshal failed: ", err.Error())
			continue
		}

		if fp.ConnId != connId {
			continue
		}

		agentLog.AgentLogger.Info("delete dnat: " + value)
		etcdPath := config.DnatConfPath + fp.ConnId + "_" + fp.Id
		vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
		vLock.(*sync.Mutex).Lock()
		defer func() {
			vLock.(*sync.Mutex).Unlock()
			nsLockDelayDel(etcdPath)
		}()

		err = fp.Destroy()
		if err != nil {
			errPanic(DestroyError, DestroyError, err)
		}

		// del dnat etcd data
		err = etcd.EtcdDelValue(etcdPath)
		if err != nil {
			errPanic(EtcdDelError, EtcdDelError, err)
		}
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info("all dnat delete success.")
	return
}

/* Snat */
func CreateSnat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateSnat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("CreateSnat Get connId " + connId)

	agentLog.AgentLogger.Info("CreateSnat:" + string(data[:]))
	conf := &app.SnatConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	conf.ConnId = connId
	/* snat 保护锁 */
	etcdPath := config.SnatConfPath + conf.ConnId + "_" + conf.Id
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(etcdPath)
	if err == nil {
		agentLog.AgentLogger.Info("snat EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(etcdPath, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(etcdPath)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModSnat(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModSnat Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("ModSnat Get connId " + connId)

	agentLog.AgentLogger.Info("ModSnat:" + string(data[:]))
	confNew := &app.SnatConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	confNew.ConnId = connId
	agentLog.AgentLogger.Info("Snat Id:", confNew.Id)
	etcdPath := config.SnatConfPath + confNew.ConnId + "_" + confNew.Id
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(etcdPath)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.SnatConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(etcdPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelSnat(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("DelSnat Get connId " + connId)

	dnatId, _ := getValue(params, "target")
	agentLog.AgentLogger.Info("Delete Snat id: " + dnatId)

	etcdPath := config.SnatConfPath + connId + "_" + dnatId
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(etcdPath)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(etcdPath)
	agentLog.AgentLogger.Info("snat oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("snat not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	fp := &app.SnatConf{}
	if err = public.GetDataFromEtcd(etcdPath, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del snat etcd data
	err = etcd.EtcdDelValue(etcdPath)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(dnatId + "delete success.")
	return
}

func DelSnatAll(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	connId, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("DelSnatAll Get connId " + connId)

	paths := []string{config.SnatConfPath}
	dnats, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("snats not found: ", err.Error())
		result = &Res{Success: true}
		return
	}

	for _, value := range dnats {

		bytes := []byte(value)
		fp := &app.SnatConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Error("snat data unmarshal failed: ", err.Error())
			continue
		}

		if fp.ConnId != connId {
			continue
		}

		agentLog.AgentLogger.Info("delete snat: " + value)
		etcdPath := config.SnatConfPath + fp.ConnId + "_" + fp.Id
		vLock, _ := public.EtcdLock.NsLock.LoadOrStore(etcdPath, new(sync.Mutex))
		vLock.(*sync.Mutex).Lock()
		defer func() {
			vLock.(*sync.Mutex).Unlock()
			nsLockDelayDel(etcdPath)
		}()

		err = fp.Destroy()
		if err != nil {
			errPanic(DestroyError, DestroyError, err)
		}

		// del snat etcd data
		err = etcd.EtcdDelValue(etcdPath)
		if err != nil {
			errPanic(EtcdDelError, EtcdDelError, err)
		}
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info("all snat delete success.")
	return
}

/* Tcp */
func UpdateTcp(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("UpdateTcp Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("UpdateTcp:" + string(data[:]))
	conf := &app.TcpConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* tcp 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.TcpConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	cfgChg := false
	//check etcd data exists
	confdataOld, err := etcd.EtcdGetValue(config.TcpConfPath + conf.Id)
	if err != nil {
		agentLog.AgentLogger.Info("tcp EtcdNoExist.")
		byteep, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		if len(conf.TcpRules) > 0 {
			agentLog.AgentLogger.Info("Create before data: " + string(byteep))
			if err = conf.Create(public.ACTION_ADD); err != nil {
				errPanic(ParamsError, ParamsError, err)
			}
			cfgChg = true
		}
	} else {
		agentLog.AgentLogger.Info("tcp EtcdHasExist, modfiy configure.")
		confCur := &app.TcpConf{}
		if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}
		if err, cfgChg = confCur.Modify(conf); err != nil {
			errPanic(ModError, ModError, err)
		}
	}

	if cfgChg {
		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.TcpConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		defer rollbackEtcdRecord(config.TcpConfPath + conf.Id)
	}

	result = &Res{Success: true, Message: string("success")}
	return
}

func DelTcp(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Tcp id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.TcpConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.TcpConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.TcpConfPath + id)
	agentLog.AgentLogger.Info("tcp oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("edge not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.TcpConfPath + id
	fp := &app.TcpConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del tcp etcd data
	err = etcd.EtcdDelValue(config.TcpConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Edge */
func CreateEdge(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateEdge Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateEdge:" + string(data[:]))
	conf := &app.EdgeConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* edge 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.EdgeConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("edge EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.EdgeConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.EdgeConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModEdge(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModEdge Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModEdge:" + string(data[:]))
	confNew := &app.EdgeConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("edge Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.EdgeConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.EdgeConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.EdgeConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelEdge(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Edge id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.EdgeConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.EdgeConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.EdgeConfPath + id)
	agentLog.AgentLogger.Info("edge oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("edge not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.EdgeConfPath + id
	fp := &app.EdgeConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del edge etcd data
	err = etcd.EtcdDelValue(config.EdgeConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* Tunnel */
func CreateTunnel(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateTunnel Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateTunnel:" + string(data[:]))
	conf := &app.TunnelConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* tunnel 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.TunnelConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.TunnelConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("tunnel EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.TunnelConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.TunnelConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModTunnel(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModTunnel Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModTunnel:" + string(data[:]))
	confNew := &app.TunnelConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Tunnel Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.TunnelConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.TunnelConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.TunnelConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.TunnelConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelTunnel(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Tunnel id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.TunnelConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.TunnelConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.TunnelConfPath + id)
	agentLog.AgentLogger.Info("tunnel oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("tunnel not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.TunnelConfPath + id
	fp := &app.TunnelConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del tunnel etcd data
	err = etcd.EtcdDelValue(config.TunnelConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* connection */
func CreateConn(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateConn Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("CreateConn:" + string(data[:]))
	conf := &app.ConnConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* Conn 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.ConnConfPath + conf.Id)
	if err == nil {
		agentLog.AgentLogger.Info("conn EtcdHasExist")
		result = &Res{Success: true, Message: "EtcdHasExist"}
		return
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.ConnConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.ConnConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModConn(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModConn Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModConn:" + string(data[:]))
	confNew := &app.ConnConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Conn Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.ConnConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.ConnConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.ConnConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func ModConnStatic(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModConnStatic Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("ModConnStatic:" + string(data[:]))
	confNew := &app.ConnConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("Conn Id:", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.ConnConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.ConnConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.ModifyStatic(confNew)
	if err != nil {
		errPanic(ModError, ModError, err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.ConnConfPath+confNew.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DelConn(params map[string]string, vapType string) (result *Res) {
	sn, err := getValue(params, "sn")
	agentLog.AgentLogger.Info("Get SN " + sn)
	if err != nil {
		errPanic(ParamsError, ParamsError, errors.New(ParamsError))
	}
	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete Conn id: " + id)

	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(config.ConnConfPath + id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.ConnConfPath + id)
	agentLog.AgentLogger.Info("conn oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("conn not Exists")
		result = &Res{Success: true, Message: "ConfIsNotExists"}
		return
	}

	path := config.ConnConfPath + id
	fp := &app.ConnConf{}
	if err = public.GetDataFromEtcd(path, fp); err != nil {
		errPanic(GetEtcdDataError, GetEtcdDataError, err)
	}

	err = fp.Destroy()
	if err != nil {
		errPanic(DestroyError, DestroyError, err)
	}

	// del Conn etcd data
	err = etcd.EtcdDelValue(config.ConnConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}
	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + "delete success.")
	return
}

/* coreList */
func UpdateCoreList(params map[string]string, data []byte, vapType string) (result *Res) {
	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("UpdateCoreList Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	agentLog.AgentLogger.Info("UpdateCoreList:" + string(data[:]))
	conf := &app.CoreListConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("UpdateCoreList before data: " + string(byteep))
	if err := conf.Update(); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	result = &Res{Success: true}
	return
}

func PostDecorator(handler PostHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkContentType(w, r) {
			res, err := ioutil.ReadAll(r.Body)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			vars := mux.Vars(r)
			result := handler(vars, res, vapType)
			sendJson(result, w, r)
		} else {
			errPanic(ContentTypeError, ContentTypeError, nil)
		}
	}
}

func PutDecorator(handler PutHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkContentType(w, r) {
			res, err := ioutil.ReadAll(r.Body)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			vars := mux.Vars(r)
			result := handler(vars, res, vapType)
			sendJson(result, w, r)
		}
	}
}

func DelDecorator(hander DelHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson(result, w, r)
	}
}

func GetDecorator(hander GetHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson(result, w, r)
	}
}

func sendJson(m *Res, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

// 扩展 sendJson
// Ifs
func GetIfsDecorator(hander GetIfsHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson_Ifs(result, w, r)
	}
}

func sendJson_Ifs(m *IfsRes, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

// Rts
func GetRtsDecorator(hander GetRtsHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson_Rts(result, w, r)
	}
}

func sendJson_Rts(m *RtsRes, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

// Detail
func GetDetailDecorator(hander GetDetailHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson_Detail(result, w, r)
	}
}

func sendJson_Detail(m *DetailRes, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

// DataStr
func GetDataStrDecorator(hander GetDataStrHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson_DataStr(result, w, r)
	}
}

func sendJson_DataStr(m *DataStrRes, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

func checkContentType(w http.ResponseWriter, r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Content-Type"), "application/json; charset=utf-8") && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		result := Res{Success: false, Code: "Error: Content-type is error", Message: "Content-type : application/json"}
		agentLog.HttpLog("info", r, "Content-Type Error : "+r.Header.Get("Content-Type"))
		sendJson(&result, w, r)
		return false
	}
	return true
}

func handlesInit() {

	network.Init()

	err := etcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Info("Etcdinit.", err)
		os.Exit(1)
	} else {
		agentLog.AgentLogger.Info("init etcd client success.")
	}

	if err = app.InitBgpMonitor(); err != nil {
		agentLog.AgentLogger.Error("Init Bgp Monitor err: ", err)
	} else {
		agentLog.AgentLogger.Info("InitBgpMonitor ok.")
	}
}
