package main

import (
	"flag"
	"github.com/tiechui1994/tcpover/transport/outbound"
	"log"
	"net/http"

	"github.com/tiechui1994/tcpover"
	"github.com/tiechui1994/tcpover/config"
	"github.com/tiechui1994/tcpover/ctx"
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
	name := flag.String("name", "", "name [SC]")
	remoteName := flag.String("remoteName", "", "remoteName. [C]")
	remoteAddr := flag.String("addr", "", "remoteAddr. [C]")

	flag.Parse()

	if !*runAsServer && !*runAsConnector && !*runAsAgent {
		log.Fatalln("must be run as one mode")
	}

	if *runAsServer && *listenAddr == "" {
		log.Fatalln("server must set listen addr")
	}

	if *runAsConnector && (*serverEndpoint == "" || *remoteAddr == "") {
		log.Fatalln("agent must set server endpoint and addr")
	}

	if *runAsAgent && (*serverEndpoint == "" || *name == "") {
		log.Fatalln("agent must set server endpoint and name")
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
		if err := c.Std(*remoteName, *remoteAddr); err != nil {
			log.Fatalln(err)
		}
		return
	}

	if *runAsAgent {
		c := tcpover.NewClient(*serverEndpoint, nil)
		var proxies []map[string]interface{}

		proxies = append(proxies, map[string]interface{}{
			"type":   ctx.Vless,
			"name":   "proxy1",
			"local":  *name,
			"remote": "",
			"direct": outbound.DirectSendRecv,
			"server": *serverEndpoint,
			"mode":   wss.ModeDirect,
			"mux":    *mux,
		})

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
