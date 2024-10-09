package tcpover

import (
	"encoding/binary"
	"fmt"
	"github.com/tiechui1994/tcpover/ctx"
	"io"
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
	"github.com/tiechui1994/tcpover/transport/wss"
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

func (s *Server) copy(local, remote io.ReadWriteCloser, deferCallback func()) {
	onceCloseLocal := &OnceCloser{Closer: local}
	onceCloseRemote := &OnceCloser{Closer: remote}

	defer func() {
		_ = onceCloseRemote.Close()
		_ = onceCloseLocal.Close()
		if deferCallback != nil {
			deferCallback()
		}
	}()

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()

		defer onceCloseRemote.Close()
		_, _ = io.CopyBuffer(remote, local, make([]byte, wss.SocketBufferLength))
	}()

	go func() {
		defer wg.Done()

		defer onceCloseLocal.Close()
		_, _ = io.CopyBuffer(local, remote, make([]byte, wss.SocketBufferLength))
	}()

	wg.Wait()
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
	if r.Header.Get("proto") == "" || r.Header.Get("proto") == ctx.Wless {
		buf := make([]byte, 1)
		_, err = io.ReadFull(remote, buf)
		if err != nil {
			return nil, "", err
		}
		buf = make([]byte, int(buf[0]))
		_, err = io.ReadFull(remote, buf)
		if err != nil {
			return nil, "", err
		}
		addr = string(buf)
		return remote, addr, nil
	} else {
		// version(1) id(16) addon(1) type(1) port(2) addrType(1)
		buf := make([]byte, 1+16+1+1+2+1)
		_, err = io.ReadFull(remote, buf)
		if err != nil {
			return nil, "", err
		}

		port := binary.BigEndian.Uint16(buf[19:])
		switch buf[len(buf)-1] {
		case 1:
			buf = make([]byte, 4)
			_, err = io.ReadFull(remote, buf)
			if err != nil {
				return nil, "", err
			}
		case 3:
			buf = make([]byte, 16)
			_, err = io.ReadFull(remote, buf)
			if err != nil {
				return nil, "", err
			}
		case 2:
			buf = make([]byte, 1)
			_, err = io.ReadFull(remote, buf)
			if err != nil {
				return nil, "", err
			}
			buf = make([]byte, int(buf[0]))
			_, err = io.ReadFull(remote, buf)
			if err != nil {
				return nil, "", err
			}
		}

		addr = net.JoinHostPort(string(buf), fmt.Sprintf("%v", port))

		buf = make([]byte, 1+1)
		_, err = remote.Write(buf)
		if err != nil {
			return nil, "", err
		}

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

		s.copy(pair.conn[0], pair.conn[1], func() {
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

	s.copy(local, remote, nil)
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
