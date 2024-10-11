package tcpover

import (
	"context"
	"fmt"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wless"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/tiechui1994/tcpover/config"
	"github.com/tiechui1994/tcpover/transport"
	"github.com/tiechui1994/tcpover/transport/wss"
)

var (
	Debug bool
)

type Client struct {
	server string
}

func NewClient(server string, proxy map[string][]string) *Client {
	if !strings.Contains(server, "://") {
		server = "wss://" + server
	}
	if proxy == nil {
		proxy = map[string][]string{}
	}

	return &Client{
		server: server,
	}
}

func (c *Client) Std(remoteName, remoteAddr string) error {
	var std io.ReadWriteCloser = NewStdReadWriteCloser()
	if Debug {
		std = NewEchoReadWriteCloser()
	}

	if err := c.stdConnectServer(std, remoteName, remoteAddr); err != nil {
		log.Printf("Std::ConnectServer %v", err)
		return err
	}

	return nil
}

func (c *Client) Serve(config config.RawConfig) error {
	for _, v := range config.Proxies {
		proxy, err := transport.ParseProxy(v)
		if err != nil {
			return err
		}

		transport.RegisterProxy(proxy)
	}

	listenAddr := fmt.Sprintf("%v", config.Listen)
	log.Printf("listen [%v] ...", config.Listen)
	err := transport.RegisterListener("mixed", listenAddr)
	if err != nil {
		return err
	}

	done := make(chan struct{})
	<-done
	return nil
}

func (c *Client) stdConnectServer(local io.ReadWriteCloser, remoteName, remoteAddr string) error {
	var mode = wss.ModeForward
	if remoteName == "" || remoteName == remoteAddr {
		mode = wss.ModeDirect
	}

	var proto = ctx.Wless
	conn, err := wss.WebSocketConnect(context.Background(), c.server, &wss.ConnectParam{
		Name: remoteName,
		Role: wss.RoleConnector,
		Mode: mode,
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
		host, port, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			return err
		}
		portVal, _ := strconv.Atoi(port)
		conn, err = client.StreamConn(conn, &vless.DstAddr{
			UDP:      false,
			AddrType: vless.AtypDomainName,
			Addr:     append([]byte{uint8(len(host))}, []byte(host)...),
			Port:     uint(portVal),
			Mux:      false,
		})
	case ctx.Wless:
		conn, err = wless.NewClient().StreamConn(conn, remoteAddr)
	}
	if err != nil {
		return err
	}
	remote := conn

	wg := &sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()

		defer local.Close()
		_, err = io.CopyBuffer(remote, local, make([]byte, wss.SocketBufferLength))
	}()

	go func() {
		defer wg.Done()

		defer remote.Close()
		_, err = io.CopyBuffer(local, remote, make([]byte, wss.SocketBufferLength))
	}()

	wg.Wait()
	return nil
}
