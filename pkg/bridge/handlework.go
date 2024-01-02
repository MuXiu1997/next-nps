package bridge

import (
	mux "ehang.io/nps-mux"
	"encoding/binary"
	"fmt"
	"github.com/MuXiu1997/next-nps/pkg/common/constant"
	"github.com/MuXiu1997/next-nps/pkg/common/utils"
	"github.com/MuXiu1997/next-nps/pkg/conn"
	"github.com/MuXiu1997/next-nps/pkg/db"
	"log/slog"
	"net"
	"time"
)

func (b *Bridge) handleWorkMain(c *conn.Conn, id int, vs string) {
	isPub := db.GetDb().IsPubClient(id)
	if isPub {
		_ = c.Close()
		return
	}
	tcpConn, ok := c.Conn.(*net.TCPConn)
	if ok {
		// add tcp keep alive option for signal connection
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(5 * time.Second)
	}
	b.upsertClient(id, NewClient(nil, nil, c, vs), func(client *Client) {
		if client.signal != nil {
			_, _ = client.signal.WriteClose()
		}
		client.signal = c
		client.Version = vs
	})
	go b.GetHealthFromClient(id, c)
	slog.Info(fmt.Sprintf("clientId %d connection succeeded, address:%s ", id, c.Conn.RemoteAddr()))
}

func (b *Bridge) handleWorkChan(c *conn.Conn, id int, vs string) {
	muxConn := mux.NewMux(c.Conn, b.BindType, b.disconnectTimeout)
	b.upsertClient(id, NewClient(muxConn, nil, nil, vs), func(client *Client) {
		client.tunnel = muxConn
	})
}

func (b *Bridge) handleWorkConfig(c *conn.Conn, id int) {
	// TODO: refactor this method @MuXiu1997
	isPub := db.GetDb().IsPubClient(id)
	client, err := db.GetDb().GetClient(id)
	if err != nil || (!isPub && !client.ConfigConnAllow) {
		_ = c.Close()
		return
	}
	_ = binary.Write(c, binary.LittleEndian, isPub)
	go b.getConfig(c, client)
}

func (b *Bridge) handleWorkRegister(c *conn.Conn) {
	var hour int32
	if err := binary.Read(c, binary.LittleEndian, &hour); err == nil {
		b.Register.Set(utils.GetIpByAddr(c.Conn.RemoteAddr().String()), time.Now().Add(time.Hour*time.Duration(hour)))
	}
}

func (b *Bridge) handleWorkSecret(c *conn.Conn) {
	if b_, err := c.GetShortContent(32); err == nil {
		b.SecretChan <- conn.NewSecret(string(b_), c)
	} else {
		slog.Error("secret error, failed to match the key successfully")
	}
}

func (b *Bridge) handleWorkFile(c *conn.Conn, id int, vs string) {
	muxConn := mux.NewMux(c.Conn, b.BindType, b.disconnectTimeout)
	b.upsertClient(id, NewClient(nil, muxConn, nil, vs), func(client *Client) {
		client.file = muxConn
	})
}

func (b *Bridge) handleWorkP2P(c *conn.Conn) {
	// read md5 secret
	if b_, err := c.GetShortContent(32); err != nil {
		slog.Error(fmt.Sprintf("p2p error, %s", err.Error()))
	} else if t := db.GetDb().GetTaskByMd5Password(string(b_)); t == nil {
		slog.Error("p2p error, failed to match the key successfully")
	} else {
		if client, ok := b.Clients.Get(t.Client.Id); !ok {
			return
		} else {
			_, _ = client.signal.Write([]byte(constant.NEW_UDP_CONN))
			svrAddr := b.P2PAddrPort.String()
			if err != nil {
				slog.Warn("get local udp addr error")
				return
			}
			_ = client.signal.WriteLenContent([]byte(svrAddr))
			_ = client.signal.WriteLenContent(b_)
			_ = c.WriteLenContent([]byte(svrAddr))
		}
	}
}
