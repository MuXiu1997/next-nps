package bridge

import (
	"encoding/binary"
	"fmt"
	"github.com/MuXiu1997/next-nps/pkg/common/constant"
	"github.com/MuXiu1997/next-nps/pkg/common/utils"
	"github.com/MuXiu1997/next-nps/pkg/conn"
	"github.com/MuXiu1997/next-nps/pkg/db"
	"log/slog"
	"strconv"
)

func (b *Bridge) handleWorkStatus(c *conn.Conn) bool {
	b_, err := c.GetShortContent(32)
	if err != nil {
		return true
	}
	var str string
	id, err := db.GetDb().GetClientIdByVkey(string(b_))
	if err != nil {
		return true
	}
	db.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*db.Host)
		if v.Client.Id == id {
			str += v.Remark + constant.CONN_DATA_SEQ
		}
		return true
	})
	db.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*db.Tunnel)
		if _, ok := b.RunningServices.Get(v.Id); ok && v.Client.Id == id {
			str += v.Remark + constant.CONN_DATA_SEQ
		}
		return true
	})
	_ = binary.Write(c, binary.LittleEndian, int32(len([]byte(str))))
	_ = binary.Write(c, binary.LittleEndian, []byte(str))
	return false
}

func (b *Bridge) handleNewConf(c *conn.Conn) (*db.Client, bool) {
	client, err := c.GetConfigInfo()
	if err != nil {
		_ = c.WriteAddFail()
		return client, true
	}
	err = db.GetDb().NewClient(client)
	if err != nil {
		_ = c.WriteAddFail()
		return client, true
	}
	_ = c.WriteAddOk()
	_, _ = c.Write([]byte(client.VerifyKey))
	b.Clients.Set(client.Id, NewClient(nil, nil, nil, ""))
	return client, false
}

func (b *Bridge) handleNewHost(c *conn.Conn, client *db.Client) bool {
	h, err := c.GetHostInfo()
	if err != nil {
		_ = c.WriteAddFail()
		return true
	}
	h.Client = client
	if h.Location == "" {
		h.Location = "/"
	}
	if !client.HasHost(h) {
		if db.GetDb().IsHostExist(h) {
			_ = c.WriteAddFail()
			return true
		}
		_ = db.GetDb().NewHost(h)
		_ = c.WriteAddOk()
	} else {
		_ = c.WriteAddOk()
	}
	return false
}

func (b *Bridge) handleNewTask(c *conn.Conn, client *db.Client) bool {
	t, err := c.GetTaskInfo()
	if err != nil {
		_ = c.WriteAddFail()
		return true
	}
	// TODO: refactor this GetPorts @MuXiu1997
	ports := utils.GetPorts(t.Ports)
	targets := utils.GetPorts(t.Target.TargetStr)
	if 1 < len(ports) && (t.Mode == "tcp" || t.Mode == "udp") && (len(ports) != len(targets)) {
		_ = c.WriteAddFail()
		return true
	} else if t.Mode == "secret" || t.Mode == "p2p" {
		ports = append(ports, 0)
	}
	if len(ports) == 0 {
		_ = c.WriteAddFail()
		return true
	}
	for i := 0; i < len(ports); i++ {
		tl := new(db.Tunnel)
		tl.Mode = t.Mode
		tl.Port = ports[i]
		tl.ServerIp = t.ServerIp
		if len(ports) == 1 {
			tl.Target = t.Target
			tl.Remark = t.Remark
		} else {
			tl.Remark = t.Remark + "_" + strconv.Itoa(tl.Port)
			tl.Target = new(db.Target)
			if t.TargetAddr != "" {
				tl.Target.TargetStr = t.TargetAddr + ":" + strconv.Itoa(targets[i])
			} else {
				tl.Target.TargetStr = strconv.Itoa(targets[i])
			}
		}
		tl.Id = int(db.GetDb().JsonDb.GetTaskId())
		tl.Status = true
		tl.Flow = new(db.Flow)
		tl.NoStore = true
		tl.Client = client
		tl.Password = t.Password
		tl.LocalPath = t.LocalPath
		tl.StripPre = t.StripPre
		tl.MultiAccount = t.MultiAccount
		if !client.HasTunnel(tl) {
			if err := db.GetDb().NewTask(tl); err != nil {
				slog.Warn(fmt.Sprintf("Add task error ,%s", err.Error()))
				_ = c.WriteAddFail()
				return true
			}
			if b_ := utils.IsServerPortAvailable(tl.Mode, uint16(tl.Port), b.allowedPorts); !b_ && t.Mode != "secret" && t.Mode != "p2p" {
				_ = c.WriteAddFail()
				return true
			}
			b.OpenTask <- tl
		}
		_ = c.WriteAddOk()
	}
	return false
}
