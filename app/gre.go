package app

import (
	"datapath/public"
	"strings"
)

type GreConf struct {
	Name          string `json:"name"`          //[继承]Gre名称
	RootId        string `json:"rootId"`        //[继承]如果rootId非空，则表示需要在rootId内创建gre
	EdgeId        string `json:"edgeId"`        //[继承]如果edgeId非空，则表示edge内部gre
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	TunnelSrc     string `json:"tunnelSrc"`     //(必填)Gre本端地址
	TunnelDst     string `json:"tunnelDst"`     //(必填)Gre对端地址
	LocalAddress  string `json:"localAddress"`  //(必填)Gre 隧道本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Gre 隧道对端地址
	GreKey        int    `json:"greKey"`        //(必填)Gre key
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
}

func (conf *GreConf) Create(action int) error {

	var err error

	if conf.EdgeId == "" {
		/* create gre tunnel */
		err := public.CreateInterfaceTypeGre(conf.Name, conf.TunnelSrc, conf.TunnelDst, conf.GreKey)
		if err != nil {
			return err
		}

		/* set link up */
		err = public.SetInterfaceLinkUp(conf.Name)
		if err != nil {
			return err
		}

		/* set ip address */
		err = public.SetInterfaceAddress(false, conf.Name, conf.LocalAddress)
		if err != nil {
			return err
		}

		/* set tc limit */
		err = public.SetInterfaceIngressLimit(conf.Name, conf.Bandwidth)
		if err != nil {
			return err
		}

		if action == public.ACTION_ADD && conf.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			err = AddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			if err != nil {
				return err
			}
		}

	} else {
		if conf.RootId == "" {
			/* create gre tunnel */
			err := public.CreateInterfaceTypeGre(conf.Name, conf.TunnelSrc, conf.TunnelDst, conf.GreKey)
			if err != nil {
				return err
			}

			/* set gre to Netns*/
			err = public.SetInterfaceNetns(conf.EdgeId, conf.Name)
			if err != nil {
				return err
			}
		} else {
			/* create gre tunnel */
			err := public.VrfCreateInterfaceTypeGre(conf.RootId, conf.Name, conf.TunnelSrc, conf.TunnelDst, conf.GreKey)
			if err != nil {
				return err
			}

			/* set gre to Netns*/
			err = public.VrfSetInterfaceNetns(conf.RootId, conf.EdgeId, conf.Name)
			if err != nil {
				return err
			}
		}

		/* set link up */
		err = public.VrfSetInterfaceLinkUp(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}

		/* set ip address */
		err = public.VrfSetInterfaceAddress(false, conf.EdgeId, conf.Name, conf.LocalAddress)
		if err != nil {
			return err
		}

		/* set tc limit */
		if strings.Contains(conf.Name, "tunn") {
			err = public.VrfSetInterfaceEgressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
		} else {
			err = public.VrfSetInterfaceIngressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
		}
		if err != nil {
			return err
		}

		if action == public.ACTION_ADD && conf.RemoteAddress != "" {
			/* (FRR) add RemoteAddress route */
			err = VrfAddRoute(false, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			if err != nil {
				return err
			}
		}

		/* set lo link up */
		err = public.VrfSetInterfaceLinkUp(conf.EdgeId, "lo")
		if err != nil {
			return err
		}
	}
	return nil
}

func (cfgCur *GreConf) Modify(cfgNew *GreConf) (error, bool) {

	var chg = false
	var err error

	if cfgCur.HealthCheck != cfgNew.HealthCheck {
		cfgCur.HealthCheck = cfgNew.HealthCheck
		chg = true
	}

	if cfgCur.EdgeId == "" {

		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			err = public.SetInterfaceIngressLimit(cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return err, false
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			chg = true
		}

		if cfgCur.TunnelSrc != cfgNew.TunnelSrc || cfgCur.TunnelDst != cfgNew.TunnelDst || cfgCur.GreKey != cfgNew.GreKey {
			err = public.ModifyInterfaceTypeGre(cfgCur.Name, cfgNew.TunnelSrc, cfgNew.TunnelDst, cfgNew.GreKey)
			if err != nil {
				return err, false
			}
			cfgCur.TunnelSrc = cfgNew.TunnelSrc
			cfgCur.TunnelDst = cfgNew.TunnelDst
			cfgCur.GreKey = cfgNew.GreKey
			chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {

				/* delete old ip address */
				err = public.SetInterfaceAddress(true, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return err, false
				}

				/* add new ip address */
				err = public.SetInterfaceAddress(false, cfgNew.Name, cfgNew.LocalAddress)
				if err != nil {
					return err, false
				}

				cfgCur.LocalAddress = cfgNew.LocalAddress
				chg = true
			}
			if cfgCur.RemoteAddress != cfgNew.RemoteAddress {

				find := false
				/* 如果是vap，需要检查vap关联linkendp实例的RemoteAddress地址是否和vap旧的RemoteAddress地址相同，如果相同，则不能删除 */
				if strings.Contains(cfgCur.Name, "vap") {
					find = CheckLinkEndpRemoteAddrExist(cfgCur.Name, cfgCur.RemoteAddress)
				}
				if !find && cfgCur.RemoteAddress != "" {
					/* (FRR) del RemoteAddress route */
					err = AddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name)
					if err != nil {
						return err, false
					}
				}

				if cfgNew.RemoteAddress != "" {
					/* (FRR) add RemoteAddress route */
					err = AddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgNew.Name)
					if err != nil {
						return err, false
					}
				}
				cfgCur.RemoteAddress = cfgNew.RemoteAddress
				chg = true
			}
		}
	} else {
		if cfgCur.Bandwidth != cfgNew.Bandwidth {
			/* set tc limit */
			if strings.Contains(cfgCur.Name, "tunn") {
				err = public.VrfSetInterfaceEgressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
			} else {
				err = public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
			}
			if err != nil {
				return err, false
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			chg = true
		}

		if cfgCur.TunnelSrc != cfgNew.TunnelSrc || cfgCur.TunnelDst != cfgNew.TunnelDst {
			err = public.VrfModifyInterfaceTypeGre(cfgCur.EdgeId, cfgCur.Name, cfgNew.TunnelSrc, cfgNew.TunnelDst)
			if err != nil {
				return err, false
			}
			cfgCur.TunnelSrc = cfgNew.TunnelSrc
			cfgCur.TunnelDst = cfgNew.TunnelDst
			chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {
				/* delete old ip address */
				err = public.VrfSetInterfaceAddress(true, cfgCur.EdgeId, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return err, false
				}

				/* add new ip address */
				err = public.VrfSetInterfaceAddress(false, cfgNew.EdgeId, cfgNew.Name, cfgNew.LocalAddress)
				if err != nil {
					return err, false
				}
				cfgCur.LocalAddress = cfgNew.LocalAddress
				chg = true
			}

			if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
				if cfgCur.RemoteAddress != "" {
					/* (FRR) del RemoteAddress route */
					err := VrfAddRoute(true, cfgCur.LocalAddress, cfgCur.RemoteAddress, cfgCur.Name, cfgCur.EdgeId)
					if err != nil {
						return err, false
					}
				}

				if cfgNew.RemoteAddress != "" {
					/* (FRR) add RemoteAddress route */
					err := VrfAddRoute(false, cfgCur.LocalAddress, cfgNew.RemoteAddress, cfgNew.Name, cfgCur.EdgeId)
					if err != nil {
						return err, false
					}
				}
				cfgCur.RemoteAddress = cfgNew.RemoteAddress
				chg = true
			}
		}
	}

	return nil, chg
}

func (conf *GreConf) Destroy() error {

	if conf.EdgeId == "" {
		if conf.RemoteAddress != "" {
			/* (FRR) delete gre RemoteAddress route */
			err := AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			if err != nil {
				return err
			}
		}

		/* destroy gre */
		err := public.DeleteInterface(conf.Name)
		if err != nil {
			return err
		}
	} else {
		if conf.RemoteAddress != "" {
			/* (FRR) delete gre RemoteAddress route */
			err := VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			if err != nil {
				return err
			}
		}

		/* destroy gre */
		err := public.VrfDeleteInterface(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
