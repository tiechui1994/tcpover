package outbound

import (
	"context"
	"fmt"
	"log"
	"net"
	"regexp"

	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/socks5"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
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

	var dispatcher dispatcher
	var err error
	if option.Mux {
	} else {
		dispatcher, err = newVlessDirectConnDispatcher(option)
	}
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

func newWlessDirectConnDispatcher(option WlessOption) (*directConnDispatcher, error) {
	client := wless.NewClient()

	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			var mode wss.Mode
			if option.Mode.IsDirect() {
				mode = wss.ModeDirect
			} else if option.Mode.IsForward() {
				mode = wss.ModeForward
			} else {
				mode = wss.ModeDirect
			}
			// name: 直接连接, name is empty
			//       远程代理, name not empty
			// mode: ModeDirect | ModeForward
			conn, err := wss.WebSocketConnect(cx, option.Server, &wss.ConnectParam{
				Name: option.Remote,
				Mode: mode,
				Role: wss.RoleAgent,
				Header: map[string][]string{
					"proto": {ctx.Wless},
				},
			})
			if err != nil {
				return nil, err
			}

			return client.StreamConn(conn, metadata.RemoteAddress())
		},
	}, nil
}

func newVlessDirectConnDispatcher(option VlessOption) (*directConnDispatcher, error) {
	client, err := vless.NewClient(option.UUID)
	if err != nil {
		return nil, err
	}

	return &directConnDispatcher{
		createConn: func(cx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
			var mode wss.Mode
			if option.Mode.IsDirect() {
				mode = wss.ModeDirect
			} else if option.Mode.IsForward() {
				mode = wss.ModeForward
			} else {
				mode = wss.ModeDirect
			}
			// name: 直接连接, name is empty
			//       远程代理, name not empty
			// mode: ModeDirect | ModeForward
			conn, err := wss.WebSocketConnect(cx, option.Server, &wss.ConnectParam{
				Name: option.Remote,
				Mode: mode,
				Role: wss.RoleAgent,
				Header: map[string][]string{
					"proto": {ctx.Vless},
				},
			})
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

func (c *directConnDispatcher) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	log.Println(metadata.SourceAddress(), "=>", metadata.RemoteAddress())
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
