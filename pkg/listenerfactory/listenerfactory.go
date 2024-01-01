package listenerfactory

import (
	"fmt"
	"github.com/MuXiu1997/next-nps/pkg/pmux"
	"log/slog"
	"net"
	"net/netip"
)

type ListenerFactory struct {
	pMux           *pmux.PortMux
	bridgeAddrPort *netip.AddrPort
	httpAddrPort   *netip.AddrPort
	httpsAddrPort  *netip.AddrPort
	webAddrPort    *netip.AddrPort
}

func NewListenerFactory(
	bridgeAddr *netip.AddrPort,
	httpAddr *netip.AddrPort,
	httpsAddr *netip.AddrPort,
	webAddr *netip.AddrPort,
) *ListenerFactory {
	f := &ListenerFactory{
		bridgeAddrPort: bridgeAddr,
		httpAddrPort:   httpAddr,
		httpsAddrPort:  httpsAddr,
		webAddrPort:    webAddr,
	}
	f.init()
	return f
}

func (f *ListenerFactory) init() {
	if f.httpAddrPort.Port() == f.bridgeAddrPort.Port() || f.httpsAddrPort.Port() == f.bridgeAddrPort.Port() || f.webAddrPort.Port() == f.bridgeAddrPort.Port() {
		f.pMux = pmux.NewPortMux(int(f.bridgeAddrPort.Port()), f.webAddrPort.Addr().String())
	}
}

func (f *ListenerFactory) GetBridgeTcpListener() (net.Listener, error) {
	slog.Info(fmt.Sprintf("server start, the bridge type is tcp, the bridge port is %d", f.bridgeAddrPort.Port()))
	if f.pMux != nil {
		return f.pMux.GetClientListener(), nil
	}
	return net.ListenTCP("tcp", net.TCPAddrFromAddrPort(*f.bridgeAddrPort))
}

func (f *ListenerFactory) GetHttpListener() (net.Listener, error) {
	slog.Info(fmt.Sprintf("start http listener, port is %d", f.httpAddrPort.Port()))
	if f.pMux != nil && f.httpAddrPort.Port() == f.bridgeAddrPort.Port() {
		return f.pMux.GetHttpListener(), nil
	}
	return net.ListenTCP("tcp", net.TCPAddrFromAddrPort(*f.httpAddrPort))
}

func (f *ListenerFactory) GetHttpsListener() (net.Listener, error) {
	slog.Info(fmt.Sprintf("start https listener, port is %d", f.httpsAddrPort.Port()))
	if f.pMux != nil && f.httpsAddrPort.Port() == f.bridgeAddrPort.Port() {
		return f.pMux.GetHttpsListener(), nil
	}
	return net.ListenTCP("tcp", net.TCPAddrFromAddrPort(*f.httpsAddrPort))
}

func (f *ListenerFactory) GetWebManagerListener() (net.Listener, error) {
	slog.Info(fmt.Sprintf("web management start, access port is %d", f.webAddrPort.Port()))
	if f.pMux != nil && f.webAddrPort.Port() == f.bridgeAddrPort.Port() {
		return f.pMux.GetManagerListener(), nil
	}
	return net.ListenTCP("tcp", net.TCPAddrFromAddrPort(*f.webAddrPort))
}
