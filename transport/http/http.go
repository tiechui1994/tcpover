// Package http implements the VLESS-over-TLS+HTTP tunnel transport. It supports
// both HTTP/1.1 (raw request/response upgrade, see ServeConn/Connect) and
// HTTP/2 (streaming via an http.Handler, see ServeH2/connectH2). The client
// entry point Connect picks the protocol based on the server URL scheme:
// "h2://" selects HTTP/2, anything else (e.g. "https://") selects HTTP/1.1.
package http

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/net/http2"

	"github.com/tiechui1994/tcpover/transport/wss"
)

const (
	// SocketBufferLength is the size of the internal buffered reader/pipe.
	SocketBufferLength = 32768
)

// ConnectParam carries the extra metadata that may be attached to the HTTP
// request (kept compatible with the wss transport for easier wiring).
type ConnectParam struct {
	Name   string
	Role   string
	Code   string
	Mode   string
	Header http.Header
}

// Connect dials server and establishes a full-duplex VLESS tunnel over TLS+HTTP.
// When the server URL uses the "h2://" scheme the connection is established
// with HTTP/2, otherwise HTTP/1.1 is used.
func Connect(ctx context.Context, server string, param *ConnectParam) (net.Conn, error) {
	if strings.HasPrefix(server, "h2://") {
		return connectH2(ctx, server, param)
	}
	return connectH1(ctx, server, param)
}

// ---------------------------------------------------------------------------
// HTTP/1.1 (non-CONNECT) transport
// ---------------------------------------------------------------------------

// Config describes the HTTP/1.1 request used to establish the tunnel.
type Config struct {
	Host    string
	Port    string
	Path    string
	Headers http.Header
	TLS     bool
}

// h1HeaderData holds the values rendered into the HTTP/1.1 header templates.
type h1HeaderData struct {
	Method  string
	Path    string
	Host    string
	Headers http.Header
}

// requestHeaderTemplate renders the client-side HTTP/1.1 request headers. The
// body is streamed separately once the request line and headers have been
// written.
var requestHeaderTemplate = template.Must(template.New("h1-request").Parse(
	"{{.Method}} {{.Path}} HTTP/1.1\r\n" +
	"Host: {{.Host}}\r\n" +
	"Accept: */*\r\n" +
	"Accept-Encoding: identity\r\n" +
	"Connection: keep-alive\r\n" +
	"{{range $key, $values := .Headers}}{{range $values}}{{$key}}: {{.}}\r\n{{end}}{{end}}" +
	"\r\n",
))

// responseHeaderTemplate renders the server-side HTTP/1.1 200 response headers
// used to acknowledge the tunnel request.
var responseHeaderTemplate = template.Must(template.New("h1-response").Parse(
	"HTTP/1.1 200 OK\r\n" +
	"Content-Type: application/octet-stream\r\n" +
	"Connection: keep-alive\r\n" +
	"\r\n",
))

// tunnelConn wraps a net.Conn so that reads come from the HTTP response/request
// body stream (upstream) while writes go straight to the underlying connection
// (downstream request body).
type tunnelConn struct {
	net.Conn
	reader io.Reader
}

func (c *tunnelConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// StreamHTTPConn sends an HTTP request over conn (which is expected to be a TLS
// connection for the https transport) and reads the 200 response. It returns a
// net.Conn that tunnels the data: writes go into the HTTP request body
// (downstream) and reads come from the HTTP response body (upstream).
//
// This is the "non-CONNECT" style tunnel: a normal HTTP request/response is used
// to establish a full-duplex stream instead of the HTTP CONNECT method or a
// WebSocket upgrade.
func StreamHTTPConn(conn net.Conn, cfg *Config) (net.Conn, error) {
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	host := cfg.Host
	if cfg.Port != "" && cfg.Port != "443" && cfg.Port != "80" {
		host = net.JoinHostPort(cfg.Host, cfg.Port)
	}

	var sb strings.Builder
	if err := requestHeaderTemplate.Execute(&sb, h1HeaderData{
		Method:  http.MethodPost,
		Path:    path,
		Host:    host,
		Headers: cfg.Headers,
	}); err != nil {
		return nil, err
	}

	if _, err := io.WriteString(conn, sb.String()); err != nil {
		return nil, err
	}

	reader := bufio.NewReaderSize(conn, SocketBufferLength)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected http status: %d %s", resp.StatusCode, resp.Status)
	}

	return &tunnelConn{
		Conn:   conn,
		reader: resp.Body,
	}, nil
}

// connectH1 dials server (https://host:port/path), performs the TLS handshake
// and the HTTP/1.1 tunnel handshake, returning a net.Conn ready to carry the
// VLESS protocol. The certificate of the remote endpoint is not verified
// strictly (matching the behavior of the wss transport).
func connectH1(ctx context.Context, server string, param *ConnectParam) (net.Conn, error) {
	if param == nil {
		param = &ConnectParam{}
	}

	u, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)

	rawConn, err := (&net.Dialer{Timeout: 45 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	tlsConfig := wss.GetGlobalFingerprintTLCConfig(&tls.Config{
		ServerName: host,
	})
	tlsConn := tls.Client(rawConn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, err
	}

	cfg := &Config{
		Host:    host,
		Port:    port,
		Path:    u.Path,
		Headers: param.Header,
		TLS:     true,
	}
	return StreamHTTPConn(tlsConn, cfg)
}

// ServeConn reads an HTTP/1.1 request from conn (a raw or TLS connection
// handled by the caller), replies with 200 and returns a net.Conn that tunnels
// the VLESS stream. Reads come from the HTTP request body (client -> server) and
// writes go into the HTTP response body (server -> client).
func ServeConn(conn net.Conn) (net.Conn, *http.Request, error) {
	reader := bufio.NewReaderSize(conn, SocketBufferLength)
	request, err := http.ReadRequest(reader)
	if err != nil {
		return nil, nil, err
	}

	var sb strings.Builder
	if err := responseHeaderTemplate.Execute(&sb, nil); err != nil {
		return nil, nil, err
	}
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		return nil, nil, err
	}

	return &tunnelConn{
		Conn:   conn,
		reader: reader,
	}, request, nil
}

// ---------------------------------------------------------------------------
// HTTP/2 transport
// ---------------------------------------------------------------------------

// addr is a minimal net.Addr backed by a precomputed string. HTTP/2 does not
// expose raw per-stream sockets, so addresses are best-effort only.
type addr struct {
	network string
	address string
}

func (a addr) Network() string { return a.network }
func (a addr) String() string  { return a.address }

// contextKey is the type used for the value stored by ConnContext.
type contextKey string

// ConnContextKey is exported so callers configuring http.Server.ConnContext can
// store the underlying connection for ServeH2 to pick up.
const ConnContextKey contextKey = "tcpover/http-net-conn"

// connContextKey is the internal alias used by ServeH2.
var connContextKey = ConnContextKey

// connectH2 dials server (h2://host:port/path or https://host:port/path),
// performs the TLS handshake with ALPN "h2" and establishes a full-duplex
// HTTP/2 stream that carries the VLESS protocol. The returned net.Conn exposes
// Read (upstream, from the response body) and Write (downstream, into the
// request body) so the caller can run VLESS on top of it.
func connectH2(ctx context.Context, server string, param *ConnectParam) (net.Conn, error) {
	if param == nil {
		param = &ConnectParam{}
	}

	u, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(host, port)

	tlsConfig := wss.GetGlobalFingerprintTLCConfig(&tls.Config{
		ServerName: host,
		NextProtos: []string{http2.NextProtoTLS},
	})

	var tlsConn net.Conn
	transport := &http2.Transport{
		TLSClientConfig: tlsConfig,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			rawConn, err := (&net.Dialer{Timeout: 45 * time.Second}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			conn := tls.Client(rawConn, cfg)
			if err := conn.HandshakeContext(ctx); err != nil {
				rawConn.Close()
				return nil, err
			}
			if state := conn.ConnectionState(); state.NegotiatedProtocol != http2.NextProtoTLS {
				rawConn.Close()
				return nil, fmt.Errorf("tls alpn negotiation failed: got %q want %q", state.NegotiatedProtocol, http2.NextProtoTLS)
			}
			tlsConn = conn
			return conn, nil
		},
	}
	client := &http.Client{Transport: transport}

	// the request is always sent over TLS even when the caller uses the logical
	// h2s:// scheme.
	requestURL := *u
	requestURL.Scheme = "https"

	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), pr)
	if err != nil {
		return nil, err
	}
	if param.Header != nil {
		for k := range param.Header {
			req.Header[k] = param.Header[k]
		}
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		_ = pw.CloseWithError(err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = pw.CloseWithError(fmt.Errorf("unexpected status: %d", resp.StatusCode))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected http status: %d %s", resp.StatusCode, resp.Status)
	}

	localAddr := addr{network: "tcp", address: address}
	remoteAddr := addr{network: "tcp", address: address}
	if tlsConn != nil {
		if la := tlsConn.LocalAddr(); la != nil {
			localAddr = addr{network: la.Network(), address: la.String()}
		}
		if ra := tlsConn.RemoteAddr(); ra != nil {
			remoteAddr = addr{network: ra.Network(), address: ra.String()}
		}
	}

	return newH2TunnelConn(resp.Body, pw, pw, nil, localAddr, remoteAddr), nil
}

// ServeH2 upgrades an HTTP/2 (or HTTP/1.1) request into a full-duplex stream and
// returns a net.Conn that tunnels the VLESS protocol. Reads come from the
// request body (client -> server) and writes go into the response body
// (server -> client). It is intended to be used from an http.Handler that is
// served by an http.Server configured with http2.ConfigureServer.
func ServeH2(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing; http/2 is required")
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	local := addr{network: "tcp", address: r.Host}
	remote := addr{network: "tcp", address: r.RemoteAddr}
	if c, ok := r.Context().Value(connContextKey).(net.Conn); ok {
		if la := c.LocalAddr(); la != nil {
			local = addr{network: la.Network(), address: la.String()}
		}
		if ra := c.RemoteAddr(); ra != nil {
			remote = addr{network: ra.Network(), address: ra.String()}
		}
	}

	return newH2TunnelConn(r.Body, w, nil, flusher, local, remote), nil
}

// h2TunnelConn adapts a unidirectional HTTP/2 request/response pair into a
// net.Conn. Because HTTP/2 has no per-stream net.Conn, Read forwards directly
// from the (request/response) body and Write flushes to the peer. SetReadDeadline
// honours read deadlines so the generic bufio.Relay can terminate a blocked read
// when the opposite direction has finished.
type h2TunnelConn struct {
	r       io.ReadCloser
	w       io.Writer
	wCloser io.Closer
	flusher http.Flusher
	local   net.Addr
	remote  net.Addr

	mu     sync.Mutex
	timer  *time.Timer
	closeR sync.Once
}

func newH2TunnelConn(r io.ReadCloser, w io.Writer, wCloser io.Closer, flusher http.Flusher, local, remote net.Addr) *h2TunnelConn {
	return &h2TunnelConn{
		r:       r,
		w:       w,
		wCloser: wCloser,
		flusher: flusher,
		local:   local,
		remote:  remote,
	}
}

// closeReader closes the underlying body exactly once. It is safe to call
// concurrently with Read: closing an http2 body cancels and unblocks a pending
// read, which is how the relay terminates a direction.
func (c *h2TunnelConn) closeReader() {
	c.closeR.Do(func() { _ = c.r.Close() })
}

func (c *h2TunnelConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *h2TunnelConn) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if err != nil {
		return n, err
	}
	if c.flusher != nil {
		c.flusher.Flush()
	}
	return n, nil
}

func (c *h2TunnelConn) Close() error {
	c.closeReader()
	var err error
	if c.wCloser != nil {
		if e := c.wCloser.Close(); e != nil {
			err = e
		}
	}
	return err
}

func (c *h2TunnelConn) LocalAddr() net.Addr  { return c.local }
func (c *h2TunnelConn) RemoteAddr() net.Addr { return c.remote }

func (c *h2TunnelConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

// SetReadDeadline honours read deadlines so the generic bufio.Relay can
// terminate a blocked read when the opposite direction finishes. Because an
// HTTP/2 body has no real deadline mechanism, an expired/past deadline closes
// the body (unblocking any pending Read); a future deadline arms a timer that
// does the same on expiry.
func (c *h2TunnelConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}

	if t.IsZero() {
		return nil
	}

	if d := time.Until(t); d <= 0 {
		c.closeReader()
		return nil
	}
	c.timer = time.AfterFunc(time.Until(t), c.closeReader)
	return nil
}

func (c *h2TunnelConn) SetWriteDeadline(t time.Time) error {
	return nil
}
