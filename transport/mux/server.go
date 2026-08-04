package mux

import (
	"context"
	"net"
	"time"
	"unsafe"

	"github.com/tiechui1994/tcpover/ctx"

	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/tiechui1994/tool/log"
	"github.com/xtaci/smux"
)

//go:linkname writeControlFrame github.com/xtaci/smux.(*Session).writeControlFrame
func writeControlFrame(m *smux.Session, f smux.Frame) (n int, err error)

type ServiceHandler interface {
	NewConnection(ctx context.Context, conn net.Conn, meta *ctx.Metadata)
}

func NewServer() *Service {
	return &Service{}
}

type frame struct {
	ver  byte   // version
	cmd  byte   // command
	sid  uint32 // stream id
	data []byte // payload
}

type Service struct{}

func (s *Service) NewConnection(conn net.Conn) error {
	// read proto
	request, err := ReadProtoRequest(conn)
	if err != nil {
		log.Errorln("service read proto request: %v", err)
		return err
	}

	// new session with request
	session, err := newServerSession(conn, request.Protocol)
	if err != nil {
		log.Errorln("service create session proto %v : %v", request.Protocol, err)
		return err
	}
	defer session.Close()

	// hook code
	go func() {
		ticker := time.NewTicker(DefaultMuxConfig.KeepAliveInterval)
		defer func() {
			_ = recover()
			ticker.Stop()
		}()

		frame := *(*smux.Frame)(unsafe.Pointer(&frame{
			ver:  byte(DefaultMuxConfig.Version),
			cmd:  3,
			sid:  1,
			data: make([]byte, 0),
		}))

		for range ticker.C {
			_, _ = writeControlFrame(session, frame)
		}
	}()

	var stream net.Conn
	for {
		stream, err = session.AcceptStream()
		if err != nil && err == smux.ErrTimeout {
			continue
		}
		if wss.IsClose(err) {
			return nil
		}
		if err != nil {
			log.Errorln("err: %v", err)
			return err
		}

		// read mux addr
		request, err := ReadStreamRequest(stream)
		if err != nil {
			log.Errorln("read mux stream request: %v", err)
			continue
		}

		log.Debugln("mux dial connect: %v", request.Destination)
		local, err := net.Dial(request.Network, request.Destination)
		if err != nil {
			log.Errorln("net dial: %v", err)
			continue
		}

		remote := &serverConn{Conn: stream}
		go bufio.Relay(local, remote, nil)
	}
}
