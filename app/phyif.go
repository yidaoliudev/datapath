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
描述：Pop的物理接口与逻辑接口的绑定关系。
控制器调用API，将逻辑接口和与之对应Pop物理接口配置信息传入，程序会查询物理接口的Mac地址并记录。
*/
type PhyifConf struct {
	Id      string `json:"id"`      //(必填)逻辑接口名称，如：wan0、lan1、lan2、lan3、lan4
	MacAddr string `json:"macAddr"` //(必填)物理接口的MAC地址
	Device  string `json:"device"`  //{不填}Pop物理接口的名称
}

func (conf *PhyifConf) Create(action int) error {

	var err error

	cmdstr := fmt.Sprintf("ip addr | grep %s -B 1 | grep BROADCAST | awk '{print $2}' | awk -F ':' '{print $1}' | grep -v '@'", conf.MacAddr)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		return derr.Error{In: err.Error(), Out: "PhyifDevice"}
	} else {
		/* 记录device信息 */
		conf.Device = strings.Replace(result_str, "\n", "", -1)
	}

	/* 检查Device是否存在 */
	filepath := fmt.Sprintf("/sys/class/net/%s/address", conf.Device)
	if !public.FileExists(filepath) {
		return derr.Error{In: err.Error(), Out: "PhyifNotFound"}
	}

	/* 如果不是wan接口，则在rename之前set down. */
	if strings.Compare(strings.ToLower(conf.Id), strings.ToLower("wan0")) != 0 {
		if conf.Device != conf.Id {
			err = public.SetInterfaceLinkDown(conf.Device)
			if err != nil {
				return err
			}
		}
	}

	/* rename interface */
	if conf.Device != conf.Id {
		err = public.RenameInterface(conf.Device, conf.Id)
		if err != nil {
			return err
		}
	}

	/* set link up */
	err = public.SetInterfaceLinkUp(conf.Id)
	if err != nil {
		return err
	}

	return nil
}

func (conf *PhyifConf) Destroy() error {

	/* 如果不是wan接口，则在rename之前set down. */
	if strings.Compare(strings.ToLower(conf.Id), strings.ToLower("wan0")) != 0 {
		err := public.SetInterfaceLinkDown(conf.Id)
		if err != nil {
			return err
		}
	}

	/* 恢复成之前的名称 */
	err := public.RenameInterface(conf.Id, conf.Device)
	if err != nil {
		return err
	}

	return nil
}

func (conf *PhyifConf) Recover(action int) (bool, error) {

	var err error
	var chg = false
	var device = conf.Device

	/* 需要考虑重启恢复过程中，接口名称会变，所以需要根据MacAddr重新查找接口名称 */
	cmdstr := fmt.Sprintf("ip addr | grep %s -B 1 | grep BROADCAST | awk '{print $2}' | awk -F ':' '{print $1}' | grep -v '@'", conf.MacAddr)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		return false, derr.Error{In: err.Error(), Out: "PhyifDevice"}
	} else {
		device = strings.Replace(result_str, "\n", "", -1)
		if device != conf.Device {
			conf.Device = device
			chg = true
		}
	}

	/* rename interface */
	if conf.Device != conf.Id {
		err = public.RenameInterface(device, conf.Id)
		if err != nil {
			return false, err
		}
	}

	/* set link up */
	err = public.SetInterfaceLinkUp(conf.Id)
	if err != nil {
		return false, err
	}

	return chg, nil
}

func GetPhyifById(id string) (error, PhyifConf) {

	var find = false
	var phyif PhyifConf
	paths := []string{config.PhyifConfPath}
	phyifs, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.PhyifConfPath not found: ", err.Error())
	} else {
		for _, value := range phyifs {
			bytes := []byte(value)
			fp := &PhyifConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			phyif = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "PhyifNotFound"}, phyif
	}

	return nil, phyif
}
