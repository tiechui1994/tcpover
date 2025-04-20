package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/tiechui1994/tcpover"
	"github.com/tiechui1994/tcpover/config"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/outbound"
	"github.com/tiechui1994/tcpover/transport/wss"
)

var debug bool

func init() {
	log.SetFlags(log.Lshortfile | log.Ltime)
}

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

	if *runAsAgent && (*serverEndpoint == "" || *name == "") {
		if *serverEndpoint == "" {
			log.Fatalln("agent must set server endpoint")
		}
		if *name == "" {
			log.Fatalln("agent must set link proxy name, others link it")
		}
	}

	if *runAsServer {
		log.Printf("server [%v] start ...", *listenAddr)
		s := tcpover.NewServer()
		if err := http.ListenAndServe(*listenAddr, s); err != nil {
			log.Fatalln(err)
		}
		return
	}

	if *runAsConnector {
		c := tcpover.NewClient(*serverEndpoint, nil)
		_type := ctx.Wless
		if *vless {
			_type = ctx.Vless
		}
		if err := c.Std(*remoteName, *remoteAddr, _type); err != nil {
			log.Fatalln(err)
		}
		return
	}

	if *runAsAgent {
		c := tcpover.NewClient(*serverEndpoint, nil)
		_type := ctx.Wless
		if *vless {
			_type = ctx.Vless
		}
		mode := wss.ModeDirect
		if *remoteName != "" && *mux {
			mode = wss.ModeForwardMux
		} else if *remoteName != "" && !*mux {
			mode = wss.ModeForward
		} else if *remoteName == "" && *mux {
			mode = wss.ModeDirectMux
		}

		var proxying =  map[string]interface{}{
			"type":   _type,
			"name":   "proxying",
			"local":  *name,
			"remote": *remoteName,
			"direct": outbound.DirectSendRecv,
			"server": *serverEndpoint,
			"mode":   mode,
			"mux":    *mux,
		}
		if _type == ctx.Vless {
			proxying["uuid"] = ""
		}

		if *mux {
			proxying["mode"] = wss.ModeDirectMux
		}
		var proxies []map[string]interface{}
		proxies = append(proxies, proxying)

		err := c.Serve(config.RawConfig{
			Listen:  *listenAddr,
			Proxies: proxies,
		})
		if err != nil {
			log.Fatalln(err)
		}
		return
	}
}
