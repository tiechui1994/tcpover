package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tiechui1994/tcpover"
	"github.com/tiechui1994/tcpover/config"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/outbound"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/tiechui1994/tool/log"
)

var debug bool

type header struct {
	data map[string]string
}

func (h *header) String() string {
	return fmt.Sprintf("%+v", h.data)
}

func (h *header) Set(s string) error {
	kv := strings.Split(strings.TrimSpace(s), ":")
	if len(kv) == 2 {
		if h.data == nil {
			h.data = make(map[string]string)
		}
		h.data[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return nil
}

func (h *header) Get() interface{} { return h.data }

func main() {
	runAsConnector := flag.Bool("c", false, "as connector")
	runAsAgent := flag.Bool("a", false, "as agent")
	runAsServer := flag.Bool("s", false, "as server")

	mux := flag.Bool("m", false, "mux connect")

	listenAddr := flag.String("l", ":1080", "Listen address [SC]")
	serverEndpoint := flag.String("e", "", "Server endpoint. [C]")
	name := flag.String("name", "", "proxy name [SC]")
	remoteName := flag.String("remoteName", "", "link remote proxy name. [C]")
	remoteAddr := flag.String("addr", "", "want to connect remote addr. [C]")

	vless := flag.Bool("vless", false, "support vless protocol. default wless protocol")
	cloudflare := flag.Bool("cf", false, "cloudflare proxy ip")
	gcore := flag.Bool("gc", false, "gcore proxy ip")

	h := new(header)
	flag.Var(h, "H", "protocol http header. [C]")

	flag.Parse()

	if !*runAsServer && !*runAsConnector && !*runAsAgent {
		log.Fatalln("must be run as one mode")
	}

	if *runAsServer && *listenAddr == "" {
		log.Fatalln("server must set listen addr")
	}

	if *runAsConnector && (*serverEndpoint == "" || *remoteAddr == "") {
		if *serverEndpoint == "" {
			log.Fatalln("connector must set server endpoint")
		}
		if *remoteAddr == "" {
			log.Fatalln("connector must set link remote addr")
		}
	}

	if *runAsAgent && (*serverEndpoint == "") {
		if *serverEndpoint == "" {
			log.Fatalln("agent must set server endpoint")
		}
	}

	if *runAsServer {
		app := http.Server{
			Handler: tcpover.NewServer(),
			Addr:    *listenAddr,
		}

		go func() {
			log.Infoln("addr %s tcpover service is starting...", *listenAddr)
			if err := app.ListenAndServe(); err != nil {
				log.Errorln("failed to start server: %v", err)
			}
		}()

		sigtermC := make(chan os.Signal, 1)
		signal.Notify(sigtermC, os.Interrupt, syscall.SIGTERM, syscall.SIGABRT)

		<-sigtermC // block until SIGTERM is received
		log.Errorln("SIGTERM received: gracefully shutting down...")

		if err := app.Shutdown(context.Background()); err != nil {
			log.Errorln("server shutdown error: %v", err)
		}
		return
	}

	if *runAsConnector {
		if *cloudflare || *gcore {
			var cname []string
			if *cloudflare {
				cname = []string{"cloudflare.182682.xyz", "bestcf.top"}
			}
			if *gcore {
				cname = []string{"gcore.182682.xyz", "core.quinn.eu.org"}
			}
			dialer := net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: time.Second * 30,
				Resolver: &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						list := [4]string{
							"114.114.114.114:53", "114.215.126.16:53",
							"208.67.222.222:53", "223.5.5.5:53",
						}
						address = list[time.Now().UnixMicro()%4]
						return net.Dial(network, address)
					},
				},
			}
			wss.DialProxy = func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(addr)
				domain := net.JoinHostPort(cname[int(time.Now().UnixMicro())%len(cname)], port)
				log.Infoln("proxy dial [%v] => %v", net.JoinHostPort(addr, port), domain)
				retry := true
			retry:
				conn, err := dialer.Dial(network, addr)
				if err != nil && retry {
					retry = false
					goto retry
				}
				return conn, err
			}
		}

		c := tcpover.NewClient(*serverEndpoint, nil)
		_type := ctx.Wless
		if *vless {
			_type = ctx.Vless
		}
		if err := c.Std(*remoteName, *remoteAddr, _type, h.data); err != nil {
			log.Fatalln("%v", err)
		}
		return
	}

	if *runAsAgent {
		c := tcpover.NewClient(*serverEndpoint, nil)
		_type := ctx.Wless
		if *vless {
			_type = ctx.Vless
		}

		var mode wss.Mode
		if *name == "" && *remoteName == "" {
			mode = wss.ModeDirect
		} else if *name != "" && *remoteName == "" {
			log.Infoln("register agent name [%v] ...", *name)
			// 要注册本地名称.
			mode = wss.ModeForward
		} else if *name == "" && *remoteName != "" {
			log.Infoln("connect to remote name [%v] ...", *remoteName)
			// 要连接到远端
			mode = wss.ModeForward
		} else if *name != "" && *remoteName != "" {
			log.Infoln("register agent name [%v and connect remote name [%v] ...", *name, *remoteName)
			// 自己要注册, 要连接到远端
			mode = wss.ModeForward
		}

		if *mux {
			mode = mode.Mux()
		}

		var proxying = map[string]interface{}{
			"type":   _type,
			"name":   "proxying",
			"local":  *name,
			"remote": *remoteName,
			"direct": outbound.DirectSendRecv,
			"server": *serverEndpoint,
			"mode":   mode,
			"mux":    *mux,
			"header": h.data,
		}
		if _type == ctx.Vless {
			proxying["uuid"] = time.Now().String()
		}

		var proxies []map[string]interface{}
		proxies = append(proxies, proxying)

		err := c.Serve(config.RawConfig{
			Listen:  *listenAddr,
			Proxies: proxies,
		})
		if err != nil {
			log.Fatalln("%v", err)
		}
		return
	}
}
