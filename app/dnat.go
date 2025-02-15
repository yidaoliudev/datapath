package app

import (
	"datapath/agentLog"
	"datapath/public"
	"errors"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Dnat Protocol Type */
	DnatTypeProtoAny = 0
	DnatTypeProtoTcp = 6
	DnatTypeProtoUdp = 17
)

/*
概述：dnat 作为独享NAT上，通过NAT地址端口映射到内网主机服务。
*/
type DnatConf struct {
	Id          string `json:"id"`          //(必填)
	ConnId      string `json:"connId"`      //(必填)Conn ID
	EdgeId      string `json:"edgeId"`      //(必填)Edge ID
	Source      string `json:"source"`      //(必填)源地址
	Destination string `json:"destination"` //(必填)目的地址
	Protocol    int    `json:"protocol"`    //[选填]协议，udp(17) 或者 tcp(6)，如果是一对一，则为any(0)
	PortSrc     int    `json:"portSrc"`     //[选填]源端口,范围 1-65535，如果是一对一，则为0
	PortDst     int    `json:"portDst"`     //[选填]目的端口，范围 1-65535，如果是一对一，则为0
	Priority    int    `json:"priority"`    //(必填)优先级
	DnatName    string `json:"dnatName"`    //{不填}
}

/*
	func areArraysEqual(arr1, arr2 []DnatConf) bool {
		if len(arr1) != len(arr2) {
			return false
		}
		for i, v := range arr1 {
			if v != arr2[i] {
				return false
			}
		}
		return true
	}
*/
func DnatRuleConfigre(undo bool, conf *DnatConf) error {

	var err error
	if conf.Destination == "" {
		agentLog.AgentLogger.Info(err, "Dnat rule destination err fail: ", conf.Destination)
		return derr.Error{In: err.Error(), Out: "DnatRuleDestinationError"}
	}

	if conf.Protocol == DnatTypeProtoAny {
		err = public.VrfSetInterfaceDnat(undo, conf.EdgeId, conf.ConnId, conf.Source, 0, "any", conf.Destination, 0)
		if err != nil {
			return err
		}
	} else if conf.Protocol == DnatTypeProtoUdp {
		err = public.VrfSetInterfaceDnat(undo, conf.EdgeId, conf.ConnId, conf.Source, conf.PortSrc, "udp", conf.Destination, conf.PortDst)
		if err != nil {
			return err
		}
	} else if conf.Protocol == DnatTypeProtoTcp {
		err = public.VrfSetInterfaceDnat(undo, conf.EdgeId, conf.ConnId, conf.Source, conf.PortSrc, "tcp", conf.Destination, conf.PortDst)
		if err != nil {
			return err
		}
	} else {
		return derr.Error{In: err.Error(), Out: "DnatRuleProtoError"}
	}

	return nil
}

func (conf *DnatConf) Create(action int) error {
	var err error

	if !public.NsExist(conf.EdgeId) {
		return errors.New("VrfNotExist")
	}

	/* get conn info */
	err, conn := GetConnInfoById(conf.ConnId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Create dnat fail, connection not found.")
		return err
	}

	if conn.Type != ConnType_Nat {
		agentLog.AgentLogger.Info(err, "Create dnat fail, connection type not NAT.")
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	/* set ConnName */
	conf.DnatName = conf.ConnId + "_" + conf.Id
	conf.EdgeId = conn.EdgeId
	conf.Source = strings.Split(conn.NatgwInfo.LocalAddress, "/")[0]

	/* create rule */
	err = DnatRuleConfigre(false, conf)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Create dnat fail, configure iptables rule err.")
		return err
	}

	return nil
}

func (cfgCur *DnatConf) Modify(cfgNew *DnatConf) (error, bool) {

	var err error
	chg := false

	if cfgCur.Priority != cfgNew.Priority {
		cfgCur.Priority = cfgNew.Priority
		chg = true
	}

	if cfgCur.Protocol != cfgNew.Protocol ||
		cfgCur.PortSrc != cfgNew.PortSrc ||
		cfgCur.PortDst != cfgNew.PortDst ||
		cfgCur.Destination != cfgNew.Destination {

		err = cfgCur.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify dnat fail, Destroy rule false.")
			return err, false
		}
		err = cfgNew.Create(public.ACTION_ADD)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Modify dnat fail, Create rule false.")
			return err, false
		}
		cfgCur.Protocol = cfgNew.Protocol
		cfgCur.PortSrc = cfgNew.PortSrc
		cfgCur.PortDst = cfgNew.PortDst
		cfgCur.Destination = cfgNew.Destination
		chg = true
	}

	return nil, chg
}

func (conf *DnatConf) Destroy() error {

	var err error
	/* get conn info */
	err, conn := GetConnInfoById(conf.ConnId)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Destroy dnat fail, connection not found.")
		return nil
	}

	if conn.Type != ConnType_Nat {
		agentLog.AgentLogger.Info(err, "Destroy dnat fail, connection type not NAT.")
		return nil
	}

	err = DnatRuleConfigre(true, conf)
	if err != nil {
		agentLog.AgentLogger.Info(err, "Destroy dnat fail, configure iptables rule err.")
		return err
	}

	return nil
}
