package bridge

import (
	mux "ehang.io/nps-mux"
	"errors"
	"github.com/MuXiu1997/next-nps/pkg/conn"
	"io"
)

var _ io.Closer = (*Client)(nil)

type Client struct {
	tunnel  *mux.Mux
	file    *mux.Mux
	signal  *conn.Conn
	Version string
	// This value is incremented by 1 each time a ping fails.
	// If it reaches 3, the client will be closed.
	retries int
}

func NewClient(tunnel, file *mux.Mux, conn *conn.Conn, version string) *Client {
	return &Client{
		signal:  conn,
		tunnel:  tunnel,
		file:    file,
		Version: version,
	}
}

func (c *Client) check() bool {
	if c.tunnel == nil || c.signal == nil {
		c.retries += 1
		if 3 <= c.retries {
			return false
		}
		return true
	}
	if c.tunnel.IsClose {
		return false
	}
	return true
}

func (c *Client) Close() error {
	var err1, err2, err3 error
	if c.tunnel != nil {
		err1 = c.tunnel.Close()
	}
	if c.signal != nil {
		err2 = c.signal.Close()
	}
	if c.file != nil {
		err3 = c.file.Close()
	}
	return errors.Join(err1, err2, err3)
}
