package serverstatus

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"math"
	"strconv"
	"time"
)

type Service struct {
	ServerStatus []map[string]interface{}
}

func NewService() *Service {
	return &Service{
		ServerStatus: make([]map[string]interface{}, 0, 1500),
	}
}

func StartNewServiceIfEnable(enable bool) *Service {
	if enable {
		s := NewService()
		s.Start()
		return s
	}
	return nil
}

func (s *Service) Start() {
	go s.getSeverStatus()
}

func (s *Service) getSeverStatus() {
	for {
		if len(s.ServerStatus) < 10 {
			time.Sleep(time.Second)
		} else {
			time.Sleep(time.Minute)
		}
		cpuPercent, _ := cpu.Percent(0, true)
		var cpuAll float64
		for _, v := range cpuPercent {
			cpuAll += v
		}
		m := make(map[string]interface{})
		loads, _ := load.Avg()
		m["load1"] = loads.Load1
		m["load5"] = loads.Load5
		m["load15"] = loads.Load15
		m["cpu"] = math.Round(cpuAll / float64(len(cpuPercent)))
		swap, _ := mem.SwapMemory()
		m["swap_mem"] = math.Round(swap.UsedPercent)
		vir, _ := mem.VirtualMemory()
		m["virtual_mem"] = math.Round(vir.UsedPercent)
		conn, _ := net.ProtoCounters(nil)
		io1, _ := net.IOCounters(false)
		time.Sleep(time.Millisecond * 500)
		io2, _ := net.IOCounters(false)
		if len(io2) > 0 && len(io1) > 0 {
			m["io_send"] = (io2[0].BytesSent - io1[0].BytesSent) * 2
			m["io_recv"] = (io2[0].BytesRecv - io1[0].BytesRecv) * 2
		}
		t := time.Now()
		m["time"] = strconv.Itoa(t.Hour()) + ":" + strconv.Itoa(t.Minute()) + ":" + strconv.Itoa(t.Second())

		for _, v := range conn {
			m[v.Protocol] = v.Stats["CurrEstab"]
		}
		if len(s.ServerStatus) >= 1440 {
			s.ServerStatus = s.ServerStatus[1:]
		}
		s.ServerStatus = append(s.ServerStatus, m)
	}
}
