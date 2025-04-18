package bufio

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type dump struct {
	net.Conn
	read  *os.File
	write *os.File
}

func New(conn net.Conn) net.Conn {
	d := &dump{Conn:conn}
	name := fmt.Sprintf("%v-%v", conn.LocalAddr(), conn.RemoteAddr())
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, ".", "")
	d.read, _ = os.Create(fmt.Sprintf("./read_%v.txt", name ))
	d.write, _ = os.Create(fmt.Sprintf("./write_%v.txt", name))
	return d
}

func (s *dump) Write(p []byte) (n int, err error) {
	n, err = s.Conn.Write(p)
	if n > 0 {
		_, _ = s.write.Write(p[:n])
	}
	return n, err
}

func (s *dump) Read(p []byte) (n int, err error) {
	n, err = s.Conn.Read(p)
	if n > 0 {
		_, _ = s.read.Write(p[:n])
	}
	return n, err
}

func Relay(leftConn, rightConn net.Conn, deferCall func(err error)) {
	var err error
	defer func() {
		leftConn.Close()
		rightConn.Close()
		if deferCall != nil {
			deferCall(err)
		}
	}()

	ch := make(chan error)

	go func() {
		_, err := io.Copy(leftConn, rightConn)
		_ = leftConn.SetReadDeadline(time.Now())
		ch <- err
	}()

	_, _ = io.Copy(rightConn, leftConn)
	_ = rightConn.SetReadDeadline(time.Now())
	err = <-ch
}
