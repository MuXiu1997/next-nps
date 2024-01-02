package bridge

import (
	mux "ehang.io/nps-mux"
	"errors"
	"fmt"
	"github.com/MuXiu1997/next-nps/pkg/common/constant"
	"github.com/MuXiu1997/next-nps/pkg/common/utils"
	"github.com/MuXiu1997/next-nps/pkg/conn"
	cpt "github.com/MuXiu1997/next-nps/pkg/crypt"
	"github.com/MuXiu1997/next-nps/pkg/db"
	"github.com/MuXiu1997/next-nps/pkg/listenerfactory"
	"github.com/MuXiu1997/next-nps/pkg/server/proxy"
	set "github.com/deckarep/golang-set/v2"
	cmap "github.com/orcaman/concurrent-map/v2"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

var _ proxy.NetBridge = (*Bridge)(nil)

type Bridge struct {
	allowedPorts      set.Set[uint16]
	listenerFactory   *listenerfactory.ListenerFactory
	BindAddrPort      netip.AddrPort
	BindType          string // BindType can be kcp or tcp
	P2PAddrPort       netip.AddrPort
	Clients           cmap.ConcurrentMap[int, *Client]
	Register          cmap.ConcurrentMap[string, time.Time]
	OpenTask          chan *db.Tunnel
	CloseTask         chan *db.Tunnel
	CloseClient       chan int
	SecretChan        chan *conn.Secret
	ipVerify          bool
	RunningServices   cmap.ConcurrentMap[int, *proxy.Service]
	disconnectTimeout int
	version           string // TODO: refactor this field @MuXiu1997
}

func NewBridge(
	allowedPorts set.Set[uint16],
	listenerFactory *listenerfactory.ListenerFactory,
	bindAddrPort netip.AddrPort,
	bindType string,
	p2pAddrPort netip.AddrPort,
	ipVerify bool,
	runningServices cmap.ConcurrentMap[int, *proxy.Service],
	disconnectTimeout int,
) *Bridge {
	return &Bridge{
		allowedPorts:    allowedPorts,
		listenerFactory: listenerFactory,
		BindAddrPort:    bindAddrPort,
		BindType:        bindType,
		P2PAddrPort:     p2pAddrPort,
		Clients: cmap.NewWithCustomShardingFunction[int, *Client](func(key int) uint32 {
			return uint32(key)
		}),
		OpenTask:          make(chan *db.Tunnel),
		CloseTask:         make(chan *db.Tunnel),
		CloseClient:       make(chan int),
		SecretChan:        make(chan *conn.Secret),
		ipVerify:          ipVerify,
		RunningServices:   runningServices,
		disconnectTimeout: disconnectTimeout,
		version:           "TODO", // TODO: refactor this field @MuXiu1997
	}
}

func (b *Bridge) StartTunnel() error {
	go b.checkClients()
	if b.BindType == "kcp" {
		slog.Info(fmt.Sprintf("server start, the bridge type is %s, the bridge port is %d", b.BindType, b.BindAddrPort.Port()))
		return conn.NewKcpListenerAndProcess(b.BindAddrPort.String(), func(c net.Conn) {
			b.clientProcess(conn.NewConn(c))
		})
	}

	listener, err := b.listenerFactory.GetBridgeTcpListener()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(0)
		return err
	}
	conn.Accept(listener, func(c net.Conn) {
		b.clientProcess(conn.NewConn(c))
	})

	return nil
}

func (b *Bridge) GetHealthFromClient(id int, c *conn.Conn) {
	for {
		if info, status, err := c.GetHealthInfo(); err != nil {
			break
		} else if !status { //the status is true , return target to the targetArr
			db.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
				// TODO: extract to a method of Tunnel @MuXiu1997
				v := value.(*db.Tunnel)
				if v.Client.Id == id && v.Mode == "tcp" && strings.Contains(v.Target.TargetStr, info) {
					v.Lock()
					if v.Target.TargetArr == nil || (len(v.Target.TargetArr) == 0 && len(v.HealthRemoveArr) == 0) {
						v.Target.TargetArr = utils.TrimArr(strings.Split(v.Target.TargetStr, "\n"))
					}
					v.Target.TargetArr = utils.RemoveArrVal(v.Target.TargetArr, info)
					if v.HealthRemoveArr == nil {
						v.HealthRemoveArr = make([]string, 0)
					}
					v.HealthRemoveArr = append(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
			db.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
				// TODO: extract to a method of Host @MuXiu1997
				v := value.(*db.Host)
				if v.Client.Id == id && strings.Contains(v.Target.TargetStr, info) {
					v.Lock()
					if v.Target.TargetArr == nil || (len(v.Target.TargetArr) == 0 && len(v.HealthRemoveArr) == 0) {
						v.Target.TargetArr = utils.TrimArr(strings.Split(v.Target.TargetStr, "\n"))
					}
					v.Target.TargetArr = utils.RemoveArrVal(v.Target.TargetArr, info)
					if v.HealthRemoveArr == nil {
						v.HealthRemoveArr = make([]string, 0)
					}
					v.HealthRemoveArr = append(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
		} else { //the status is false,remove target from the targetArr
			db.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
				v := value.(*db.Tunnel)
				if v.Client.Id == id && v.Mode == "tcp" && utils.IsArrContains(v.HealthRemoveArr, info) && !utils.IsArrContains(v.Target.TargetArr, info) {
					v.Lock()
					v.Target.TargetArr = append(v.Target.TargetArr, info)
					v.HealthRemoveArr = utils.RemoveArrVal(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})

			db.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
				v := value.(*db.Host)
				if v.Client.Id == id && utils.IsArrContains(v.HealthRemoveArr, info) && !utils.IsArrContains(v.Target.TargetArr, info) {
					v.Lock()
					v.Target.TargetArr = append(v.Target.TargetArr, info)
					v.HealthRemoveArr = utils.RemoveArrVal(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
		}
	}
	b.DelClient(id)
}

func (b *Bridge) verifyError(c *conn.Conn) {
	_, _ = c.Write([]byte(constant.VERIFY_EER))
}

func (b *Bridge) verifySuccess(c *conn.Conn) {
	_, _ = c.Write([]byte(constant.VERIFY_SUCCESS))
}

// clientProcess handles the process of client connection.
// It performs several checks including test flag, version check, and client verification.
// If all checks pass, it delegates the handling based on the received flag.
func (b *Bridge) clientProcess(c *conn.Conn) {
	// read test flag
	if _, err := c.GetShortContent(3); err != nil {
		slog.Info(fmt.Sprintf("The client %s connect error", c.Conn.RemoteAddr()))
		return
	}
	// version check
	if b_, err := c.GetShortLenContent(); err != nil || string(b_) != b.version {
		slog.Info(fmt.Sprintf("The client %s version does not match", c.Conn.RemoteAddr()))
		_ = c.Close()
		return
	}
	// version get
	var vs []byte
	var err error
	if vs, err = c.GetShortLenContent(); err != nil {
		slog.Info(fmt.Sprintf("get client %s version error", err.Error()))
		_ = c.Close()
		return
	}
	// write server version to client
	_, _ = c.Write([]byte(cpt.Md5(b.version)))
	c.SetReadDeadlineBySecond(5)
	var buf []byte
	// get vKey from client
	if buf, err = c.GetShortContent(32); err != nil {
		_ = c.Close()
		return
	}
	// verify
	id, err := db.GetDb().GetIdByVerifyKey(string(buf), c.Conn.RemoteAddr().String())
	if err != nil {
		slog.Info(fmt.Sprintf("Current client connection validation error, close this client: %s", c.Conn.RemoteAddr()))
		b.verifyError(c)
		return
	} else {
		b.verifySuccess(c)
	}
	if flag, err := c.ReadFlag(); err == nil {
		b.handleClientWorkType(flag, c, id, string(vs))
	} else {
		slog.Warn(fmt.Sprintf("%s, %s", err, flag))
	}
	return
}

// DelClient removes a client from the bridge.
func (b *Bridge) DelClient(id int) {
	if client, ok := b.Clients.Get(id); ok {
		_ = client.Close()
		b.Clients.Remove(id)
		if db.GetDb().IsPubClient(id) {
			return
		}
		if c, err := db.GetDb().GetClient(id); err == nil {
			b.CloseClient <- c.Id
		}
	}
}

func (b *Bridge) handleClientWorkType(workType string, c *conn.Conn, id int, vs string) {
	switch workType {
	case constant.WORK_MAIN:
		b.handleWorkMain(c, id, vs)
	case constant.WORK_CHAN:
		b.handleWorkChan(c, id, vs)
	case constant.WORK_CONFIG:
		b.handleWorkConfig(c, id)
	case constant.WORK_REGISTER:
		go b.handleWorkRegister(c)
	case constant.WORK_SECRET:
		b.handleWorkSecret(c)
	case constant.WORK_FILE:
		b.handleWorkFile(c, id, vs)
	case constant.WORK_P2P:
		b.handleWorkP2P(c)
	}
	c.SetAlive(b.BindType)
	return
}

func (b *Bridge) upsertClient(id int, newClient *Client, updateExistFunc func(*Client)) {
	b.Clients.Upsert(
		id,
		newClient,
		func(exist bool, c *Client, nc *Client) *Client {
			if exist {
				if updateExistFunc != nil {
					updateExistFunc(c)
				}
				return c
			}
			return nc
		},
	)
}

func (b *Bridge) SendLinkInfo(clientId int, link *conn.Link, t *db.Tunnel) (target net.Conn, err error) {
	//if the proxy type is local
	if link.LocalProxy {
		target, err = net.Dial("tcp", link.Host)
		return
	}
	if client, ok := b.Clients.Get(clientId); ok {
		//If ip is restricted to do ip verification
		if b.ipVerify {
			ip := utils.GetIpByAddr(link.RemoteAddr)
			if v, ok := b.Register.Get(ip); !ok {
				return nil, errors.New(fmt.Sprintf("The ip %s is not in the validation list", ip))
			} else {
				if !v.After(time.Now()) {
					return nil, errors.New(fmt.Sprintf("The validity of the ip %s has expired", ip))
				}
			}
		}
		var tunnel *mux.Mux
		if t != nil && t.Mode == "file" {
			tunnel = client.file
		} else {
			tunnel = client.tunnel
		}
		if tunnel == nil {
			err = errors.New("the client connect error")
			return
		}
		if target, err = tunnel.NewConn(); err != nil {
			return
		}
		if t != nil && t.Mode == "file" {
			//TODO if t.mode is file ,not use crypt or compress
			link.Crypt = false
			link.Compress = false
			return
		}
		if _, err = conn.NewConn(target).SendInfo(link, ""); err != nil {
			slog.Info(fmt.Sprintf("new connect error ,the target %s refuse to connect", link.Host))
			return
		}
	} else {
		err = errors.New(fmt.Sprintf("the client %d is not connect", clientId))
	}
	return
}

func (b *Bridge) getConfig(c *conn.Conn, client *db.Client) {
	var fail bool
loop:
	for {
		flag, err := c.ReadFlag()
		if err != nil {
			break
		}
		switch flag {
		case constant.WORK_STATUS:
			fail = b.handleWorkStatus(c)
		case constant.NEW_CONF:
			client, fail = b.handleNewConf(c)
		case constant.NEW_HOST:
			fail = b.handleNewHost(c, client)
		case constant.NEW_TASK:
			fail = b.handleNewTask(c, client)
		}
		if fail {
			break loop
		}
	}
	if fail && client != nil {
		b.DelClient(client.Id)
	}
	_ = c.Close()
}

// checkClients periodically checks the status of the clients.
// It removes any client that has been disconnected or has not responded to the ping.
func (b *Bridge) checkClients() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			disconnectedClients := make([]int, 0)

			b.Clients.IterCb(func(id int, client *Client) {
				if !client.check() {
					disconnectedClients = append(disconnectedClients, id)
				}
			})

			for _, id := range disconnectedClients {
				slog.Info("client closed", "id", id)
			}
		}
	}
}
