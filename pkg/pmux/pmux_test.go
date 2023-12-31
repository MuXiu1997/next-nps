package pmux

import (
	"log/slog"
	"testing"
	"time"
)

func TestPortMux_Close(t *testing.T) {
	// logs.Reset()
	// logs.EnableFuncCallDepth(true)
	// logs.SetLogFuncCallDepth(3)

	pMux := NewPortMux(8888, "Ds")
	go func() {
		if pMux.Start() != nil {
			slog.Warn("Error")
		}
	}()
	time.Sleep(time.Second * 3)
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		slog.Warn("", "conn", conn, "err", err)
	}()
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		slog.Warn("", "conn", conn, "err", err)
	}()
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		slog.Warn("", "conn", conn, "err", err)
	}()
	l := pMux.GetHttpListener()
	conn, err := l.Accept()
	slog.Warn("", "conn", conn, "err", err)
}
