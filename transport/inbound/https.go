package inbound

import (
	"net"
	"net/http"

	"github.com/tiechui1994/tcpover/ctx"
)

func NewHTTPS(request *http.Request, conn net.Conn) ctx.ConnContext {
	metadata := parseHTTPAddr(request)
	metadata.Type = ctx.HTTPCONNECT
	if ip, port, err := parseAddr(conn.RemoteAddr()); err == nil {
		metadata.SrcIP = ip
		metadata.SrcPort = uint16(port)
	}
	metadata.Origin = conn.LocalAddr().String()
	return ctx.NewConnContext(conn, metadata)
}
