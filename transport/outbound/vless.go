package outbound

import (
	"context"
	"fmt"
	"log"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/socks5"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/xtaci/smux"
)

var DefaultMuxConfig = smux.Config{
	Version:           1,
	KeepAliveInterval: 10 * time.Second,
	KeepAliveTimeout:  30 * time.Second,
	MaxFrameSize:      32768,
	MaxReceiveBuffer:  4194304,
	MaxStreamBuffer:   65536,
	KeepAliveDisabled: true,
}

type VlessOption struct {
	WlessOption
	UUID string `proxy:"uuid"`
}

func NewVless(option VlessOption) (ctx.Proxy, error) {
	if option.Server == "" {
		return nil, fmt.Errorf("server must be set")
	}
	if !regexp.MustCompile(`^(ws|wss)://`).MatchString(option.Server) {
		return nil, fmt.Errorf("server must be startsWith wss:// or ws://")
	}

	dispatcher, err := newVlessDirectConnDispatcher(option)
	if err != nil {
		return nil, err
	}

	if option.Direct == DirectRecvOnly || option.Direct == DirectSendRecv {
		responder := PassiveResponder{server: option.Server}
		responder.manage(option.Local)
	}

	return &Vless{
		base: &base{
			name:      option.Name,
			proxyType: ctx.Vless,
		},
		dispatcher: dispatcher,
	}, nil
}

type Vless struct {
	*base
	dispatcher dispatcher
}

func (p *Vless) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	return p.dispatcher.DialContext(ctx, metadata)
}

func handleOption(option *WlessOption) {
	if option.Mode.IsDirect() {
		if option.Mux {
			option.Mode = wss.ModeDirectMux
		} else {
			option.Mode = wss.ModeDirect
		}
	} else if option.Mode.IsForward() {

		if option.Mux {
			option.Mode = wss.ModeForwardMux
		} else {
			option.Mode = wss.ModeForward
		}
	} else {
		if option.Mux {
			option.Mode = wss.ModeDirectMux
		} else {
			option.Mode = wss.ModeDirect
		}
	}
}

func newWlessDirectConnDispatcher(option WlessOption) (*directConnDispatcher, error) {
	client := wless.NewClient()
	pool := &sync.Map{}

	handleOption(&option)
	log.Printf("mux: %v, %v", option.Mux, option.Mode)

	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			if option.Mux {
				try := 0
			again:
				if try > 2 {
					log.Printf("try too manay times")
					return nil, fmt.Errorf("try too manay times")
				}
				conn, link, err := openMuxConnectConn(pool)
				if err != nil {
					return nil, err
				}
				// mux conn link
				if link {
					return client.StreamConn(conn, metadata.RemoteAddress())
				}

				// mux session link
				conn, err = connect(cx, option.Mode, option.Server, option.Remote, ctx.Wless)
				if err != nil {
					return nil, err
				}
				domain := "mux.cool.com:443"
				conn, err = client.StreamConn(conn, domain)
				if err != nil {
					return nil, err
				}
				session, err := smux.Client(conn, &DefaultMuxConfig)
				if err != nil {
					return nil, err
				}
				pool.Store(session, session)
				try += 1
				goto again
			}

			conn, err := connect(cx, option.Mode, option.Server, option.Remote, ctx.Wless)
			if err != nil {
				return nil, err
			}
			return client.StreamConn(conn, metadata.RemoteAddress())
		},
	}, nil
}

func newVlessDirectConnDispatcher(option VlessOption) (*directConnDispatcher, error) {
	handleOption(&(option.WlessOption))
	log.Printf("mux: %v, %v", option.Mux, option.Mode)

	client, err := vless.NewClient(option.UUID)
	if err != nil {
		return nil, err
	}

	pool := &sync.Map{}
	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			if option.Mux {
			again:
				conn, link, err := openMuxConnectConn(pool)
				if err != nil {
					return nil, err
				}
				if link {
					return client.StreamConn(conn, parseVlessAddr(metadata, true))
				}

				conn, err = connect(cx, option.Mode, option.Server, option.Remote, ctx.Vless)
				if err != nil {
					return nil, err
				}

				domain := "mux.cool.com"
				conn, err = client.StreamConn(conn, &vless.DstAddr{
					UDP:      false,
					AddrType: vless.AtypDomainName,
					Addr:     append([]byte{uint8(len(domain))}, []byte(domain)...),
					Port:     443,
				})
				if err != nil {
					return nil, err
				}
				session, err := smux.Client(conn, &DefaultMuxConfig)
				if err != nil {
					return nil, err
				}
				pool.Store(session, session)
				goto again
			}

			conn, err := connect(cx, option.Mode, option.Server, option.Remote, ctx.Vless)
			if err != nil {
				return nil, err
			}

			return client.StreamConn(conn, parseVlessAddr(metadata, false))
		},
	}, nil
}

type directConnDispatcher struct {
	createConn func(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error)
}

func connect(ctx context.Context, optionMode wss.Mode, optionServer, remoteName string, proxyType ctx.ProxyType) (net.Conn, error) {
	// name: 直接连接, name is empty
	//       远程代理, name not empty
	// mode: ModeDirect | ModeForward
	conn, err := wss.WebSocketConnect(ctx, optionServer, &wss.ConnectParam{
		Name: remoteName,
		Mode: optionMode,
		Role: wss.RoleAgent,
		Header: map[string][]string{
			"proto": {proxyType},
		},
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
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

	conn.GetDieCh()

	return conn, true, nil
}

func (c *directConnDispatcher) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	log.Println(metadata.SourceAddress(), "=>", metadata.RemoteAddress())
	return c.createConn(ctx, metadata)
}

func parseVlessAddr(metadata *ctx.Metadata, mux bool) *vless.DstAddr {
	var addrType byte
	var addr []byte
	switch metadata.AddrType() {
	case socks5.AtypIPv4:
		addrType = vless.AtypIPv4
		addr = make([]byte, net.IPv4len)
		copy(addr[:], metadata.DstIP)
	case socks5.AtypIPv6:
		addrType = vless.AtypIPv6
		addr = make([]byte, net.IPv6len)
		copy(addr[:], metadata.DstIP)
	case socks5.AtypDomainName:
		addrType = vless.AtypDomainName
		addr = make([]byte, len(metadata.Host)+1)
		addr[0] = byte(len(metadata.Host))
		copy(addr[1:], metadata.Host)
	}

	port := metadata.DstPort
	return &vless.DstAddr{
		UDP:      false,
		AddrType: addrType,
		Addr:     addr,
		Port:     uint(port),
		Mux:      mux,
	}
}
