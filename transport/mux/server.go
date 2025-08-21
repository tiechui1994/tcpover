package mux

import (
	"net"

	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/tiechui1994/tool/log"
	"github.com/xtaci/smux"
)

func NewServer() *Service {
	return &Service{}
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
			conn.Close()
			log.Errorln("read mux stream request: %v", err)
			continue
		}

		log.Debugln("server dial connect addr: %v", request.Destination)
		local, err := net.Dial(request.Network, request.Destination)
		if err != nil {
			conn.Close()
			log.Errorln("net dial: %v", err)
			continue
		}

		remote := &serverConn{Conn: stream}
		go bufio.Relay(local, remote, nil)
	}
}
