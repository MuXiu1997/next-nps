package conn

import (
	"errors"
	"io"

	"github.com/golang/snappy"
)

var _ io.ReadWriteCloser = (*SnappyConn)(nil)

type SnappyConn struct {
	w *snappy.Writer
	r *snappy.Reader
	c io.Closer
}

func NewSnappyConn(conn io.ReadWriteCloser) *SnappyConn {
	c := new(SnappyConn)
	c.w = snappy.NewBufferedWriter(conn)
	c.r = snappy.NewReader(conn)
	c.c = conn.(io.Closer)
	return c
}

func (s *SnappyConn) Write(b []byte) (n int, err error) {
	if n, err = s.w.Write(b); err != nil {
		return
	}
	if err = s.w.Flush(); err != nil {
		return
	}
	return
}

func (s *SnappyConn) Read(b []byte) (n int, err error) {
	return s.r.Read(b)
}

func (s *SnappyConn) Close() error {
	err1 := s.w.Close()
	err2 := s.c.Close()
	return errors.Join(err1, err2)
}
