package tcpover

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/mux"
	"github.com/tiechui1994/tcpover/transport/shadowsocks/core"
	"github.com/tiechui1994/tcpover/transport/socks5"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/tiechui1994/tool/log"
	"github.com/tiechui1994/tool/util"
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
	}
}

func (s *Server) getConnectConnAndAddr(r *http.Request, w http.ResponseWriter) (remote net.Conn, addr string, err error) {
	var socket *websocket.Conn
	socket, err = s.upgrade.Upgrade(w, r, s.defaultHeader)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			http.Error(w, fmt.Sprintf("upgrade error: %v", err), http.StatusInternalServerError)
		}
		return nil, "", fmt.Errorf("upgrade error: %w", err)
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
		_, _, addr, err = vless.ReadConnFirstPacket(remote)
		return remote, addr, err
	default:
		addr, err = wless.ReadConnFirstPacket(remote)
		return remote, addr, err
	}
}

func (s *Server) forwardConnect(name, code string, mode wss.Mode, r *http.Request, w http.ResponseWriter) {
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

	// active connection
	if name != "" {
		manage, ok := s.manageConn.Load(name)
		if !ok {
			log.Errorln("agent [%v] not running", name)
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
		log.Errorln("%v", err)
		return
	}
	defer remote.Close()

	local, err := net.Dial("tcp", addr)
	if err != nil {
		log.Debugln("tcp connect [%v] : %v", addr, err)
		return
	}

	bufio.Relay(local, remote, nil)
}

func (s *Server) directConnectMux(r *http.Request, w http.ResponseWriter) {
	remote, _, err := s.getConnectConnAndAddr(r, w)
	if err != nil {
		log.Errorln("%v", err)
		return
	}
	defer remote.Close()

	server := mux.NewServer()
	err = server.NewConnection(remote)
	if err != nil {
		log.Errorln("NewConnection: %v", err)
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
		if r.URL.Path == "/health" {
			s.Health(w, r)
			return
		}
		if r.URL.Path == "/upgrade" {
			s.Upgrade(w, r)
			return
		}
		if r.URL.Path == "/" {
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

		var u *url.URL
		if regexp.MustCompile(`^/https?://`).MatchString(r.RequestURI) {
			u, _ = url.Parse(r.RequestURI[1:])
		} else {
			u, _ = url.Parse("https://api.quinn.eu.org")
		}
		log.Infoln("proxy url: %v", u)
		s.ProxyHandler(u, w, r)
		return
	}

	name := r.URL.Query().Get("name")
	code := r.URL.Query().Get("code")
	role := r.URL.Query().Get("rule")
	mode := wss.Mode(r.URL.Query().Get("mode"))

	log.Debugln("enter connections:%v, code:%v, name:%v, role:%v, mode:%v", atomic.AddInt32(&s.conn, +1), code, name, role, mode)
	defer func() {
		log.Debugln("leave connections:%v  code:%v, name:%v, role:%v, mode:%v", atomic.AddInt32(&s.conn, -1), code, name, role, mode)
	}()

	// 情况1: 直接连接
	if (role == wss.RoleAgent || role == wss.RoleConnector) && mode.IsDirect() {
		if mode.IsMux() {
			s.directConnectMux(r, w)
		} else {
			s.directConnect(r, w)
		}
		return
	}

	// 情况2: 主动连接方, 需要通过被动方
	if role == wss.RoleAgent || role == wss.RoleConnector {
		s.forwardConnect(name, code, mode, r, w)
		return
	}

	// 情况3: 管理员通道
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

func (s *Server) Upgrade(w http.ResponseWriter, r *http.Request) {
	download := strings.TrimSpace(r.Header.Get("url"))
	if !strings.HasPrefix(download, "http://") && !strings.HasPrefix(download, "https://") {
		http.Error(w, "invalid download url", http.StatusBadRequest)
		return
	}

	reader, err := util.File(download, "GET", util.WithRetry(2))
	if err != nil {
		http.Error(w, "download failure "+err.Error(), http.StatusBadRequest)
		return
	}

	pwd, _ := os.Executable()
	oldPath := filepath.Join(filepath.Dir(pwd), "stream.backup")
	fd, err := os.OpenFile(oldPath, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		http.Error(w, "create temp file failure "+err.Error(), http.StatusInternalServerError)
		return
	}

	hash := sha1.New()
	readerToHash := io.TeeReader(reader, hash)

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

func (s *Server) ProxyHandler(target *url.URL, w http.ResponseWriter, r *http.Request) {
	(&httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = target
			req.Host = target.Host
			req.RequestURI = target.RequestURI()
			req.Header.Set("Host", target.Host)
		},
	}).ServeHTTP(w, r)
}

type ControlMessage struct {
	Command uint32
	Data    map[string]interface{}
}

const (
	CommandLink = 0x01
	CommandPing = 0x02
)

func (s *Server) SSrServer(ctx context.Context, port uint16) error {
	lister, err := net.Listen("tcp", ":8080")
	if err != nil {
		return err
	}

	cipher, err := core.PickCipher("CHACHA20-IETF-POLY1305", nil, "123456")
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			conn, err := lister.Accept()
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

				_ = target
			}()
		}
	}
}
