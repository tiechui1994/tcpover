package vless

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"github.com/tiechui1994/tcpover/transport/socks5"
)

type Conn struct {
	net.Conn
	dst      *DstAddr
	id       *UUID
	received bool
}

func (vc *Conn) Read(b []byte) (int, error) {
	if vc.received {
		return vc.Conn.Read(b)
	}

	if err := vc.recvResponse(); err != nil {
		return 0, err
	}
	vc.received = true
	return vc.Conn.Read(b)
}

func (vc *Conn) sendRequest() error {
	buf := &bytes.Buffer{}

	buf.WriteByte(Version)   // protocol version
	buf.Write(vc.id.Bytes()) // 16 bytes of uuid

	buf.WriteByte(0) // addon data length. 0 means no addon data

	// command
	if vc.dst.UDP {
		buf.WriteByte(CommandUDP)
	} else {
		buf.WriteByte(CommandTCP)
	}

	// Port AddrType Addr
	binary.Write(buf, binary.BigEndian, uint16(vc.dst.Port)) // 2
	buf.WriteByte(vc.dst.AddrType)                           // 1
	buf.Write(vc.dst.Addr)

	_, err := vc.Conn.Write(buf.Bytes())
	return err
}

func (vc *Conn) recvResponse() error {
	var err error
	buf := make([]byte, 1)
	_, err = io.ReadFull(vc.Conn, buf)
	if err != nil {
		return err
	}

	if buf[0] != Version {
		return errors.New("unexpected response version")
	}

	_, err = io.ReadFull(vc.Conn, buf)
	if err != nil {
		return err
	}

	length := int64(buf[0])
	if length != 0 { // addon data length > 0
		io.CopyN(io.Discard, vc.Conn, length) // just discard
	}

	return nil
}

// newConn return a Conn instance
func newConn(conn net.Conn, client *Client, dst *DstAddr) (*Conn, error) {
	c := &Conn{
		Conn: conn,
		id:   client.uuid,
		dst:  dst,
	}

	if err := c.sendRequest(); err != nil {
		return nil, err
	}
	return c, nil
}

func ReadAddr(conn net.Conn) (socks5.Addr, error) {
	var err error
	// version(1) id(16) addon(1) command(1)
	buf := make([]byte, 1+16+1+1)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}

	// port(2) addrType(1)
	buf = make([]byte, 2+1)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}

	var addr []byte
	port := buf[0:2]
	switch buf[2] {
	case AtypDomainName:
		var domainLength byte
		domainLength, err = socks5.ReadByte(conn) // read 2nd byte for domain length
		if err != nil {
			return nil, err
		}
		b := make([]byte, 1+1+uint16(domainLength)+2)
		b[0] = socks5.AtypDomainName
		b[1] = domainLength
		_, err = io.ReadFull(conn, b[2:2+domainLength])
		if err != nil {
			return nil, err
		}
		copy(b[2+domainLength:], port)
		addr = b
	case AtypIPv4:
		var b [1 + net.IPv4len + 2]byte
		b[0] = socks5.AtypIPv4
		_, err = io.ReadFull(conn, b[1:1+net.IPv4len])
		if err != nil {
			return nil, err
		}
		copy(b[1+net.IPv4len:], port)
		addr = b[:]
	case AtypIPv6:
		var b [1 + net.IPv6len + 2]byte
		b[0] = socks5.AtypIPv6
		_, err = io.ReadFull(conn, b[1:1+net.IPv6len])
		copy(b[1+net.IPv6len:], port)
		if err != nil {
			return nil, err
		}
		copy(b[1+net.IPv6len:], port)
		addr = b[:]
	default:
		return nil, socks5.ErrAddressNotSupported
	}

	// reply vless response
	buf = make([]byte, 1+1)
	_, err = conn.Write(buf)
	if err != nil {
		return nil, err
	}

	return addr, nil
}
