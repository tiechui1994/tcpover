package outbound

import (
	"context"
	"fmt"
	"net"
	"regexp"

	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/mux"
	"github.com/tiechui1994/tcpover/transport/socks5"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/tiechui1994/tool/log"
)

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
		responder.manage(option.Local, option.Header)
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

	handleOption(&option)
	log.Debugln("mux: %v, %v", option.Mux, option.Mode)

	muxClient := mux.NewClient(func() (net.Conn, error) {
		conn, err := connect(context.Background(), option.Mode, option.Server, option.Remote, ctx.Wless, option.Header)
		if err != nil {
			return nil, err
		}

		domain := "sp.mux.sing-box.arpa:444"
		return client.StreamConn(conn, domain)
	})

	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			if option.Mux {
				return muxClient.DialContext(cx, metadata)
			}

			conn, err := connect(cx, option.Mode, option.Server, option.Remote, ctx.Wless, option.Header)
			if err != nil {
				return nil, err
			}
			return client.StreamConn(conn, metadata.RemoteAddress())
		},
	}, nil
}

func newVlessDirectConnDispatcher(option VlessOption) (*directConnDispatcher, error) {
	handleOption(&(option.WlessOption))
	log.Debugln("mux: %v, %v", option.Mux, option.Mode)

	client, err := vless.NewClient(option.UUID)
	if err != nil {
		return nil, err
	}

	muxClient := mux.NewClient(func() (net.Conn, error) {
		conn, err := connect(context.Background(), option.Mode, option.Server, option.Remote, ctx.Vless, option.Header)
		if err != nil {
			return nil, err
		}

		domain := "sp.mux.sing-box.arpa"
		return client.StreamConn(conn, &vless.DstAddr{
			UDP:      false,
			AddrType: vless.AtypDomainName,
			Addr:     append([]byte{byte(len(domain))}, []byte(domain)...),
			Port:     444,
		})
	})

	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			if option.Mux {
				return muxClient.DialContext(cx, metadata)
			}

			conn, err := connect(cx, option.Mode, option.Server, option.Remote, ctx.Vless, option.Header)
			if err != nil {
				return nil, err
			}

			return client.StreamConn(conn, parseVlessAddr(metadata))
		},
	}, nil
}

type directConnDispatcher struct {
	createConn func(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error)
}

func connect(ctx context.Context, optionMode wss.Mode, optionServer, remoteName string, proxyType ctx.ProxyType, header map[string]string) (net.Conn, error) {
	// name: 直接连接, name is empty
	//       远程代理, name not empty
	// mode: ModeDirect | ModeForward
	conn, err := wss.WebSocketConnect(ctx, optionServer, &wss.ConnectParam{
		Name:   remoteName,
		Mode:   optionMode,
		Role:   wss.RoleAgent,
		Header: wss.Header(proxyType, header),
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (c *directConnDispatcher) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	log.Debugln("dispatcher from %v => %v", metadata.SourceAddress(), metadata.RemoteAddress())
	return c.createConn(ctx, metadata)
}

func parseVlessAddr(metadata *ctx.Metadata) *vless.DstAddr {
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
	}
}
