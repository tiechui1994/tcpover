package wless

import (
	"bytes"
	"io"
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


func ReadConnFirstPacket(conn net.Conn) (addr string, err error) {
	buf := make([]byte, 1)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return addr, err
	}

	buf = make([]byte, int(buf[0]))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return addr, err
	}

	addr = string(buf)
	return  addr, nil
}
