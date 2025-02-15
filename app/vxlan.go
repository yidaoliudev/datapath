package app

type VxlanConf struct {
	Name          string `json:"name"`          //[继承]Vxlan名称
	EdgeId        string `json:"edgeId"`        //[继承]如果edgeId非空，则表示edge内部Vxlan
	Bandwidth     int    `json:"bandwidth"`     //[继承]带宽限速，单位Mbps，默认0
	TunnelSrc     string `json:"tunnelSrc"`     //(必填)Vxlan本端地址
	TunnelDst     string `json:"tunnelDst"`     //(必填)Vxlan对端地址
	VxlanId       int    `json:"vxlanId"`       //(必填)Vxlan ID
	VxlanPort     int    `json:"vxlanPort"`     //(必填)Vxlan Port
	LocalAddress  string `json:"localAddress"`  //(必填) 隧道本端地址
	RemoteAddress string `json:"remoteAddress"` //(必填)Vxlan 隧道对端地址
	HealthCheck   bool   `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
}
