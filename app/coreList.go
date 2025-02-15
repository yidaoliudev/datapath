package app

import (
	"bufio"
	"datapath/agentLog"
	"net"
	"os"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	CoreListPath = "/mnt/agent/coreList.conf"
)

/*

概述：coreList 作为配置POP控制器服务集群地址列表，生效后，由discover程序自动适应并及时更新可用的控制器服务。

*/

type CoreListConf struct {
	CoreList []string `json:"coreList"`
}

func WriteCoreListConf(coreList []string) error {

	// 打开文件，清空内容
	file, err := os.OpenFile(CoreListPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		agentLog.AgentLogger.Error("OpenFile coreList err: ", err)
		return err
	}
	defer file.Close()

	// 准备写入数据
	writer := bufio.NewWriter(file)

	// 写入每个字符串
	for _, line := range coreList {
		ipAddr := line
		if strings.Contains(line, "/") {
			ipAddr = strings.Split(line, "/")[0]
		}
		_, err := writer.WriteString(ipAddr + "\n")
		if err != nil {
			agentLog.AgentLogger.Error("WriteString coreList err: ", err)
			return err
		}
	}

	// 刷新缓冲区，确保所有数据都被写入
	err = writer.Flush()
	if err != nil {
		agentLog.AgentLogger.Error("Flush coreList err: ", err)
		return err
	}

	return nil
}

func (conf *CoreListConf) Update() error {

	var err error
	/* 如果数组为空，则直接返回 */
	if len(conf.CoreList) == 0 {
		return derr.Error{In: err.Error(), Out: "AddressListIsEmpty"}
	}

	/* 检查数据源格式，是否为IPv4地址 */
	for _, value := range conf.CoreList {
		ipAddr := value
		if strings.Contains(value, "/") {
			ipAddr = strings.Split(value, "/")[0]
		}

		if net.ParseIP(ipAddr).To4() == nil {
			agentLog.AgentLogger.Info(err, "Update coreList fail, address not IPv4: ", value)
			return derr.Error{In: err.Error(), Out: "AddressNotIpv4"}
		}
	}

	/* 更新coreList文件 */
	return WriteCoreListConf(conf.CoreList)
}
