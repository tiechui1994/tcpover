package http

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"

	"golang.org/x/net/http2"

	"github.com/tiechui1994/tcpover/transport/common/ca"
	"github.com/tiechui1994/tcpover/transport/vless"
	"github.com/tiechui1994/tcpover/transport/wss"
)

// TestVlessOverH1 exercises the full tcp+tls+http (non-CONNECT) tunnel:
// client --TLS--> HTTP request --> server, then VLESS runs on top of the
// tunnel and the server simply echoes the payload back.
func TestVlessOverH1(t *testing.T) {
	cert, err := ca.LoadTLSKeyPair("", "")
	if err != nil {
		t.Fatalf("load cert: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	tlsListener := tls.NewListener(lis, &tls.Config{Certificates: []tls.Certificate{cert}})
	go func() {
		for {
			conn, err := tlsListener.Accept()
			if err != nil {
				return
			}
			go func() {
				tunnel, _, err := ServeConn(conn)
				if err != nil {
					conn.Close()
					return
				}
				// consume the vless request header and reply the vless response
				if _, err := vless.ReadAddr(tunnel); err != nil {
					tunnel.Close()
					return
				}
				// echo back whatever the client sends through the tunnel
				buf := make([]byte, 4096)
				for {
					n, err := tunnel.Read(buf)
					if n > 0 {
						if _, werr := tunnel.Write(buf[:n]); werr != nil {
							break
						}
					}
					if err != nil {
						break
					}
				}
				tunnel.Close()
			}()
		}
	}()

	server := "https://" + lis.Addr().String() + "/"
	conn, err := Connect(context.Background(), server, &ConnectParam{
		Header: wss.Header("Vless", nil),
	})
	if err != nil {
		t.Fatalf("h1 connect: %v", err)
	}
	defer conn.Close()

	client, err := vless.NewClient("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("vless client: %v", err)
	}

	dst := &vless.DstAddr{
		UDP:      false,
		AddrType: vless.AtypDomainName,
		Addr:     append([]byte{byte(len("echo"))}, []byte("echo")...),
		Port:     80,
	}
	vconn, err := client.StreamConn(conn, dst)
	if err != nil {
		t.Fatalf("vless stream: %v", err)
	}

	payload := []byte("hello-vless-over-https")
	if _, err := vconn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(vconn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(buf) != string(payload) {
		t.Fatalf("unexpected echo: got %q want %q", buf, payload)
	}
}

// TestVlessOverH2 exercises the full tcp+tls+http2 tunnel: client --TLS(ALPN
// h2)--> HTTP/2 POST --> server, then VLESS runs on top of the tunnel and the
// server simply echoes the payload back.
func TestVlessOverH2(t *testing.T) {
	cert, err := ca.LoadTLSKeyPair("", "")
	if err != nil {
		t.Fatalf("load cert: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	tlsListener := tls.NewListener(lis, &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tunnel, err := ServeH2(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// consume the vless request header and reply the vless response
		if _, err := vless.ReadAddr(tunnel); err != nil {
			tunnel.Close()
			return
		}
		// echo back whatever the client sends through the tunnel
		buf := make([]byte, 4096)
		for {
			n, err := tunnel.Read(buf)
			if n > 0 {
				if _, werr := tunnel.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		tunnel.Close()
	})

	srv := &http.Server{Handler: handler}
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		t.Fatalf("configure h2: %v", err)
	}
	go func() {
		_ = srv.Serve(tlsListener)
	}()
	defer srv.Close()

	server := "h2://" + lis.Addr().String() + "/"
	conn, err := Connect(context.Background(), server, &ConnectParam{
		Header: wss.Header("Vless", nil),
	})
	if err != nil {
		t.Fatalf("h2 connect: %v", err)
	}
	defer conn.Close()

	client, err := vless.NewClient("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("vless client: %v", err)
	}

	dst := &vless.DstAddr{
		UDP:      false,
		AddrType: vless.AtypDomainName,
		Addr:     append([]byte{byte(len("echo"))}, []byte("echo")...),
		Port:     80,
	}
	vconn, err := client.StreamConn(conn, dst)
	if err != nil {
		t.Fatalf("vless stream: %v", err)
	}

	payload := []byte("hello-vless-over-https-h2")
	if _, err := vconn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(vconn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(buf) != string(payload) {
		t.Fatalf("unexpected echo: got %q want %q", buf, payload)
	}
}
