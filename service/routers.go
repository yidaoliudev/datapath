package main

import (
	"datapath/agentLog"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/gorilla/mux"
)

type Route struct {
	Name    string
	Method  string
	Path    string
	Handler http.HandlerFunc
}

type Routes []Route

var routes = Routes{
	Route{
		"createPhyif",
		"POST",
		"/pop/{sn}/phyif",
		PostDecorator(CreatePhyif, ""),
	},
	Route{
		"delPhyif",
		"DELETE",
		"/pop/{sn}/phyif/{id}",
		DelDecorator(DelPhyif, ""),
	},
	Route{
		"createPort",
		"POST",
		"/pop/{sn}/port",
		PostDecorator(CreatePort, ""),
	},
	Route{
		"modPort",
		"PUT",
		"/pop/{sn}/port",
		PutDecorator(ModPort, ""),
	},
	Route{
		"delPort",
		"DELETE",
		"/pop/{sn}/port/{id}",
		DelDecorator(DelPort, ""),
	},
	Route{
		"createVap",
		"POST",
		"/pop/{sn}/vap",
		PostDecorator(CreateVap, ""),
	},
	Route{
		"modVap",
		"PUT",
		"/pop/{sn}/vap",
		PutDecorator(ModVap, ""),
	},
	Route{
		"delVap",
		"DELETE",
		"/pop/{sn}/vap/{id}",
		DelDecorator(DelVap, ""),
	},
	Route{
		"createLinkEndp",
		"POST",
		"/pop/{sn}/link/endpoint",
		PostDecorator(CreateLinkEndp, ""),
	},
	Route{
		"modLinkEndp",
		"PUT",
		"/pop/{sn}/link/endpoint",
		PutDecorator(ModLinkEndp, ""),
	},
	Route{
		"delLinkEndp",
		"DELETE",
		"/pop/{sn}/link/{id}/endpoint",
		DelDecorator(DelLinkEndp, ""),
	},
	Route{
		"createLinkTran",
		"POST",
		"/pop/{sn}/link/tranpoint",
		PostDecorator(CreateLinkTran, ""),
	},
	Route{
		"modLinkTran",
		"PUT",
		"/pop/{sn}/link/tranpoint",
		PutDecorator(ModLinkTran, ""),
	},
	Route{
		"delLinkTran",
		"DELETE",
		"/pop/{sn}/link/{id}/tranpoint",
		DelDecorator(DelLinkTran, ""),
	},
	Route{
		"createVplEndp",
		"POST",
		"/pop/{sn}/vpl/endpoint",
		PostDecorator(CreateVplEndp, ""),
	},
	Route{
		"modVplEndp",
		"PUT",
		"/pop/{sn}/vpl/endpoint",
		PutDecorator(ModVplEndp, ""),
	},
	Route{
		"delVplEndp",
		"DELETE",
		"/pop/{sn}/vpl/{id}/endpoint",
		DelDecorator(DelVplEndp, ""),
	},
	Route{
		"createVplTran",
		"POST",
		"/pop/{sn}/vpl/tranpoint",
		PostDecorator(CreateVplTran, ""),
	},
	Route{
		"modVplTran",
		"PUT",
		"/pop/{sn}/vpl/tranpoint",
		PutDecorator(ModVplTran, ""),
	},
	Route{
		"delVplTran",
		"DELETE",
		"/pop/{sn}/vpl/{id}/tranpoint",
		DelDecorator(DelVplTran, ""),
	},
	Route{
		"createNat",
		"POST",
		"/pop/{sn}/nat",
		PostDecorator(CreateNat, ""),
	},
	Route{
		"modNat",
		"PUT",
		"/pop/{sn}/nat",
		PutDecorator(ModNat, ""),
	},
	Route{
		"delNat",
		"DELETE",
		"/pop/{sn}/nat/{id}",
		DelDecorator(DelNat, ""),
	},
	Route{
		"createNatshare",
		"POST",
		"/pop/{sn}/natshare",
		PostDecorator(CreateNatshare, ""),
	},
	Route{
		"modNatshare",
		"PUT",
		"/pop/{sn}/natshare",
		PutDecorator(ModNatshare, ""),
	},
	Route{
		"delNatshare",
		"DELETE",
		"/pop/{sn}/natshare/{id}",
		DelDecorator(DelNatshare, ""),
	},
	Route{
		"createEdge",
		"POST",
		"/pop/{sn}/edge",
		PostDecorator(CreateEdge, ""),
	},
	Route{
		"modEdge",
		"PUT",
		"/pop/{sn}/edge",
		PutDecorator(ModEdge, ""),
	},
	Route{
		"delEdge",
		"DELETE",
		"/pop/{sn}/edge/{id}",
		DelDecorator(DelEdge, ""),
	},
	Route{
		"checkEdgeRoutes",
		"GET",
		"/pop/{sn}/edge/{id}/routes",
		GetRtsDecorator(CheckToolRoutes, ""),
	},
	Route{
		"createTunnel",
		"POST",
		"/pop/{sn}/tunnel",
		PostDecorator(CreateTunnel, ""),
	},
	Route{
		"modTunnel",
		"PUT",
		"/pop/{sn}/tunnel",
		PutDecorator(ModTunnel, ""),
	},
	Route{
		"delTunnel",
		"DELETE",
		"/pop/{sn}/tunnel/{id}",
		DelDecorator(DelTunnel, ""),
	},
	Route{
		"createConn",
		"POST",
		"/pop/{sn}/conn",
		PostDecorator(CreateConn, ""),
	},
	Route{
		"modConn",
		"PUT",
		"/pop/{sn}/conn",
		PutDecorator(ModConn, ""),
	},
	Route{
		"modConnStatic",
		"PUT",
		"/pop/{sn}/connStatic",
		PutDecorator(ModConnStatic, ""),
	},
	Route{
		"delConn",
		"DELETE",
		"/pop/{sn}/conn/{id}",
		DelDecorator(DelConn, ""),
	},
	Route{
		"clearConnBgp",
		"GET",
		"/pop/{sn}/conn/{id}/bgpclear",
		DelDecorator(ClearConnBgp, ""),
	},
	Route{
		"initConnIpsec",
		"GET",
		"/pop/{sn}/conn/{id}/ipsecSa/init",
		GetDetailDecorator(InitConnIpsec, ""),
	},
	Route{
		"getConnIpsecSaInfo",
		"GET",
		"/pop/{sn}/conn/{id}/ipsecSa",
		GetDetailDecorator(GetConnIpsecSaInfo, ""),
	},
	Route{
		"createDnat",
		"POST",
		"/pop/{sn}/conn/{id}/dnat",
		PostDecorator(CreateDnat, ""),
	},
	Route{
		"modDnat",
		"PUT",
		"/pop/{sn}/conn/{id}/dnat",
		PutDecorator(ModDnat, ""),
	},
	Route{
		"delDnat",
		"DELETE",
		"/pop/{sn}/conn/{id}/dnat/{target}",
		DelDecorator(DelDnat, ""),
	},
	Route{
		"delDnatAll",
		"DELETE",
		"/pop/{sn}/conn/{id}/dnat",
		DelDecorator(DelDnatAll, ""),
	},
	Route{
		"createSnat",
		"POST",
		"/pop/{sn}/conn/{id}/snat",
		PostDecorator(CreateSnat, ""),
	},
	Route{
		"modSnat",
		"PUT",
		"/pop/{sn}/conn/{id}/snat",
		PutDecorator(ModSnat, ""),
	},
	Route{
		"delSnat",
		"DELETE",
		"/pop/{sn}/conn/{id}/snat/{target}",
		DelDecorator(DelSnat, ""),
	},
	Route{
		"delSnatAll",
		"DELETE",
		"/pop/{sn}/conn/{id}/snat",
		DelDecorator(DelSnatAll, ""),
	},
	Route{
		"updateTcp",
		"POST",
		"/pop/{sn}/tcp",
		PostDecorator(UpdateTcp, ""),
	},
	Route{
		"delTcp",
		"DELETE",
		"/pop/{sn}/tcp/{id}",
		DelDecorator(DelTcp, ""),
	},
	Route{
		"initVapIpsec",
		"GET",
		"/pop/{sn}/vap/{id}/ipsecSa/init",
		GetDetailDecorator(InitVapIpsec, ""),
	},
	Route{
		"getVapIpsecSaInfo",
		"GET",
		"/pop/{sn}/vap/{id}/ipsecSa",
		GetDetailDecorator(GetVapIpsecSaInfo, ""),
	},
	Route{
		"getPopIfsInfo",
		"GET",
		"/pop/{sn}/ifs",
		GetIfsDecorator(GetPopIfsInfo, ""),
	},
	Route{
		"checkToolRoutes",
		"GET",
		"/pop/{sn}/tools/{id}/routes",
		GetRtsDecorator(CheckToolRoutes, ""),
	},
	Route{
		"checkToolPing",
		"GET",
		"/pop/{sn}/tools/{id}/ping/{target}",
		GetDecorator(CheckToolPing, ""),
	},
	Route{
		"checkToolTcping",
		"GET",
		"/pop/{sn}/tools/{id}/tcping/{target}/{port}",
		GetDecorator(CheckToolTcping, ""),
	},
	Route{
		"startToolIperf3",
		"POST",
		"/pop/{sn}/tools/iperf3",
		PostDecorator(StartToolIperf3, ""),
	},
	Route{
		"stopToolIperf3",
		"DELETE",
		"/pop/{sn}/tools/iperf3/{id}",
		DelDecorator(StopToolIperf3, ""),
	},
	Route{
		"getToolIperf3Log",
		"GET",
		"/pop/{sn}/tools/iperf3/{edgeId}/log",
		GetDataStrDecorator(GetToolIperf3Log, ""),
	},
	Route{
		"clearToolIperf3Log",
		"DELETE",
		"/pop/{sn}/tools/iperf3/{edgeId}/log",
		DelDecorator(ClearToolIperf3Log, ""),
	},
	Route{
		"opsEcho",
		"GET",
		"/ops/echo",
		GetDataStrDecorator(OpsEcho, ""),
	},
	Route{
		"updateCoreList",
		"PUT",
		"/pop/{sn}/coreList",
		PutDecorator(UpdateCoreList, ""),
	},
}

func popAgentRouter() *mux.Router {
	router := mux.NewRouter()
	for _, route := range routes {
		var handler http.Handler
		handler = route.Handler
		handler = logger(handler)
		router.Methods(route.Method).Path(route.Path).Name(route.Name).Handler(handler)
	}

	return router
}

func logger(handler http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			runtime.LockOSThread()
			agentLog.AgentLogger.Info(fmt.Sprintf("%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr))
			defer httpLog(start, w, r)
			handler.ServeHTTP(w, r)
		})
}

func httpLog(start time.Time, w http.ResponseWriter, r *http.Request) {
	if msg := recover(); msg != nil {
		switch t := msg.(type) {
		case Msg:
			msgOb := msg.(Msg)
			//res, err := json.Marshal(msgOb.res)
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusOK, time.Since(start)))
			agentLog.AgentLogger.Error(msgOb)
			agentLog.AgentLogger.Error(msgOb.Err.Error())
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			if msgOb.res.Code == "500" {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			res, err := json.Marshal(msgOb.res.Message)
			if _, err = io.WriteString(w, string(res[:])); err != nil {
				agentLog.AgentLogger.Error("write body string error.")
			}
		case error:
			err := msg.(error)
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusOK, time.Since(start)))
			agentLog.AgentLogger.Error(err.Error())
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			if _, err = io.WriteString(w, `{"success": false, "code": "InternalError", }`); err != nil {
				agentLog.AgentLogger.Error("write body string error.")
			}
		default:
			agentLog.AgentLogger.Error(fmt.Sprintf("Error Type : %T", t))
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusInternalServerError, time.Since(start)))
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			http.Error(w, InternalError, http.StatusInternalServerError)
		}
	} else {
		agentLog.AgentLogger.Info(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
			r.RemoteAddr, http.StatusOK, time.Since(start)))
	}
}
