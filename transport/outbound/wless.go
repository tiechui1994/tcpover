package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tiechui1994/tcpover/transport/mux"
	"log"
	"net"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

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
		responder := &PassiveResponder{server: option.Server}
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
	CommandPing = 0x02
)

type PassiveResponder struct {
	count  int32
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
		Mode: wss.ModeForward,
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
		_, _, addr, err = vless.ReadConnFirstPacket(conn)
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
		Mode: wss.ModeForwardMux,
		Header: map[string][]string{
			"proto": {proto},
		},
	})
	if err != nil {
		return err
	}

	switch proto {
	case ctx.Vless:
		_, _, _, err = vless.ReadConnFirstPacket(conn)
	default:
		_, err = wless.ReadConnFirstPacket(conn)
	}
	if err != nil {
		return err
	}

	log.Printf("conncet count: %v", atomic.AddInt32(&(c.count), 1))
	server := mux.NewServer()
	go func() {
		defer func() {
			log.Printf("disConncet count: %v", atomic.AddInt32(&(c.count), -1))
			conn.Close()
		}()
		err = server.NewConnection(conn)
		if err != nil {
			log.Printf("NewConnection: %v", err)
		}
	}()

	return nil
}
