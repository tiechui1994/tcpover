package mux

import (
	"bytes"
	"fmt"
	"log"
	"net"
)

type serverConn struct {
	net.Conn
	responseWritten bool
}

func (c *serverConn) Write(b []byte) (n int, err error) {
	if c.responseWritten {
		return c.Conn.Write(b)
	}

	buffer := bytes.Buffer{}
	buffer.WriteByte(statusSuccess)
	buffer.Write(b)
	_, err = c.Conn.Write(buffer.Bytes())
	if err != nil {
		log.Printf("server write mux status: %v", err)
		return
	}
	c.responseWritten = true
	return len(b), nil
}

type clientConn struct {
	net.Conn
	destination    string
	requestWritten bool
	responseRead   bool
}

func (c *clientConn) readResponse() error {
	response, err := ReadStreamResponse(c.Conn)
	if err != nil {
		log.Printf("client read mux status: %v", err)
		return err
	}
	if response.Status == statusError {
		return fmt.Errorf("remote error: %v", response.Message)
	}
	return nil
}

func (c *clientConn) Read(b []byte) (n int, err error) {
	if !c.responseRead {
		err = c.readResponse()
		if err != nil {

			return
		}
		c.responseRead = true
	}
	return c.Conn.Read(b)
}

func (c *clientConn) Write(b []byte) (n int, err error) {
	if c.requestWritten {
		return c.Conn.Write(b)
	}

	request := StreamRequest{
		Network:     "tcp",
		Destination: c.destination,
	}
	data := EncodeStreamRequest(request)
	_, err = c.Conn.Write(append(data, b...))
	if err != nil {
		return
	}
	c.requestWritten = true
	return len(b), nil
}

type protocolConn struct {
	net.Conn
	request        Request
	requestWritten bool
}

func (c *protocolConn) Write(p []byte) (n int, err error) {
	if c.requestWritten {
		return c.Conn.Write(p)
	}
	buffer := EncodeProtoRequest(c.request, p)
	n, err = c.Conn.Write(buffer)
	if err == nil {
		n--
	}
	c.requestWritten = true
	return n, err
}
