package anytls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"

	"github.com/tiechui1994/tcpover/transport/anytls/padding"
	"github.com/tiechui1994/tcpover/transport/anytls/session"
	"github.com/tiechui1994/tcpover/transport/common/ca"
)

type ClientConfig struct {
	Password                 string
	IdleSessionCheckInterval time.Duration
	IdleSessionTimeout       time.Duration
	MinIdleSession           int
	Server                   string
	Dialer                   net.Dialer
	TLSConfig                *TLSConfig
}

type Client struct {
	passwordSha256 []byte
	tlsConfig      *TLSConfig
	dialer         net.Dialer
	server         string
	sessionClient  *session.Client
	padding        atomic.Value
}

func NewClient(ctx context.Context, config ClientConfig) *Client {
	pw := sha256.Sum256([]byte(config.Password))
	c := &Client{
		passwordSha256: pw[:],
		tlsConfig:      config.TLSConfig,
		dialer:         config.Dialer,
		server:         config.Server,
	}
	// Initialize the padding state of this client
	padding.UpdatePaddingScheme(padding.DefaultPaddingScheme, &c.padding)
	c.sessionClient = session.NewClient(ctx, c.createOutboundTLSConnection, &c.padding, config.IdleSessionCheckInterval, config.IdleSessionTimeout, config.MinIdleSession)
	return c
}

func (c *Client) createOutboundTLSConnection(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, "tcp", c.server)
	if err != nil {
		return nil, err
	}

	b := bytes.NewBuffer(nil)

	b.Write(c.passwordSha256)
	var paddingLen int
	if pad := c.padding.Load().(*padding.PaddingFactory).GenerateRecordPayloadSizes(0); len(pad) > 0 {
		paddingLen = pad[0]
	}

	binary.Write(b, binary.BigEndian, uint16(paddingLen))
	if paddingLen > 0 {
		b.Write(make([]byte, paddingLen))
	}

	tlsConn, err := StreamTLSConn(ctx, conn, c.tlsConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}

	_, err = b.WriteTo(tlsConn)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (c *Client) Close() error {
	return c.sessionClient.Close()
}

type TLSConfig struct {
	Host              string
	SkipCertVerify    bool
	FingerPrint       string
	Certificate       string
	PrivateKey        string
	ClientFingerprint string
	NextProtos        []string
}

func StreamTLSConn(ctx context.Context, conn net.Conn, cfg *TLSConfig) (net.Conn, error) {
	tlsConfig, err := ca.GetTLSConfig(ca.Option{
		TLSConfig: &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.SkipCertVerify,
			NextProtos:         cfg.NextProtos,
		},
		Fingerprint: cfg.FingerPrint,
		Certificate: cfg.Certificate,
		PrivateKey:  cfg.PrivateKey,
	})
	if err != nil {
		return nil, err
	}

	tlsConn := tls.Client(conn, tlsConfig)

	err = tlsConn.HandshakeContext(ctx)
	return tlsConn, err
}
