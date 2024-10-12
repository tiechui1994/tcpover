package mux

import (
	"github.com/tiechui1994/tcpover/transport/common/bufio"
	"github.com/tiechui1994/tcpover/transport/wss"
	"github.com/xtaci/smux"
	"log"
	"net"
)

func NewServer() *Service {
	return &Service{}
}

type Service struct{}

func (s *Service) NewConnection(conn net.Conn) error {
	// read proto
	request, err := ReadProtoRequest(conn)
	if err != nil {
		log.Printf("read proto request: %v", err)
		return err
	}

	// new session with request
	session, err := newServerSession(conn, request.Protocol)
	if err != nil {
		log.Printf("create session for proto %v : %v", request.Protocol, err)
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
			log.Println("err: ", err)
			return err
		}

		// read mux addr
		request, err := ReadStreamRequest(stream)
		if err != nil {
			conn.Close()
			log.Printf("read multiplex stream request: %v", err)
			continue
		}
		log.Printf("ReadStreamRequest: %v", request)
		local, err := net.Dial(request.Network, request.Destination)
		if err != nil {
			conn.Close()
			log.Printf("net dail: %v", err)
			continue
		}

		remote := &serverConn{Conn: stream}
		go bufio.Relay(local, remote, func() {

		})
	}
}
