package app

import (
	"datapath/agentLog"
	"datapath/config"
	"datapath/etcd"
	"datapath/public"
	"encoding/json"

	"gitlab.daho.tech/gdaho/util/derr"
)

/*
LinkEndp 为link的端节点。
*/
type LinkEndpConf struct {
	Id            string `json:"id"`            //(必填)
	VapId         string `json:"vapId"`         //(必填)本端Vap Id
	LocalAddress  string `json:"localAddress"`  //(必填)本端Link地址（配置在vap实例的接口上）， Partner全局唯一，可以和vap的接口地址一致
	RemoteAddress string `json:"remoteAddress"` //(必填)对端Link地址， Partner全局唯一
	HealthCheck   bool   `json:"healthCheck"`   //[选填]Link健康检查，默认true(启动)，探测PeerVapAddr的时延，丢包等质量。
	VapNexthop    string `json:"vapNexthop"`    //[选填]如果是L2 Network，需要指定vap的nexthop
}

/*
LinkTran 为link的中转节点。
*/
type LinkTranConf struct {
	Id          string `json:"id"`          //(必填)
	VapIdA      string `json:"vapIdA"`      //(必填)Vap-A Id
	VapIdZ      string `json:"vapIdZ"`      //(必填)Vap-Z Id
	AddressA    string `json:"addressA"`    //(必填)Vap-A 关联地址
	AddressZ    string `json:"addressZ"`    //(必填)Vap-Z 关联地址
	VapNexthopA string `json:"vapNexthopA"` //[选填]记录Vap-A的Nexthop信息，如果是L2 Network，需要指定vap的nexthop
	VapNexthopZ string `json:"vapNexthopZ"` //[选填]记录Vap-Z的Nexthop信息，如果是L2 Network，需要指定vap的nexthop
}

/*

LinkEndpConf

*/
func (conf *LinkEndpConf) Create(action int) error {

	/* 获取Link对应的Vap信息 */
	err, vap := GetVapInfoById(conf.VapId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "get Vap info fail: ", conf.VapId)
		return err
	}

	if conf.LocalAddress != vap.LocalAddress {
		/* set ip address */
		err = public.SetInterfaceAddress(false, vap.VapName, conf.LocalAddress)
		if err != nil {
			return err
		}
	}

	if action == public.ACTION_ADD && conf.RemoteAddress != "" {
		/* (FRR) add route RemoteAddress via vap */
		if conf.VapNexthop != "" {
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop)
		} else {
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, vap.VapNexthop)
			conf.VapNexthop = vap.VapNexthop
		}
		if err != nil {
			return err
		}
	}

	/* 默认开启 */
	conf.HealthCheck = true
	return nil
}

func (cfgCur *LinkEndpConf) Modify(cfgNew *LinkEndpConf) (error, bool) {

	return nil, false
}

func (conf *LinkEndpConf) Destroy() error {

	/* 获取Link对应的Vap信息 */
	err, vap := GetVapInfoById(conf.VapId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "get Port info fail: ", conf.VapId)
		return err
	}

	if conf.LocalAddress != vap.LocalAddress {
		/* set ip address */
		err = public.SetInterfaceAddress(true, vap.VapName, conf.LocalAddress)
		if err != nil {
			return err
		}
	}

	if conf.RemoteAddress != "" {
		/* 如果是linkendp，需要检查linkendp关联vap实例的RemoteAddress地址是否和linkendp的RemoteAddress地址相同，如果相同，则不能删除 */
		find := CheckVapRemoteAddrExist(conf.VapId, conf.RemoteAddress)
		if !find {
			/* (FRR) delelte route RemoteAddress via vap */
			if conf.VapNexthop != "" {
				err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.VapNexthop)
			} else {
				err = AddRoute(true, conf.LocalAddress, conf.RemoteAddress, vap.VapNexthop)
			}
			if err != nil {
				return err
			}
		}
	}

	return nil
}

/*

LinkTranConf / VplTranConf

*/
func (conf *LinkTranConf) Create(action int) error {

	/* 获取A端对应的Vap信息 */
	err, vapA := GetVapInfoById(conf.VapIdA)
	if err != nil {
		agentLog.AgentLogger.Info(err, "get Vap-A info fail: ", conf.VapIdA)
		return err
	}

	err, vapZ := GetVapInfoById(conf.VapIdZ)
	if err != nil {
		agentLog.AgentLogger.Info(err, "get Vap-Z info fail: ", conf.VapIdZ)
		return err
	}

	if action == public.ACTION_ADD && conf.AddressA != "" {
		/* (FRR) add route AddressA to vap-A */
		if conf.VapNexthopA != "" {
			err = AddRoute(false, conf.AddressZ, conf.AddressA, conf.VapNexthopA)
		} else {
			err = AddRoute(false, conf.AddressZ, conf.AddressA, vapA.VapNexthop)
			conf.VapNexthopA = vapA.VapNexthop
		}
		if err != nil {
			return err
		}
	}

	if action == public.ACTION_ADD && conf.AddressZ != "" {
		/* (FRR) add route AddressA to vap-A */
		if conf.VapNexthopZ != "" {
			err = AddRoute(false, conf.AddressA, conf.AddressZ, conf.VapNexthopZ)
		} else {
			err = AddRoute(false, conf.AddressA, conf.AddressZ, vapZ.VapNexthop)
			conf.VapNexthopZ = vapZ.VapNexthop
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (cfgCur *LinkTranConf) Modify(cfgNew *LinkTranConf) (error, bool) {

	return nil, false
}

func (conf *LinkTranConf) Destroy() error {

	if conf.AddressA != "" {
		/* (FRR) del route AddressA to vap-A */
		err := AddRoute(true, conf.AddressZ, conf.AddressA, conf.VapNexthopA)
		if err != nil {
			return err
		}
	}

	if conf.AddressZ != "" {
		/* (FRR) del route AddressZ to vap-Z */
		err := AddRoute(true, conf.AddressA, conf.AddressZ, conf.VapNexthopZ)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetLinkEndpInfoById(id string) (error, LinkEndpConf) {

	var find = false
	var linkEndp LinkEndpConf
	paths := []string{config.LinkEndpConfPath}
	linkEndps, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.LinkEndpConfPath not found: ", err.Error())
	} else {
		for _, value := range linkEndps {
			bytes := []byte(value)
			fp := &LinkEndpConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			linkEndp = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "linkEndpNotFound"}, linkEndp
	}

	return nil, linkEndp
}

func CheckLinkEndpRemoteAddrExist(vapId, remoteAddress string) bool {

	var find = false
	paths := []string{config.LinkEndpConfPath}
	linkEndps, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.LinkEndpConfPath not found: ", err.Error())
	} else {
		for _, value := range linkEndps {
			bytes := []byte(value)
			fp := &LinkEndpConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.VapId == vapId && fp.RemoteAddress == remoteAddress {
				find = true
				break
			}
		}
	}

	return find
}
