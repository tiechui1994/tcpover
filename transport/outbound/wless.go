package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"io"
	"log"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/tiechui1994/tcpover/ctx"
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

	var dispatcher dispatcher
	var err error
	if option.Mux {
	} else {
		dispatcher, err = newWlessDirectConnDispatcher(option)
	}
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
					addr := cmd.Data["Addr"].(string)
					network := cmd.Data["Network"].(string)
					proto := cmd.Data["Proto"].(string)
					if v := cmd.Data["Mux"]; v.(bool) {
					} else {
						err = c.connectLocal(code, network, addr, proto)
					}
					if err != nil {
						log.Println("ConnectLocal:", err)
					}
				}()
			}
		}
	}()
}

func (c *PassiveResponder) connectLocal(code, network, addr, proto string) error {
	var local io.ReadWriteCloser
	var err error
	local, err = net.Dial(network, addr)
	if err != nil {
		return err
	}

	onceCloseLocal := &OnceCloser{Closer: local}
	defer onceCloseLocal.Close()

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

	switch proto {
	case ctx.Vless:
		client, _ := vless.NewClient("")
		domain := "vless.mux"
		conn, err = client.StreamConn(conn, &vless.DstAddr{
			UDP:      false,
			AddrType: vless.AtypDomainName,
			Port:     443,
			Addr:     append([]byte{uint8(len(domain))}, []byte(domain)...),
		})
	case ctx.Wless:
		conn, err = wless.NewClient().StreamConn(conn, "wless.mux:443")
	}
	if err != nil {
		return err
	}

	remote := conn
	onceCloseRemote := &OnceCloser{Closer: remote}
	defer onceCloseLocal.Close()

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()

		defer onceCloseRemote.Close()
		_, err = io.CopyBuffer(remote, local, make([]byte, wss.SocketBufferLength))
		log.Printf("ConnectLocal::error1: %v", err)
	}()

	go func() {
		defer wg.Done()

		defer onceCloseLocal.Close()
		_, err = io.CopyBuffer(local, remote, make([]byte, wss.SocketBufferLength))
		log.Printf("ConnectLocal::error2: %v", err)
	}()

	wg.Wait()
	return nil
}

type OnceCloser struct {
	io.Closer
	once sync.Once
}

func (c *OnceCloser) Close() (err error) {
	c.once.Do(func() {
		err = c.Closer.Close()
	})
	return err
}

func NewPipeConn(reader *io.PipeReader, writer *io.PipeWriter, meta *ctx.Metadata) net.Conn {
	return &pipeConn{
		reader: reader,
		writer: writer,
		local:  &addr{network: meta.NetWork, addr: meta.SourceAddress()},
		remote: &addr{network: meta.NetWork, addr: meta.RemoteAddress()},
	}
}

type addr struct {
	network string
	addr    string
}

func (a *addr) Network() string {
	return a.network
}

func (a *addr) String() string {
	return a.addr
}

type pipeConn struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	local  net.Addr
	remote net.Addr
}

func (p *pipeConn) Close() error {
	err := p.reader.Close()
	if err != nil {
		return err
	}

	err = p.writer.Close()
	return err
}

func (p *pipeConn) Read(b []byte) (n int, err error) {
	return p.reader.Read(b)
}

func (p *pipeConn) Write(b []byte) (n int, err error) {
	return p.writer.Write(b)
}

func (p *pipeConn) LocalAddr() net.Addr {
	return p.local
}

func (p *pipeConn) RemoteAddr() net.Addr {
	return p.remote
}

func (p *pipeConn) SetDeadline(t time.Time) error {
	return nil
}

func (p *pipeConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (p *pipeConn) SetWriteDeadline(t time.Time) error {
	return nil
}
