package mux

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/xtaci/smux"
)

var DefaultMuxConfig = smux.Config{
	Version:           1,
	KeepAliveInterval: 10 * time.Second,
	KeepAliveTimeout:  30 * time.Second,
	MaxFrameSize:      32768,
	MaxReceiveBuffer:  4194304,
	MaxStreamBuffer:   65536,
	KeepAliveDisabled: true,
}

const (
	AtypIPv4       = 0x01
	AtypIPv6       = 0x04
	AtypDomainName = 0x03
)

const (
	Version0 = 0
	Version1 = 1
)

func ReadByte(reader io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(reader, b[:])
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

type Request struct {
	Version  byte
	Protocol byte
}

func ReadProtoRequest(reader io.Reader) (*Request, error) {
	version, err := ReadByte(reader)
	if err != nil {
		return nil, err
	}
	if version < Version0 || version > Version1 {
		return nil, fmt.Errorf("unsupported version: %v", version)
	}
	protocol, err := ReadByte(reader)
	if err != nil {
		return nil, err
	}
	return &Request{Version: version, Protocol: protocol}, nil
}

func EncodeProtoRequest(request Request, payload []byte) []byte {
	buf := bytes.Buffer{}
	buf.WriteByte(request.Version)
	buf.WriteByte(request.Protocol)
	buf.Write(payload)
	return buf.Bytes()
}

func newServerSession(conn net.Conn, protocol byte) (*smux.Session, error) {
	switch protocol {
	case 0:
		client, err := smux.Server(conn, &DefaultMuxConfig)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, fmt.Errorf("unexpected protocol %v", protocol)
	}
}

func newClientSession(conn net.Conn, protocol byte) (*smux.Session, error) {
	switch protocol {
	case 0:
		client, err := smux.Client(conn, &DefaultMuxConfig)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, fmt.Errorf("unexpected protocol %v", protocol)
	}
}

const (
	flagUDP       = 1
	flagAddr      = 2
	statusSuccess = 0
	statusError   = 1
)

func EncodeStreamRequest(request StreamRequest) []byte {
	buffer := bytes.Buffer{}
	destination := request.Destination
	var flags uint16
	if request.Network == "udp" {
		flags |= flagUDP
	}
	binary.Write(&buffer, binary.BigEndian, flags)

	addr, port, _ := net.SplitHostPort(destination)
	v, _ := strconv.Atoi(port)
	writeAddrPort(&buffer, AtypDomainName, addr, uint16(v))
	return buffer.Bytes()
}

type StreamRequest struct {
	Network     string
	Destination string
}

func writeAddrPort(buf *bytes.Buffer, addrType byte, addr string, port uint16) {
	buf.WriteByte(addrType)
	switch addrType {
	case AtypIPv4:
		buf.WriteString(addr)
		binary.Write(buf, binary.BigEndian, port)
	case AtypDomainName:
		buf.WriteByte(byte(len(addr)))
		buf.WriteString(addr)
		binary.Write(buf, binary.BigEndian, port)
	}
}

func readAddrPort(conn io.Reader) (addr string, err error) {
	// addrType(1)
	buf := make([]byte, 1)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return addr, err
	}

	switch buf[0] {
	case AtypIPv4:
		buf = make([]byte, 4+2)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return addr, err
		}
		addr = net.JoinHostPort(net.IP(buf[0:4]).String(), fmt.Sprintf("%d", binary.BigEndian.Uint16(buf[4:6])))
	case AtypIPv6:
		buf = make([]byte, 16+2)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return addr, err
		}
		addr = net.JoinHostPort(net.IP(buf[0:16]).String(), fmt.Sprintf("%d", binary.BigEndian.Uint16(buf[16:18])))
	case AtypDomainName:
		buf = make([]byte, 1)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return addr, err
		}
		buf = make([]byte, int(buf[0])+2)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return addr, err
		}
		addr = net.JoinHostPort(string(buf[:len(buf)-2]), fmt.Sprintf("%d", binary.BigEndian.Uint16(buf[len(buf)-2:])))
	}

	return addr, nil
}

func ReadStreamRequest(reader io.Reader) (*StreamRequest, error) {
	var flags uint16
	err := binary.Read(reader, binary.BigEndian, &flags)
	if err != nil {
		return nil, err
	}
	destination, err := readAddrPort(reader)
	if err != nil {
		return nil, err
	}
	var network string
	if flags&flagUDP == 0 {
		network = "tcp"
	} else {
		network = "udp"
	}
	return &StreamRequest{network, destination}, nil
}

type StreamResponse struct {
	Status  uint8
	Message string
}

func ReadStreamResponse(reader io.Reader) (*StreamResponse, error) {
	var response StreamResponse
	status, err := ReadByte(reader)
	if err != nil {
		return nil, err
	}
	response.Status = status
	return &response, nil
}

func IsSpecialFqdn(fqdn string) bool {
	switch fqdn {
	case "sp.mux.sing-box.arpa", // mux
		"v1.mux.cool": // mux
		return true
	default:
		return false
	}
}
