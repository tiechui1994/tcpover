package vless

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/tiechui1994/tool/log"
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

func ReadConnFirstPacket(conn net.Conn) (id string, command int, addr string, err error) {
	// version(1) id(16) addon(1) command(1) port(2) addrType(1)
	buf := make([]byte, 1+16+1+1)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return id, command, addr, err
	}

	id = string(buf[1:17])
	command = int(buf[18])

	switch byte(command) {
	case CommandTCP, CommandUDP:
		buf = make([]byte, 2+1)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return id, command, addr, err
		}

		port := binary.BigEndian.Uint16(buf[0:2])
		switch buf[len(buf)-1] {
		case AtypIPv4:
			buf = make([]byte, 4)
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				return id, command, addr, err
			}
			addr = net.JoinHostPort(net.IP(buf[0:4]).String(), fmt.Sprintf("%d", binary.BigEndian.Uint16(buf[4:6])))
		case AtypIPv6:
			buf = make([]byte, 16)
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				return id, command, addr, err
			}
			addr = net.JoinHostPort(net.IP(buf[0:16]).String(), fmt.Sprintf("%d", binary.BigEndian.Uint16(buf[16:18])))
		case AtypDomainName:
			buf = make([]byte, 1)
			_, err = io.ReadFull(conn, buf) // 1 => 23
			if err != nil {
				return id, command, addr, err
			}
			buf = make([]byte, int(buf[0]))
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				return id, command, addr, err
			}
			addr = net.JoinHostPort(string(buf), fmt.Sprintf("%v", port))
		}

		log.Debugln("read addr: %v", addr)

		// reply vless response
		buf = make([]byte, 1+1)
		_, err = conn.Write(buf)
		if err != nil {
			return id, command, addr, err
		}
	}

	return id, command, addr, nil
}
