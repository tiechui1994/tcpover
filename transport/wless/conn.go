package wless

import (
	"bytes"
	"io"
	"net"
	"strconv"

	"github.com/tiechui1994/tcpover/transport/socks5"
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

func ReadAddr(conn net.Conn) (addr socks5.Addr, err error) {
	// length(1) + N
	var length byte
	length, err = socks5.ReadByte(conn)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, int(length))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	host, p, err := net.SplitHostPort(string(buf))
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return nil, err
	}

	b := make([]byte, 1+1+len(host)+2)
	b[0] = socks5.AtypDomainName
	b[1] = byte(len(host))
	copy(b[2:2+len(host)], host)
	b[2+len(host)] = uint8(port >> 8)
	b[2+len(host)+1] = uint8(port)

	return b, nil
}
