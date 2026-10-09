package tcpover

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/common/ca"
	"github.com/tiechui1994/tcpover/transport/common/log"
	thttp "github.com/tiechui1994/tcpover/transport/http"
	"github.com/tiechui1994/tcpover/transport/inbound"
	"github.com/tiechui1994/tcpover/transport/mux"
	"github.com/tiechui1994/tcpover/transport/shadowsocks/core"
	"github.com/tiechui1994/tcpover/transport/socks5"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
	"golang.org/x/net/http2"
)

type PairGroup struct {
	done chan struct{}
	conn []net.Conn
}

type Server struct {
	manageConn sync.Map // addr <=> conn

	groupMux  sync.RWMutex
	groupConn map[string]*PairGroup // code <=> []conn

	defaultHeader http.Header
	upgrade       *websocket.Upgrader
	conn          int32 // number of active connections

	date time.Time
}

func NewServer() *Server {
	return &Server{
		defaultHeader: map[string][]string{
			"X-Version": {Version},
		},
		upgrade: &websocket.Upgrader{
			Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
				w.Header().Set("Sec-Websocket-Version", "13")
				w.Header().Set("X-Version", Version)
				http.Error(w, http.StatusText(status), status)
			},
		},
		groupConn: map[string]*PairGroup{},
		date:      time.Now(),
	}
}

func (s *Server) getConnectConnAndAddr(r *http.Request, w http.ResponseWriter) (remote net.Conn, addr socks5.Addr, err error) {
	var socket *websocket.Conn
	socket, err = s.upgrade.Upgrade(w, r, s.defaultHeader)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			http.Error(w, fmt.Sprintf("upgrade error: %v", err), http.StatusInternalServerError)
		}
		return nil, nil, fmt.Errorf("upgrade error: %w", err)
	}

	remote = wss.NewWebsocketConn(socket)
	defer func() {
		if err != nil {
			remote.Close()
			return
		}
		log.Debugln("connect addr: %v", addr)
	}()

	var proto = r.Header.Get("proto")
	if proto == "" {
		proto = r.URL.Query().Get("proto")
	}
	log.Debugln("proto %v", proto)
	switch proto {
	case ctx.Vless:
		addr, err = vless.ReadAddr(remote)
		return remote, addr, err
	default:
		addr, err = wless.ReadAddr(remote)
		return remote, addr, err
	}
}

func (s *Server) forwardConnect(remoteName, code string, mode wss.Mode, r *http.Request, w http.ResponseWriter) {
	socket, err := s.upgrade.Upgrade(w, r, s.defaultHeader)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			http.Error(w, fmt.Sprintf("upgrade error: %v", err), http.StatusInternalServerError)
		}
		log.Errorln("upgrade error: %v", err)
		return
	}
	conn := wss.NewWebsocketConn(socket)
	defer conn.Close()

	if code == "" && remoteName == "" {
		log.Errorln("code and name is empty")
		return
	}

	// active connection
	if remoteName != "" {
		manage, ok := s.manageConn.Load(remoteName)
		if !ok {
			log.Errorln("agent [%v] not running", remoteName)
			return
		}

		var proto = ctx.Wless
		if r.Header.Get("proto") == ctx.Vless {
			proto = ctx.Vless
		} else if r.URL.Query().Get("proto") == ctx.Vless {
			proto = ctx.Vless
		}

		code = time.Now().Format("20060102150405.9999")
		data := map[string]interface{}{
			"Code":    code,
			"Network": "tcp",
			"Mux":     mode.IsMux(),
			"Proto":   proto,
		}
		_ = manage.(*websocket.Conn).WriteJSON(ControlMessage{
			Command: CommandLink,
			Data:    data,
		})
	}

	// 配对连接
	s.groupMux.Lock()
	if pair, ok := s.groupConn[code]; ok {
		pair.conn = append(pair.conn, conn)
		s.groupMux.Unlock()

		bufio.Relay(pair.conn[0], pair.conn[1], func(err error) {
			close(pair.done)
			s.groupMux.Lock()
			delete(s.groupConn, code)
			s.groupMux.Unlock()
		})
	} else {
		pair := &PairGroup{
			done: make(chan struct{}),
			conn: []net.Conn{conn},
		}
		s.groupConn[code] = pair
		s.groupMux.Unlock()
		<-pair.done
	}
}

func (s *Server) directConnect(r *http.Request, w http.ResponseWriter) {
	remote, addr, err := s.getConnectConnAndAddr(r, w)
	if err != nil {
		if !wss.IsClose(err) {
			log.Errorln("get connect addr: %v", err)
		}		
		return
	}
	defer remote.Close()

	cc := inbound.NewSocket(addr, remote, ctx.SHADOWSOCKS)
	if mux.IsSpecialFqdn(cc.Metadata().Host) {
		server := mux.NewServer()
		_ = server.NewConnection(remote)
	} else {
		local, err := net.Dial("tcp", cc.Metadata().RemoteAddress())
		if err != nil {
			log.Debugln("tcp connect [%v] : %v", addr, err)
			return
		}

		bufio.Relay(local, remote, nil)
	}
}

func (s *Server) manageConnect(name string, r *http.Request, w http.ResponseWriter) {
	conn, err := s.upgrade.Upgrade(w, r, s.defaultHeader)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			http.Error(w, fmt.Sprintf("upgrade error: %v", err), http.StatusInternalServerError)
		}
		log.Errorln("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.manageConn.Store(name, conn)
	defer s.manageConn.Delete(name)

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()
	for range ticker.C {
		err := conn.WriteJSON(ControlMessage{
			Command: CommandPing,
			Data:    map[string]interface{}{},
		})
		if wss.IsClose(err) {
			log.Errorln("closing ..... : %v", conn.Close())
			return
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") != "websocket" {
		if strings.HasSuffix(r.URL.Path, "/health") {
			s.Health(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/upgrade") {
			s.Upgrade(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/time") {
			s.Time(w, r)
			return
		}

		closed := r.URL.Query().Get("close")
		if closed != "" {
			s.Version(w, r)
			time.AfterFunc(5*time.Second, func() {
				os.Exit(0)
			})
			return
		}

		s.Version(w, r)
		return
	}

	name := r.URL.Query().Get("name")
	code := r.URL.Query().Get("code")

	mode := wss.Mode(r.URL.Query().Get("mode"))

	uuid := time.Now().Format("2006.0102.150405.9999")
	atomic.AddInt32(&s.conn, +1)
	log.Debugln("enter:%v, code:%v, name:%v, mode:%v", uuid, code, name, mode)
	defer func() {
		atomic.AddInt32(&s.conn, -1)
		log.Debugln("leave:%v  code:%v, name:%v, mode:%v", uuid, code, name, mode)
	}()

	// 情况1: 直接连接
	if mode.IsDirect() {
		s.directConnect(r, w)
		return
	}

	// 情况2: 主动连接方, 需要通过被动方
	if mode.IsForward() {
		s.forwardConnect(name, code, mode, r, w)
		return
	}

	// 情况3: 管理员通道
	role := r.URL.Query().Get("rule")
	if role == wss.RoleManager {
		s.manageConnect(name, r, w)
		return
	}
}

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	raw, _ := json.Marshal(map[string]interface{}{
		"message":     "tcpover service is healthy",
		"environment": os.Getenv("ENV"),
		"timestamp":   time.Now(),
	})
	_, _ = w.Write(raw)
}

func (s *Server) Version(w http.ResponseWriter, r *http.Request) {
	raw, _ := json.Marshal(map[string]interface{}{
		"version": Version,
		"now":     time.Now().Format("2006-01-02T15:04:05.9999"),
	})
	_, _ = w.Write(raw)
}

func (s *Server) Time(w http.ResponseWriter, r *http.Request) {
	raw, _ := json.Marshal(map[string]interface{}{
		"time":  time.Since(s.date).String(),
		"start": s.date.Format("2006-01-02T15:04:05.9999"),
		"now":   time.Now().Format("2006-01-02T15:04:05.9999"),
	})
	_, _ = w.Write(raw)
}

func (s *Server) Upgrade(w http.ResponseWriter, r *http.Request) {
	download := strings.TrimSpace(r.Header.Get("url"))
	if !strings.HasPrefix(download, "http://") && !strings.HasPrefix(download, "https://") {
		http.Error(w, "invalid download url", http.StatusBadRequest)
		return
	}

	response, err := http.Get(download)
	if err != nil {
		http.Error(w, "download failure "+err.Error(), http.StatusBadRequest)
		return
	}
	defer response.Body.Close()

	pwd, _ := os.Executable()
	oldPath := filepath.Join(filepath.Dir(pwd), "stream.backup")
	fd, err := os.OpenFile(oldPath, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		http.Error(w, "create temp file failure "+err.Error(), http.StatusInternalServerError)
		return
	}

	hash := sha1.New()
	readerToHash := io.TeeReader(response.Body, hash)

	_, err = io.Copy(fd, readerToHash)
	if err != nil {
		http.Error(w, "download copy failure "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = os.Rename(oldPath, pwd)
	if err != nil {
		http.Error(w, "rename failure "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = fmt.Fprint(w, hex.EncodeToString(hash.Sum(nil)))
}

type ControlMessage struct {
	Command uint32
	Data    map[string]interface{}
}

const (
	CommandLink = 0x01
	CommandPing = 0x02
)

func (s *Server) SS(ct context.Context, port uint16, name, password string) error {
	var listenConfig = net.ListenConfig{
		Control: Control,
	}

	listen, err := listenConfig.Listen(ct, "tcp", fmt.Sprintf("0.0.0.0:%v", port))
	if err != nil {
		return err
	}

	cipher, err := core.PickCipher(name, nil, password)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ct.Done():
			return nil
		default:
			conn, err := listen.Accept()
			if err != nil {
				continue
			}
			go func() {
				conn := cipher.StreamConn(conn)
				target, err := socks5.ReadAddr0(conn)
				if err != nil {
					_ = conn.Close()
					return
				}

				cc := inbound.NewSocket(target, conn, ctx.SHADOWSOCKS)
				if mux.IsSpecialFqdn(cc.Metadata().Host) {
					server := mux.NewServer()
					_ = server.NewConnection(cc.Conn())
				} else {
					local, err := net.Dial("tcp", cc.Metadata().RemoteAddress())
					if err != nil {
						log.Debugln("tcp connect [%v] : %v", cc.Metadata().RemoteAddress(), err)
						return
					}

					bufio.Relay(local, cc.Conn(), nil)
				}
			}()
		}
	}
}

func (s *Server) TCPVless(ct context.Context, port uint16) error {
	var listenConfig = net.ListenConfig{
		Control: Control,
	}

	listen, err := listenConfig.Listen(ct, "tcp", fmt.Sprintf("0.0.0.0:%v", port))
	if err != nil {
		return err
	}

	for {
		select {
		case <-ct.Done():
			return nil
		default:
			conn, err := listen.Accept()
			if err != nil {
				continue
			}
			go func() {
				s.serveVlessTunnel(conn)
			}()
		}
	}
}

// serveVlessTunnel reads the VLESS request header from tunnel, dials the target
// and relays traffic. It is shared by the raw VLESS, VLESS-over-H1 and
// VLESS-over-H2 servers.
func (s *Server) serveVlessTunnel(tunnel net.Conn) {
	defer tunnel.Close()

	addr, err := vless.ReadAddr(tunnel)
	if err != nil {
		return
	}

	cc := inbound.NewSocket(addr, tunnel, ctx.SHADOWSOCKS)
	if mux.IsSpecialFqdn(cc.Metadata().Host) {
		server := mux.NewServer()
		_ = server.NewConnection(cc.Conn())
		return
	}

	local, err := net.Dial("tcp", cc.Metadata().RemoteAddress())
	if err != nil {
		log.Debugln("tcp connect [%v] : %v", cc.Metadata().RemoteAddress(), err)
		return
	}

	bufio.Relay(local, cc.Conn(), nil)
}

// TCPVlessH1 serves the VLESS protocol over a TCP+TLS+HTTP transport
// (non-CONNECT). Each connection is upgraded into a full-duplex stream via a
// plain HTTP request/response as implemented in transport/http.
//
// certPEM and keyPEM are optional PEM encoded certificate/key. When both are
// empty a random self-signed key pair is generated.
func (s *Server) TCPVlessH1(ct context.Context, port uint16, certPEM, keyPEM string) error {
	cert, err := ca.LoadTLSKeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}

	var listenConfig = net.ListenConfig{
		Control: Control,
	}
	listen, err := listenConfig.Listen(ct, "tcp", fmt.Sprintf("0.0.0.0:%v", port))
	if err != nil {
		return err
	}

	tlsListener := tls.NewListener(listen, &tls.Config{
		Certificates: []tls.Certificate{cert},
	})

	for {
		select {
		case <-ct.Done():
			return nil
		default:
			conn, err := tlsListener.Accept()
			if err != nil {
				continue
			}
			go func() {
				tunnel, _, err := thttp.ServeConn(conn)
				if err != nil {
					_ = conn.Close()
					return
				}
				s.serveVlessTunnel(tunnel)
			}()
		}
	}
}

// TCPVlessH2 serves the VLESS protocol over a TCP+TLS+HTTP/2 (or HTTP/1.1)
// transport. The TLS listener advertises both "h2" and "http/1.1" via ALPN so
// the same port can be reached by either protocol. Each HTTP request is upgraded
// into a full-duplex stream via transport/http.ServeH2 and then carries VLESS.
//
// certPEM and keyPEM are optional PEM encoded certificate/key. When both are
// empty a random self-signed key pair is generated.
func (s *Server) TCPVlessH2(ct context.Context, port uint16, certPEM, keyPEM string) error {
	cert, err := ca.LoadTLSKeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}

	var listenConfig = net.ListenConfig{
		Control: Control,
	}
	listen, err := listenConfig.Listen(ct, "tcp", fmt.Sprintf("0.0.0.0:%v", port))
	if err != nil {
		return err
	}

	tlsListener := tls.NewListener(listen, &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tunnel, err := thttp.ServeH2(w, r)
		if err != nil {
			log.Errorln("h2 serve conn: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.serveVlessTunnel(tunnel)
	})

	server := &http.Server{
		Handler: handler,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, thttp.ConnContextKey, c)
		},
	}
	if err := http2.ConfigureServer(server, &http2.Server{}); err != nil {
		return err
	}

	go func() {
		<-ct.Done()
		_ = server.Close()
	}()

	if err := server.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
