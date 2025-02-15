package main

import (
	"datapath/agentLog"
	"datapath/app"
	"datapath/public"
	v "datapath/version"
	"flag"
	"fmt"
	"net/http"
	"os"
)

const (
	LogName = "/var/log/pop/pop.log"
)

var (
	agentip, agentport *string
	version            *bool
	BgpMonitorInterval *int
)

func flagInit() error {
	version = flag.Bool("v", false, "print version and quit")

	agentip = flag.String("agentip", "0.0.0.0", "agent service ip")
	agentport = flag.String("agentport", "8001", "agent service port")
	BgpMonitorInterval = flag.Int("bgpMonitorInterval", 10, "bgp monitor interval")

	flag.Parse()
	if *version {
		fmt.Println(v.VERSION)
		os.Exit(0)
	}
	others := flag.Args()
	if len(others) > 0 {
		fmt.Println(fmt.Sprintf("unknown pop command %v, (`pop --help' for list)", others))
		os.Exit(0)
	}

	app.SetBgpPara(*BgpMonitorInterval)

	return nil
}

func main() {
	var err error

	if err = flagInit(); err != nil {
		fmt.Println("flagInit fail err: ", err)
		return
	}

	agentLog.Init(LogName)

	if err = public.PopConfigInit(); err != nil {
		fmt.Println("PopConfigInit fail err: ", err)
		return
	}

	if err := popHearbeatReport(); err != nil {
		fmt.Println("popHearbeatReport fail err: ", err)
		return
	}

	r := popAgentRouter()

	handlesInit()

	agentLog.AgentLogger.Info("start!! sn:", public.G_coreConf.Sn)
	agentLog.AgentLogger.Info(http.ListenAndServe(*agentip+":"+*agentport, r))
}
