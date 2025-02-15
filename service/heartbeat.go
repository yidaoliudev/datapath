package main

import (
	"context"
	"datapath/agentLog"
	"datapath/public"
	v "datapath/version"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"gitlab.daho.tech/gdaho/log"
)

type httpInfo struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Msg     string `json:"msg"`
}

var heartBeatInterval int = 30

func popHearbeatReport() error {

	var buf [2048]byte

	cmdstr := fmt.Sprintf("echo $(date '+%Y-%m-%d %H:%M:%S')")
	err, time := public.ExecBashCmdWithRet(cmdstr)
	if err != nil || time == "" {
		agentLog.AgentLogger.Error(cmdstr, err, "HeartBeat get time fail: ", time)
		os.Exit(0)
		return err
	}

	/* 第一次上报心跳失败了程序就退出 */
	cmdstr = fmt.Sprintf("/usr/bin/curl  -X PUT -H 'X-Request-Source: admin-api' -d  '{\"version\":\"%s\"}' -L -k -s %s://%s:%d/api/dpConfig/pops/%s/heartbeat",
		v.VERSION, public.G_coreConf.CoreProto, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.Sn)
	err, res_str := public.ExecBashCmdWithRet(cmdstr)
	log.Warning("cmd_str:", cmdstr, " res_str", res_str, " err", err)
	if err != nil {
		agentLog.AgentLogger.Info("HeartBeat curl err :", err)
		os.Exit(0)
		return err
	} else {
		fp := &httpInfo{}
		if err := json.Unmarshal([]byte(res_str), fp); err != nil {
			agentLog.AgentLogger.Info("HeartBeat httpInfo err :", err)
			return err
		}
		if !fp.Success {
			log.Warning("HeartBeat cmd_str:", cmdstr, "err:", err, "res_str:", res_str)
			fmt.Println(fp.Code, fp.Msg)
			os.Exit(0)
			return err
		}

	}

	public.G_HeartBeatInfo = public.HeartBeatInfo{v.VERSION}
	bytedata, err := json.Marshal(public.G_HeartBeatInfo)
	if err != nil {
		agentLog.AgentLogger.Error("HeartBeat Marshal HeartBeatInfo err:", err)
		return err
	}

	url := fmt.Sprintf("/api/dpConfig/pops/%s/heartbeat", public.G_coreConf.Sn)
	ctx, _ := context.WithCancel(context.Background())
	agentLog.AgentLogger.Info("HeartBeat init ok, url", url, " G_HeartBeatInfo: ", public.G_HeartBeatInfo)
	go func() {
		for {
			defer func() {
				agentLog.AgentLogger.Info("HeartBeat defer heatBeatReport success")
				n := runtime.Stack(buf[:], true)
				agentLog.AgentLogger.Info(string(buf[:]), n)
				if err := recover(); err != nil {
					agentLog.AgentLogger.Error("HeartBeat heartbeat err:", err)
				}
			}()

			if err := heatBeatReport(ctx, bytedata, url); err == nil {
				agentLog.AgentLogger.Info("HeartBeat heatBeatReport success")
				return
			} else {
				agentLog.AgentLogger.Error("HeartBeat heatBeatReport err:", err)
			}
		}
	}()

	return nil
}

func heatBeatReport(ctx context.Context, res []byte, url string) error {

	tick := time.NewTicker(time.Duration(heartBeatInterval) * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			_, err := public.RequestCore(res, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
			if err != nil {
				///agentLog.AgentLogger.Error("HeartBeat RequestCore err : ", err)
				log.Warning("HeartBeat RequestCore err : ", err)
			}

		case <-ctx.Done():
			agentLog.AgentLogger.Info("<-ctx.Done()")
			// 必须返回成功
			return nil
		}
	}
}
