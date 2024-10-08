package ctx

import (
	"fmt"
	"net"

	"github.com/tiechui1994/tcpover/transport/socks5"
)

type Metadata struct {
	NetWork string `json:"network"`
	Type    Type   `json:"type"`
	SrcIP   net.IP `json:"sourceIP"`
	DstIP   net.IP `json:"destinationIP"`
	SrcPort uint16 `json:"sourcePort"`
	DstPort uint16 `json:"destinationPort"`
	Host    string `json:"host"`
	Origin  string `json:"origin"`
}

func (m *Metadata) RemoteAddress() string {
	return net.JoinHostPort(m.String(), fmt.Sprintf("%v", m.DstPort))
}

func (m *Metadata) SourceAddress() string {
	return net.JoinHostPort(m.SrcIP.String(), fmt.Sprintf("%v", m.SrcPort))
}

func (m *Metadata) String() string {
	if m.Host != "" {
		return m.Host
	} else if m.DstIP != nil {
		return m.DstIP.String()
	} else {
		return "<nil>"
	}
}

func (m *Metadata) Valid() bool {
	return m.Host != "" || m.DstIP != nil
}

func (m *Metadata) AddrType() int {
	switch true {
	case m.Host != "" || m.DstIP == nil:
		return socks5.AtypDomainName
	case m.DstIP.To4() != nil:
		return socks5.AtypIPv4
	default:
		return socks5.AtypIPv6
	}
}
