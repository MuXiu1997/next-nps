package conn

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MuXiu1997/next-nps/pkg/common"
	cpt "github.com/MuXiu1997/next-nps/pkg/crypt"
	"github.com/MuXiu1997/next-nps/pkg/db"
	"github.com/MuXiu1997/next-nps/pkg/goroutine"
	"github.com/MuXiu1997/next-nps/pkg/pmux"
	rt "github.com/MuXiu1997/next-nps/pkg/rate"
	"github.com/xtaci/kcp-go"
)

var _ io.ReadWriteCloser = (*Conn)(nil)
var _ net.Conn = (*Conn)(nil)

type Conn struct {
	Conn net.Conn
	Rb   []byte
}

func NewConn(conn net.Conn) *Conn {
	return &Conn{Conn: conn}
}

// readRequest reads data from the connection into the provided buffer until it encounters a sequence of "\r\n\r\n"
// (indicating the end of an HTTP header) or the buffer is full. It returns the number of bytes read and any error encountered.
func (c *Conn) readRequest(buf []byte) (n int, err error) {
	var rd int
	for {
		rd, err = c.Read(buf[n:])
		if err != nil {
			return
		}
		n += rd
		if n < 4 {
			continue
		}
		if string(buf[n-4:n]) == "\r\n\r\n" {
			return
		}
		// buf is full, can't contain the request
		if n == cap(buf) {
			err = io.ErrUnexpectedEOF
			return
		}
	}
}

func (c *Conn) GetHost() (method, address string, rb []byte, err error, r *http.Request) {
	var b [32 * 1024]byte
	var n int
	if n, err = c.readRequest(b[:]); err != nil {
		return
	}
	rb = b[:n]
	r, err = http.ReadRequest(bufio.NewReader(bytes.NewReader(rb)))
	if err != nil {
		return
	}
	hostPortURL, err := url.Parse(r.Host)
	if err != nil {
		address = r.Host
		err = nil
		return
	}
	if hostPortURL.Opaque == "443" {
		if strings.Index(r.Host, ":") == -1 {
			address = r.Host + ":443"
		} else {
			address = r.Host
		}
	} else {
		if strings.Index(r.Host, ":") == -1 {
			address = r.Host + ":80"
		} else {
			address = r.Host
		}
	}
	return
}

func (c *Conn) GetShortLenContent() (b []byte, err error) {
	var l int
	if l, err = c.GetLen(); err != nil {
		return
	}
	if l < 0 || 32<<10 < l {
		err = errors.New("read length error")
		return
	}
	return c.GetShortContent(l)
}

func (c *Conn) GetShortContent(l int) (b []byte, err error) {
	buf := make([]byte, l)
	return buf, binary.Read(c, binary.LittleEndian, &buf)
}

// ReadLen reads a specified number of bytes from the connection into the provided buffer.
func (c *Conn) ReadLen(cLen int, buf []byte) (int, error) {
	if cLen <= 0 || len(buf) < cLen {
		return 0, fmt.Errorf("invalid length %d", cLen)
	}
	if n, err := io.ReadFull(c, buf[:cLen]); err != nil || n != cLen {
		return n, fmt.Errorf("error reading specified length %w", err)
	}
	return cLen, nil
}

// GetLen reads a 4-byte length from the connection and returns it as an int.
func (c *Conn) GetLen() (int, error) {
	var l int32
	err := binary.Read(c, binary.LittleEndian, &l)
	return int(l), err
}

// WriteLen writes a 4-byte length to the connection.
func (c *Conn) WriteLenContent(buf []byte) (err error) {
	var b []byte
	if b, err = GetLenBytes(buf); err != nil {
		return
	}
	return binary.Write(c.Conn, binary.LittleEndian, b)
}

// ReadFlag reads a 4-byte flag from the connection and returns it as a string
func (c *Conn) ReadFlag() (string, error) {
	buf := make([]byte, 4)
	return string(buf), binary.Read(c, binary.LittleEndian, &buf)
}

func (c *Conn) SetAlive(tp string) {
	switch c.Conn.(type) {
	case *kcp.UDPSession, *net.TCPConn, *pmux.PortConn:
		_ = c.Conn.SetReadDeadline(time.Time{})
	}
}

func (c *Conn) SetReadDeadlineBySecond(t time.Duration) {
	switch c.Conn.(type) {
	case *kcp.UDPSession, *net.TCPConn, *pmux.PortConn:
		_ = c.Conn.SetReadDeadline(time.Now().Add(time.Duration(t) * time.Second))
	}
}

// get link info from conn
func (c *Conn) GetLinkInfo() (lk *Link, err error) {
	err = c.getInfo(&lk)
	return
}

// send info for link
func (c *Conn) SendHealthInfo(info, status string) (int, error) {
	raw := bytes.NewBuffer([]byte{})
	common.BinaryWrite(raw, info, status)
	return c.Write(raw.Bytes())
}

// get health info from conn
func (c *Conn) GetHealthInfo() (info string, status bool, err error) {
	var l int
	buf := common.BufPoolMax.Get().([]byte)
	defer common.PutBufPoolMax(buf)
	if l, err = c.GetLen(); err != nil {
		return
	} else if _, err = c.ReadLen(l, buf); err != nil {
		return
	} else {
		arr := strings.Split(string(buf[:l]), common.CONN_DATA_SEQ)
		if len(arr) >= 2 {
			return arr[0], common.GetBoolByStr(arr[1]), nil
		}
	}
	return "", false, errors.New("receive health info error")
}

// get task info
func (c *Conn) GetHostInfo() (h *db.Host, err error) {
	err = c.getInfo(&h)
	h.Id = int(db.GetDb().JsonDb.GetHostId())
	h.Flow = new(db.Flow)
	h.NoStore = true
	return
}

// get task info
func (c *Conn) GetConfigInfo() (client *db.Client, err error) {
	err = c.getInfo(&client)
	client.NoStore = true
	client.Status = true
	if client.Flow == nil {
		client.Flow = new(db.Flow)
	}
	client.NoDisplay = false
	return
}

// get task info
func (c *Conn) GetTaskInfo() (t *db.Tunnel, err error) {
	err = c.getInfo(&t)
	t.Id = int(db.GetDb().JsonDb.GetTaskId())
	t.NoStore = true
	t.Flow = new(db.Flow)
	return
}

// send  info
func (c *Conn) SendInfo(t interface{}, flag string) (int, error) {
	/*
		The task info is formed as follows:
		+----+-----+---------+
		|type| len | content |
		+----+---------------+
		| 4  |  4  |   ...   |
		+----+---------------+
	*/
	raw := bytes.NewBuffer([]byte{})
	if flag != "" {
		binary.Write(raw, binary.LittleEndian, []byte(flag))
	}
	b, err := json.Marshal(t)
	if err != nil {
		return 0, err
	}
	lenBytes, err := GetLenBytes(b)
	if err != nil {
		return 0, err
	}
	binary.Write(raw, binary.LittleEndian, lenBytes)
	return c.Write(raw.Bytes())
}

// get task info
func (c *Conn) getInfo(t interface{}) (err error) {
	var l int
	buf := common.BufPoolMax.Get().([]byte)
	defer common.PutBufPoolMax(buf)
	if l, err = c.GetLen(); err != nil {
		return
	} else if _, err = c.ReadLen(l, buf); err != nil {
		return
	} else {
		json.Unmarshal(buf[:l], &t)
	}
	return
}

func (c *Conn) Close() error {
	return c.Conn.Close()
}

func (c *Conn) Write(b []byte) (int, error) {
	return c.Conn.Write(b)
}

func (c *Conn) Read(b []byte) (n int, err error) {
	if c.Rb != nil {
		//if the rb is not nil ,read rb first
		if len(c.Rb) > 0 {
			n = copy(b, c.Rb)
			c.Rb = c.Rb[n:]
			return
		}
		c.Rb = nil
	}
	return c.Conn.Read(b)
}

func (c *Conn) WriteClose() (int, error) {
	return c.Write([]byte(common.RES_CLOSE))
}

func (c *Conn) WriteMain() (int, error) {
	return c.Write([]byte(common.WORK_MAIN))
}

func (c *Conn) WriteConfig() (int, error) {
	return c.Write([]byte(common.WORK_CONFIG))
}

func (c *Conn) WriteChan() (int, error) {
	return c.Write([]byte(common.WORK_CHAN))
}

// get task or host result of add
func (c *Conn) GetAddStatus() (b bool) {
	_ = binary.Read(c.Conn, binary.LittleEndian, &b)
	return
}

func (c *Conn) WriteAddOk() error {
	return binary.Write(c.Conn, binary.LittleEndian, true)
}

func (c *Conn) WriteAddFail() error {
	defer func(c *Conn) {
		_ = c.Close()
	}(c)
	return binary.Write(c.Conn, binary.LittleEndian, false)
}

func (c *Conn) LocalAddr() net.Addr {
	return c.Conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.Conn.RemoteAddr()
}

func (c *Conn) SetDeadline(t time.Time) error {
	return c.Conn.SetDeadline(t)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.Conn.SetReadDeadline(t)
}

// GetLenBytes returns a byte slice containing the length of the given byte slice in the first 4 bytes, followed by the
// given byte slice.
func GetLenBytes(buf []byte) (b []byte, err error) {
	raw := bytes.NewBuffer([]byte{})
	if err = binary.Write(raw, binary.LittleEndian, int32(len(buf))); err != nil {
		return
	}
	if err = binary.Write(raw, binary.LittleEndian, buf); err != nil {
		return
	}
	b = raw.Bytes()
	return
}

func SetupUdpSession(s *kcp.UDPSession) {
	s.SetStreamMode(true)
	s.SetWindowSize(1024, 1024)
	_ = s.SetReadBuffer(64 * 1024)
	_ = s.SetWriteBuffer(64 * 1024)
	s.SetNoDelay(1, 10, 2, 1)
	s.SetMtu(1600)
	s.SetACKNoDelay(true)
	s.SetWriteDelay(false)
}

// conn1 mux conn
func CopyWaitGroup(conn1, conn2 net.Conn, crypt bool, snappy bool, rate *rt.Rate, flow *db.Flow, isServer bool, rb []byte) {
	//var in, out int64
	//var wg sync.WaitGroup
	connHandle := GetConn(conn1, crypt, snappy, rate, isServer)
	if rb != nil {
		_, _ = connHandle.Write(rb)
	}
	//go func(in *int64) {
	//	wg.Add(1)
	//	*in, _ = common.CopyBuffer(connHandle, conn2)
	//	connHandle.Close()
	//	conn2.Close()
	//	wg.Done()
	//}(&in)
	//out, _ = common.CopyBuffer(conn2, connHandle)
	//connHandle.Close()
	//conn2.Close()
	//wg.Wait()
	//if flow != nil {
	//	flow.Add(in, out)
	//}
	wg := new(sync.WaitGroup)
	wg.Add(1)
	err := goroutine.CopyConnsPool.Invoke(goroutine.NewConns(connHandle, conn2, flow, wg))
	wg.Wait()
	if err != nil {
		slog.Error(err.Error())
	}
}

// GetConn creates a new connection based on the given parameters.
func GetConn(conn net.Conn, crypt, snappy bool, rate *rt.Rate, isServer bool) io.ReadWriteCloser {
	if crypt {
		if isServer {
			return rt.NewRateConn(cpt.NewTlsServerConn(conn), rate)
		}
		return rt.NewRateConn(cpt.NewTlsClientConn(conn), rate)
	} else if snappy {
		return rt.NewRateConn(NewSnappyConn(conn), rate)
	}
	return rt.NewRateConn(conn, rate)
}

var _ io.Writer = (*LenConn)(nil)

type LenConn struct {
	conn io.Writer
	Len  int
}

func NewLenConn(conn io.Writer) *LenConn {
	return &LenConn{conn: conn}
}

func (c *LenConn) Write(p []byte) (n int, err error) {
	n, err = c.conn.Write(p)
	c.Len += n
	return
}
