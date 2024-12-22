package wss

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type websocketConn struct {
	conn       *websocket.Conn
	reader     io.Reader
	remoteAddr net.Addr

	// https://godoc.org/github.com/gorilla/websocket#hdr-Concurrency
	rMux sync.Mutex
	wMux sync.Mutex
}

type WebsocketConfig struct {
	Host      string
	Port      string
	Path      string
	Headers   http.Header
	TLS       bool
	TLSConfig *tls.Config
}

// Read implements net.Conn.Read()
func (wsc *websocketConn) Read(b []byte) (int, error) {
	wsc.rMux.Lock()
	defer wsc.rMux.Unlock()
	for {
		reader, err := wsc.getReader()
		if err != nil {
			return 0, err
		}

		nBytes, err := reader.Read(b)
		if err == io.EOF {
			wsc.reader = nil
			continue
		}
		return nBytes, err
	}
}

// Write implements io.Writer.
func (wsc *websocketConn) Write(b []byte) (int, error) {
	wsc.wMux.Lock()
	defer wsc.wMux.Unlock()
	if err := wsc.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (wsc *websocketConn) Close() error {
	var errors []string
	if err := wsc.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second*5)); err != nil {
		errors = append(errors, err.Error())
	}
	if err := wsc.conn.Close(); err != nil {
		errors = append(errors, err.Error())
	}
	if len(errors) > 0 {
		return fmt.Errorf("failed to close connection: %s", strings.Join(errors, ","))
	}
	return nil
}

func (wsc *websocketConn) getReader() (io.Reader, error) {
	if wsc.reader != nil {
		return wsc.reader, nil
	}

	_, reader, err := wsc.conn.NextReader()
	if err != nil {
		return nil, err
	}
	wsc.reader = reader
	return reader, nil
}

func (wsc *websocketConn) LocalAddr() net.Addr {
	return wsc.conn.LocalAddr()
}

func (wsc *websocketConn) RemoteAddr() net.Addr {
	return wsc.remoteAddr
}

func (wsc *websocketConn) SetDeadline(t time.Time) error {
	if err := wsc.SetReadDeadline(t); err != nil {
		return err
	}
	return wsc.SetWriteDeadline(t)
}

func (wsc *websocketConn) SetReadDeadline(t time.Time) error {
	return wsc.conn.SetReadDeadline(t)
}

func (wsc *websocketConn) SetWriteDeadline(t time.Time) error {
	return wsc.conn.SetWriteDeadline(t)
}

func NewWebsocketConn(conn *websocket.Conn) net.Conn  {
	return &websocketConn{
		conn:       conn,
		remoteAddr: conn.RemoteAddr(),
	}
}

func StreamWebSocketConn(conn net.Conn, c *WebsocketConfig) (net.Conn, error) {
	dialer := &websocket.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return conn, nil
		},
		ReadBufferSize:   4 * 1024,
		WriteBufferSize:  4 * 1024,
		HandshakeTimeout: time.Second * 8,
	}

	scheme := "ws"
	if c.TLS {
		scheme = "wss"
		dialer.TLSClientConfig = c.TLSConfig
	}

	u, err := url.Parse(c.Path)
	if err != nil {
		return nil, fmt.Errorf("parse url %s error: %w", c.Path, err)
	}

	uri := url.URL{
		Scheme:   scheme,
		Host:     net.JoinHostPort(c.Host, c.Port),
		Path:     u.Path,
		RawQuery: u.RawQuery,
	}

	headers := http.Header{}
	if c.Headers != nil {
		for k := range c.Headers {
			headers.Add(k, c.Headers.Get(k))
		}
	}

	wsConn, resp, err := dialer.Dial(uri.String(), headers)
	if err != nil {
		reason := err.Error()
		if resp != nil {
			reason = resp.Status
		}
		return nil, fmt.Errorf("dial %s error: %s", uri.Host, reason)
	}

	return &websocketConn{
		conn:       wsConn,
		remoteAddr: conn.RemoteAddr(),
	}, nil
}

var (
	webSocketCloseCode = []int{
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseProtocolError,
		websocket.CloseUnsupportedData,
		websocket.CloseNoStatusReceived,
		websocket.CloseAbnormalClosure,
		websocket.CloseInvalidFramePayloadData,
		websocket.CloseInternalServerErr,
		websocket.CloseServiceRestart,
		websocket.CloseTryAgainLater,
	}
)

func isSyscallError(v syscall.Errno) bool {
	return v.Is(syscall.ECONNABORTED) || v.Is(syscall.ECONNRESET) ||
		v.Is(syscall.ETIMEDOUT) || v.Is(syscall.ECONNREFUSED) ||
		v.Is(syscall.ENETUNREACH) || v.Is(syscall.ENETRESET) ||
		v.Is(syscall.EPIPE)
}

func IsClose(err error) bool {
	if err == nil {
		return false
	}

	if _, ok := err.(*websocket.CloseError); ok {
		return websocket.IsCloseError(err, webSocketCloseCode...)
	}

	if v, ok := err.(*net.OpError); ok {
		if vv, ok := v.Err.(syscall.Errno); ok {
			result := isSyscallError(vv)
			if result {
				fmt.Println("net.OpError", err)
			}
			return result
		}
	}

	if v, ok := err.(syscall.Errno); ok {
		result := isSyscallError(v)
		if result {
			fmt.Println("syscall.Errno", err)
		}
		return result
	}

	if strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "broken pipe") ||
		strings.Contains(err.Error(), "connection reset by peer") {
		fmt.Println("contains broken", err)
		return true
	}

	if errors.Is(err, websocket.ErrCloseSent) {
		fmt.Println("websocket close sent", err)
		return true
	}

	return false
}

