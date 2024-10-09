package wless

import "net"

// Client is wless connection generator
type Client struct {
}

// StreamConn return a Conn with net.Conn and DstAddr
func (c *Client) StreamConn(conn net.Conn, dst string) (net.Conn, error) {
	return newConn(conn, dst)
}

// NewClient return Client instance
func NewClient() *Client{
	return &Client{}
}
