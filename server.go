package tcpover

import (
	"fmt"
	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/outbound"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/xtaci/smux"
)

type PairGroup struct {
	done chan struct{}
	conn []net.Conn
}

type Server struct {
	manageConn sync.Map // addr <=> conn

	groupMux  sync.RWMutex
	groupConn map[string]*PairGroup // code <=> []conn

	upgrade *websocket.Upgrader
	conn    int32 // number of active connections
}

func NewServer() *Server {
	return &Server{
		upgrade:   &websocket.Upgrader{},
		groupConn: map[string]*PairGroup{},
	}
}

func (s *Server) getConnectConnAndAddr(r *http.Request, w http.ResponseWriter) (remote net.Conn, addr string, err error) {
	socket, err := s.upgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return nil, "", fmt.Errorf("upgrade error: %v", err)
	}

	remote = wss.NewWebsocketConn(socket)
	defer func() {
		if err != nil {
			remote.Close()
		} else {
			log.Printf("connect addr: %v", addr)
		}
	}()

	var proto = r.Header.Get("proto")
	log.Printf("proto %v", proto)
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
	conn, addr, err := s.getConnectConnAndAddr(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// active connection
	if name != "" {
		manage, ok := s.manageConn.Load(name)
		if !ok {
			log.Printf("agent [%v] not running", name)
			http.Error(w, fmt.Sprintf("Agent [%v] not connect", name), http.StatusBadRequest)
			return
		}

		var proto = ctx.Wless
		if r.Header.Get("proto") == ctx.Vless {
			proto = ctx.Vless
		}

		code = time.Now().Format("20060102150405.9999")
		data := map[string]interface{}{
			"Code":    code,
			"Addr":    addr,
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

		bufio.Relay(pair.conn[0], pair.conn[1], func() {
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	local, err := net.Dial("tcp", addr)
	if err != nil {
		log.Printf("tcp connect [%v] : %v", addr, err)
		http.Error(w, fmt.Sprintf("tcp connect failed: %v", err), http.StatusInternalServerError)
		return
	}

	bufio.Relay(local, remote, nil)
}

func (s *Server) directConnectMux(r *http.Request, w http.ResponseWriter) {
	remote, _, err := s.getConnectConnAndAddr(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	defer func() {
		remote.Close()
	}()

	session, err := smux.Server(remote, &outbound.DefaultMuxConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Printf("smux Server: %v", err)
		return
	}

	var proto = r.Header.Get("proto")

	for {
		conn, err := session.AcceptStream()
		if err != nil && err == smux.ErrTimeout {
			continue
		}
		if err != nil {
			return
		}

		var addr string
		switch proto {
		case ctx.Vless:
			_, _, addr, err = vless.ReadConnFirstPacket(conn)
		default:
			addr, err = wless.ReadConnFirstPacket(conn)
		}
		if err != nil {
			conn.Close()
			continue
		}

		local, err := net.Dial("tcp", addr)
		if err != nil {
			conn.Close()
			continue
		}
		remote := conn
		go bufio.Relay(local, remote, func() {
			log.Printf("mux close: %v, count: %v", addr, session.NumStreams())
		})
	}
}

func (s *Server) manageConnect(name string, r *http.Request, w http.ResponseWriter) {
	conn, err := s.upgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		http.Error(w, fmt.Sprintf("upgrade error: %v", err), http.StatusBadRequest)
		return
	}

	s.manageConn.Store(name, conn)
	defer s.manageConn.Delete(name)

	conn.SetPingHandler(func(message string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		} else if err == nil {
			return nil
		}

		if wss.IsClose(err) {
			log.Printf("closing ..... : %v", conn.Close())
			return err
		}
		log.Printf("pong error: %v", err)
		return err
	})
	for {
		_, _, err := conn.ReadMessage()
		if wss.IsClose(err) {
			log.Printf("closing ..... : %v", conn.Close())
			return
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") != "websocket" {
		var u *url.URL
		if regexp.MustCompile(`^/https?://`).MatchString(r.RequestURI) {
			u, _ = url.Parse(r.RequestURI[1:])
		} else {
			u, _ = url.Parse("https://api.quinn.eu.org")
		}
		log.Printf("url: %v", u)
		s.ProxyHandler(u, w, r)
		return
	}

	name := r.URL.Query().Get("name")
	code := r.URL.Query().Get("code")
	role := r.URL.Query().Get("rule")
	mode := wss.Mode(r.URL.Query().Get("mode"))

	log.Printf("enter connections:%v, code:%v, name:%v, role:%v, mode:%v", atomic.AddInt32(&s.conn, +1), code, name, role, mode)
	defer func() {
		log.Printf("leave connections:%v  code:%v, name:%v, role:%v, mode:%v", atomic.AddInt32(&s.conn, -1), code, name, role, mode)
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
)
