package pprof

import (
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
)

func RunPProf(ip string, port uint16) {
	addr := fmt.Sprintf("%s:%d", ip, port)
	go func() {
		_ = http.ListenAndServe(addr, nil)
	}()
	slog.Info(fmt.Sprintf("PProf debug listen on %s", addr))
}

func RunPProfIfEnable(ip string, port uint16, enable bool) {
	if enable {
		RunPProf(ip, port)
	}
}
