package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/xtaci/smux"

	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"github.com/tiechui1994/tcpover/transport/wss"
)

const (
	DirectRecvOnly = "recv"
	DirectSendOnly = "send"
	DirectSendRecv = "sendrecv"
)

type WlessOption struct {
	Name   string   `proxy:"name"`
	Local  string   `proxy:"local"`
	Remote string   `proxy:"remote"`
	Mode   wss.Mode `proxy:"mode"`
	Server string   `proxy:"server"`
	Direct string   `proxy:"direct"`
	Mux    bool     `proxy:"mux"`
}

func NewWless(option WlessOption) (ctx.Proxy, error) {
	if option.Server == "" {
		return nil, fmt.Errorf("server must be set")
	}
	if !regexp.MustCompile(`^(ws|wss)://`).MatchString(option.Server) {
		return nil, fmt.Errorf("server must be startsWith wss:// or ws://")
	}

	dispatcher, err := newWlessDirectConnDispatcher(option)
	if err != nil {
		return nil, err
	}

	if option.Direct == DirectRecvOnly || option.Direct == DirectSendRecv {
		responder := PassiveResponder{server: option.Server}
		responder.manage(option.Local)
	}

	return &Wless{
		base: &base{
			name:      option.Name,
			proxyType: ctx.Wless,
		},
		dispatcher: dispatcher,
	}, nil
}

type dispatcher interface {
	DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error)
}

type Wless struct {
	*base
	dispatcher dispatcher
}

func (p *Wless) DialContext(ctx context.Context, metadata *ctx.Metadata) (net.Conn, error) {
	return p.dispatcher.DialContext(ctx, metadata)
}

type ControlMessage struct {
	Command uint32
	Data    map[string]interface{}
}

const (
	CommandLink = 0x01
)

type PassiveResponder struct {
	server string
}

func (c *PassiveResponder) manage(name string) {
	times := 1
try:
	time.Sleep(time.Second * time.Duration(times))
	if times >= 64 {
		times = 1
	}

	conn, err := wss.RawWebSocketConnect(context.Background(), c.server, &wss.ConnectParam{
		Name: name,
		Role: wss.RoleManager,
	})
	if err != nil {
		log.Printf("Manage::DialContext: %v", err)
		times = times * 2
		goto try
	}

	var onceClose sync.Once
	closeFunc := func() {
		log.Printf("Manage Socket Close: %v", conn.Close())
		c.manage(name)
		log.Printf("Reconnect to server success")
	}

	go func() {
		defer onceClose.Do(closeFunc)

		for {
			var cmd ControlMessage
			_, p, err := conn.ReadMessage()
			if wss.IsClose(err) {
				return
			}
			if err != nil {
				log.Printf("ReadMessage: %+v", err)
				continue
			}
			err = json.Unmarshal(p, &cmd)
			if err != nil {
				log.Printf("Unmarshal: %v", err)
				continue
			}

			switch cmd.Command {
			case CommandLink:
				log.Printf("ControlMessage => cmd %v, data: %v", cmd.Command, cmd.Data)
				go func() {
					code := cmd.Data["Code"].(string)
					network := cmd.Data["Network"].(string)
					proto := cmd.Data["Proto"].(string)
					if v := cmd.Data["Mux"]; v.(bool) {
						err = c.connectLocalMux(code, network, proto)
					} else {
						err = c.connectLocal(code, network, proto)
					}
					if err != nil {
						log.Println("ConnectLocal:", err)
					}
				}()
			}
		}
	}()
}

func (c *PassiveResponder) connectLocal(code, network, proto string) error {
	conn, err := wss.WebSocketConnect(context.Background(), c.server, &wss.ConnectParam{
		Code: code,
		Role: wss.RoleAgent,
		Header: map[string][]string{
			"proto": {proto},
		},
	})
	if err != nil {
		return err
	}
	defer func() {
		conn.Close()
	}()

	var addr string
	switch proto {
	case ctx.Vless:
		_, _, addr, err = vless.ReadConnFirstPacket(conn, false)
	default:
		addr, err = wless.ReadConnFirstPacket(conn)
	}
	if err != nil {
		return err
	}

	// link
	remote := conn
	local, err := net.Dial(network, addr)
	if err != nil {
		return err
	}

	bufio.Relay(local, remote, nil)
	return nil
}

func (c *PassiveResponder) connectLocalMux(code, network, proto string) error {
	conn, err := wss.WebSocketConnect(context.Background(), c.server, &wss.ConnectParam{
		Code: code,
		Role: wss.RoleAgent,
		Header: map[string][]string{
			"proto": {proto},
		},
	})
	if err != nil {
		return err
	}
	defer func() {
		conn.Close()
	}()

	switch proto {
	case ctx.Vless:
		_, _, _, err = vless.ReadConnFirstPacket(conn, true)
	default:
		_, err = wless.ReadConnFirstPacket(conn)
	}
	if err != nil {
		return err
	}

	// mux link
	session, err := smux.Server(conn, &DefaultMuxConfig)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := session.AcceptStream()
			if err != nil && err == smux.ErrTimeout {
				continue
			}
			if wss.IsClose(err) {
				return
			}
			if err != nil {
				log.Printf("session.AcceptStream: %v", err)
				return
			}

			var addr string
			switch proto {
			case ctx.Vless:
				addr, err = vless.ReadMuxAddr(conn)
			case ctx.Wless:
				addr, err = wless.ReadConnFirstPacket(conn)
			}
			if err != nil {
				conn.Close()
				continue
			}

			remote := conn
			local, err := net.Dial(network, addr)
			if err != nil {
				conn.Close()
				continue
			}

			go bufio.Relay(local, remote, func() {
				log.Printf("close: %v", addr)
			})
		}
	}()

	return nil
}
