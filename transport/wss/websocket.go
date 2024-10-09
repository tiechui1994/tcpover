package wss

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

const (
	ModeDirect     Mode = "direct"
	ModeForward    Mode = "forward"
	ModeDirectMux  Mode = "directMux"
	ModeForwardMux Mode = "forwardMux"
)

const (
	RoleManager   = "manager"
	RoleAgent     = "Agent"
	RoleConnector = "Connector"
)

const (
	SocketBufferLength = 16384
)

type Mode string

func (m Mode) IsDirect() bool {
	return m == ModeDirect || m == ModeDirectMux
}

func (m Mode) IsForward() bool {
	return m == ModeForward || m == ModeForwardMux
}

func (m Mode) IsMux() bool {
	return m == ModeDirectMux || m == ModeForwardMux
}

type ConnectParam struct {
	Name   string
	Role   string
	Code   string
	Mode   Mode
	Header http.Header
}

var (
	dialer = &websocket.Dialer{
		Proxy: http.ProxyFromEnvironment,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			v := addr
			log.Printf("DialContext [%v]: %v", addr, v)
			return (&net.Dialer{}).DialContext(context.Background(), network, v)
		},
		HandshakeTimeout: 45 * time.Second,
		WriteBufferSize:  SocketBufferLength,
		ReadBufferSize:   SocketBufferLength,
	}
)

func WebSocketConnect(ctx context.Context, server string, param *ConnectParam) (net.Conn, error) {
	conn, err := RawWebSocketConnect(ctx, server, param)
	if err != nil {
		return nil, err
	}

	return &websocketConn{
		conn:       conn,
		remoteAddr: conn.RemoteAddr(),
	}, nil
}

func RawWebSocketConnect(ctx context.Context, server string, param *ConnectParam) (*websocket.Conn, error) {
	if param.Header == nil {
		param.Header = http.Header{}
	}

	query := url.Values{}
	query.Set("name", param.Name)
	query.Set("rule", param.Role)
	query.Set("code", param.Code)
	query.Set("mode", string(param.Mode))
	u := server + "?" + query.Encode()
	conn, resp, err := dialer.DialContext(ctx, u, param.Header)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		buf := bytes.NewBuffer(nil)
		_ = resp.Write(buf)
		return nil, fmt.Errorf("statusCode != 101:\n%s", buf.String())
	}

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			err = conn.WriteControl(websocket.PingMessage, []byte(nil), time.Now().Add(time.Second))
			if IsClose(err) {
				return
			}
			if err != nil {
				log.Printf("Ping: %v", err)
			}
		}
	}()

	return conn, err
}
