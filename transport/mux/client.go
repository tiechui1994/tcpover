package mux

import (
	"context"
	"net"
	"sync"

	"github.com/tiechui1994/tcpover/ctx"
	"github.com/xtaci/smux"
)

type Client struct {
	pool   *sync.Map
	dialer func() (net.Conn, error)
}

func NewClient(dialer func() (net.Conn, error)) *Client {
	return &Client{
		pool:   &sync.Map{},
		dialer: dialer,
	}
}

func (c *Client) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	conn, err := c.openStream()
	if err != nil {
		return nil, err
	}

	// wrap mux Conn
	return &clientConn{Conn: conn, destination: metadata.RemoteAddress()}, nil
}

func (c *Client) openStream() (net.Conn, error) {
	conn, ok, err := openMuxConnectConn(c.pool)
	if err != nil {
		return nil, err
	}
	if ok {
		return conn, nil
	}

	// create new mux conn
	muxConn, err := c.dialer()
	if err != nil {
		return nil, err
	}

	// wrap proto conn
	protoConn := &protocolConn{
		Conn: muxConn,
		request: Request{
			Version:  0,
			Protocol: 0,
		},
	}
	session, err := newClientSession(protoConn, 0)
	if err != nil {
		protoConn.Close()
		return nil, err
	}

	c.pool.Store(session, session)
	return c.openStream()
}

func openMuxConnectConn(pool *sync.Map) (net.Conn, bool, error) {
	var mux *smux.Session
	pool.Range(func(key, value interface{}) bool {
		session := value.(*smux.Session)
		if session.IsClosed() || session.NumStreams() > 8 {
			return true
		}
		mux = session
		return false
	})

	if mux == nil {
		return nil, false, nil
	}

	conn, err := mux.OpenStream()
	if err != nil {
		if err == smux.ErrGoAway {
			return nil, false, nil
		}
		return nil, false, err
	}

	return conn, true, nil
}
