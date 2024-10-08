package ctx

import (
	"net"
)

type ConnContext interface {
	Metadata() *Metadata
	Conn() net.Conn
}

const (
	HTTP Type = iota
	HTTPCONNECT
	SOCKS5
)

type Type int

func (t Type) String() string {
	switch t {
	case HTTP:
		return "HTTP"
	case HTTPCONNECT:
		return "HTTP Connect"
	case SOCKS5:
		return "Socks5"
	default:
		return "Unknown"
	}
}

type connContext struct {
	metadata *Metadata
	conn     net.Conn
}

func (c *connContext) Metadata() *Metadata {
	return c.metadata
}

func (c *connContext) Conn() net.Conn {
	return c.conn
}

func NewConnContext(conn net.Conn, metadata *Metadata) ConnContext {
	return &connContext{
		metadata: metadata,
		conn:     conn,
	}
}
