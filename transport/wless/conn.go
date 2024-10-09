package wless

import (
	"bytes"
	"net"
)

type Conn struct {
	net.Conn
	addr     string
	received bool
}

func (vc *Conn) Read(b []byte) (int, error) {
	return vc.Conn.Read(b)
}

func (vc *Conn) sendRequest() error {
	buf := &bytes.Buffer{}

	buf.WriteByte(byte(len(vc.addr)))
	buf.Write([]byte(vc.addr))

	_, err := vc.Conn.Write(buf.Bytes())
	return err
}

// newConn return a Conn instance
func newConn(conn net.Conn, dst string) (*Conn, error) {
	c := &Conn{
		Conn: conn,
		addr: dst,
	}

	if err := c.sendRequest(); err != nil {
		return nil, err
	}
	return c, nil
}
