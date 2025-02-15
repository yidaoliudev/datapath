package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Snat DstType: 目的地址类型 */
	DstType_Address = 0
	DstType_IpRange = 1
)

/*
概述：snat 作为独享NAT上，将分支不同cidr源网段指定不同的地址做SNAT。
*/
type SnatConf struct {
	Id          string   `json:"id"`          //(必填)
	ConnId      string   `json:"connId"`      //(必填)Conn ID
	EdgeId      string   `json:"edgeId"`      //(必填)Edge ID
	Source      []string `json:"source"`      //(必填)源地址网段
	Destination string   `json:"destination"` //[选填]目的地址
	DstRange    []string `json:"dstRange"`    //[选填]目的地址Range
	Priority    int      `json:"priority"`    //(必填)优先级
	DstType     int      `json:"dstType"`     //{不填}目的地址类型
	SnatName    string   `json:"snatName"`    //{不填}
}

func SnatRuleConfigre(undo bool, conf *SnatConf) error {

	var err error
	var destination string

	if len(conf.Source) == 0 {
		agentLog.AgentLogger.Info(err, "Snat rule source err fail: ")
		return derr.Error{In: err.Error(), Out: "SnatRuleSourceError"}
	}

	if conf.DstType == DstType_Address {
		destination = strings.Split(conf.Destination, "/")[0]
	} else if conf.DstType == DstType_IpRange {
		range_from := strings.Split(conf.DstRange[0], "/")[0]
		range_end := strings.Split(conf.DstRange[len(conf.DstRange)-1], "/")[0]
		destination = range_from + "-" + range_end
	}

	for _, src := range conf.Source {
		err = public.VrfSetInterfaceSnatBySource(undo, conf.EdgeId, conf.ConnId, src, destination)
		if err != nil {
			return err
		}
	}
	return nil
}

func (conf *SnatConf) Create(action int) error {
	var err error

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	/* get conn info */
	err, conn := GetConnInfoById(conf.ConnId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Create snat fail, connection not found.")
		return err
	}

	if conn.Type != ConnType_Nat {
		agentLog.AgentLogger.Info(err, "Create snat fail, connection type not NAT.")
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	/* set dstType */
	if conf.Destination != "" {
		conf.DstType = DstType_Address
	} else if len(conf.DstRange) != 0 {
		conf.DstType = DstType_IpRange
	} else {
		agentLog.AgentLogger.Info(err, "Create snat fail, DstType err.")
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	/* set ConnName */
	conf.SnatName = conf.ConnId + "_" + conf.Id
	conf.EdgeId = conn.EdgeId

	/* Conn接口增加second address */
	if conf.DstType == DstType_Address {
		public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.ConnId, conf.Destination)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Create snat fail, set ip address err.")
			return err
		}
	} else if conf.DstType == DstType_IpRange {
		nat := &conn.NatgwInfo
		natgwLocalAddress := nat.LocalAddress
		for _, destination := range conf.DstRange {
			if destination != natgwLocalAddress {
				public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.ConnId, destination)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Create snat fail, set ip address err.")
					return err
				}
			}
		}
	}

	/* create rule */
	err = SnatRuleConfigre(false, conf)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Create snat fail, configure iptables rule err.")
		return err
	}

	return nil
}

func (cfgCur *SnatConf) Modify(cfgNew *SnatConf) (error, bool) {

	var err error
	var destination string
	chg := false

	if cfgCur.Priority != cfgNew.Priority {
		cfgCur.Priority = cfgNew.Priority
		chg = true
	}

	if cfgCur.DstType == DstType_Address {
		destination = strings.Split(cfgCur.Destination, "/")[0]
	} else if cfgCur.DstType == DstType_IpRange {
		range_from := strings.Split(cfgCur.DstRange[0], "/")[0]
		range_end := strings.Split(cfgCur.DstRange[len(cfgCur.DstRange)-1], "/")[0]
		destination = range_from + "-" + range_end
	}

	add, delete := public.Arrcmp(cfgCur.Source, cfgNew.Source)
	if len(add) != 0 || len(delete) != 0 {
		if len(delete) != 0 {
			for _, src := range delete {
				err = public.VrfSetInterfaceSnatBySource(true, cfgCur.EdgeId, cfgCur.ConnId, src, destination)
				if err != nil {
					return err, false
				}
			}
		}

		if len(add) != 0 {
			for _, src := range add {
				err = public.VrfSetInterfaceSnatBySource(false, cfgCur.EdgeId, cfgCur.ConnId, src, destination)
				if err != nil {
					return err, false
				}
			}
		}

		/* change */
		cfgCur.Source = cfgNew.Source
		chg = true
	}

	return nil, chg
}

func (conf *SnatConf) Destroy() error {

	var err error
	/* get conn info */
	err, conn := GetConnInfoById(conf.ConnId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Destroy snat fail, connection not found.")
		return nil
	}

	if conn.Type != ConnType_Nat {
		agentLog.AgentLogger.Info(err, "Destroy snat fail, connection type not NAT.")
		return nil
	}

	err = SnatRuleConfigre(true, conf)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Destroy snat fail, configure iptables rule err.")
		return err
	}

	/* Conn接口删除second address */
	if conf.DstType == DstType_Address {
		public.VrfSetInterfaceAddress(true, conf.EdgeId, conf.ConnId, conf.Destination)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Create snat fail, delete ip address err.")
			return err
		}
	} else if conf.DstType == DstType_IpRange {
		nat := &conn.NatgwInfo
		natgwLocalAddress := nat.LocalAddress
		for _, destination := range conf.DstRange {
			if destination != natgwLocalAddress {
				public.VrfSetInterfaceAddress(true, conf.EdgeId, conf.ConnId, destination)
				if err != nil {
					agentLog.AgentLogger.Info(err, "Create snat fail, set ip address err.")
					return err
				}
			}
		}
	}

	return nil
}
