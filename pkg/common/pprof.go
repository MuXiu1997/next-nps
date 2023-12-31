package common

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"net/http"
	_ "net/http/pprof"
)

func RunPProf(ip string, port int) {
	addr := fmt.Sprintf("%s:%d", ip, port)
	go func() {
		_ = http.ListenAndServe(addr, nil)
	}()
	logs.Info("PProf debug listen on", addr)
}

func RunPProfIfEnable(ip string, port int, enable bool) {
	if enable {
		RunPProf(ip, port)
	}
}
