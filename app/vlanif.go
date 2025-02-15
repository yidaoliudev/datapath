package app

import (
	"datapath/public"
)

type VlanifConf struct {
	Name          string `json:"name"`          //[继承]Vlanif名称
	EdgeId        string `json:"edgeId"`        //[继承]如果EdgeId非空，则表示Edge内Vlanif
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	PhyifName     string `json:"phyifName"`     //[继承]Vlanif的物理接口名称
	VlanId        int    `json:"vlanId"`        //(必填)Vlan ID
	LocalAddress  string `json:"localAddress"`  //(必填)Vlanif本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Vlanif对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
}

func (conf *VlanifConf) Create(action int) error {

	/* create vlanif */
	err := public.CreateInterfaceTypeVlanif(conf.PhyifName, conf.Name, conf.VlanId)
	if err != nil {
		return err
	}

	if conf.EdgeId == "" {
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
		/* set vlan to Netns*/
		err = public.SetInterfaceNetns(conf.EdgeId, conf.Name)
		if err != nil {
			return err
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
		err = public.VrfSetInterfaceIngressLimit(conf.EdgeId, conf.Name, conf.Bandwidth)
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
	}

	return nil
}

func (cfgCur *VlanifConf) Modify(cfgNew *VlanifConf) (error, bool) {
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

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {

				err = public.SetInterfaceAddress(true, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return err, false
				}

				err = public.SetInterfaceAddress(false, cfgNew.Name, cfgNew.LocalAddress)
				if err != nil {
					return err, false
				}

				cfgCur.LocalAddress = cfgNew.LocalAddress
				chg = true
			}
			if cfgCur.RemoteAddress != cfgNew.RemoteAddress {
				if cfgCur.RemoteAddress != "" {
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
			err = public.VrfSetInterfaceIngressLimit(cfgCur.EdgeId, cfgCur.Name, cfgNew.Bandwidth)
			if err != nil {
				return err, false
			}
			cfgCur.Bandwidth = cfgNew.Bandwidth
			chg = true
		}

		if cfgCur.LocalAddress != cfgNew.LocalAddress ||
			cfgCur.RemoteAddress != cfgNew.RemoteAddress {

			if cfgCur.LocalAddress != cfgNew.LocalAddress {

				err = public.VrfSetInterfaceAddress(true, cfgCur.EdgeId, cfgCur.Name, cfgCur.LocalAddress)
				if err != nil {
					return err, false
				}

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

func (conf *VlanifConf) Destroy() error {

	if conf.EdgeId == "" {
		if conf.RemoteAddress != "" {
			/* (FRR) delete vlan RemoteAddress route */
			err := AddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name)
			if err != nil {
				return err
			}
		}

		/* destroy vlan */
		err := public.DeleteInterface(conf.Name)
		if err != nil {
			return err
		}
	} else {
		if conf.RemoteAddress != "" {
			/* (FRR) delete vlan RemoteAddress route */
			err := VrfAddRoute(true, conf.LocalAddress, conf.RemoteAddress, conf.Name, conf.EdgeId)
			if err != nil {
				return err
			}
		}

		/* destroy vlan */
		err := public.VrfDeleteInterface(conf.EdgeId, conf.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
