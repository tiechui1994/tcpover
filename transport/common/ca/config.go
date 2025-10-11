package ca

import "C"
import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
)

var globalCertPool *x509.CertPool
var mutex sync.RWMutex
var errNotMatch = errors.New("certificate fingerprints do not match")

//go:embed ca-certificates.crt
var _CaCertificates []byte
var DisableEmbedCa, _ = strconv.ParseBool(os.Getenv("DISABLE_EMBED_CA"))
var DisableSystemCa, _ = strconv.ParseBool(os.Getenv("DISABLE_SYSTEM_CA"))

func initializeCertPool() {
	var err error
	if DisableSystemCa {
		globalCertPool = x509.NewCertPool()
	} else {
		globalCertPool, err = x509.SystemCertPool()
		if err != nil {
			globalCertPool = x509.NewCertPool()
		}
	}
	if !DisableEmbedCa {
		globalCertPool.AppendCertsFromPEM(_CaCertificates)
	}
}

func GetCertPool() *x509.CertPool {
	mutex.Lock()
	defer mutex.Unlock()
	if globalCertPool == nil {
		initializeCertPool()
	}
	return globalCertPool
}

type Option struct {
	TLSConfig   *tls.Config
	Fingerprint string
	ZeroTrust   bool
	Certificate string
	PrivateKey  string
}

func GetTLSConfig(opt Option) (tlsConfig *tls.Config, err error) {
	tlsConfig = opt.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}
	tlsConfig.Time = time.Now

	if opt.ZeroTrust {
		tlsConfig.RootCAs = zeroTrustCertPool()
	} else {
		tlsConfig.RootCAs = GetCertPool()
	}

	if len(opt.Fingerprint) > 0 {
		tlsConfig.VerifyPeerCertificate, err = NewFingerprintVerifier(opt.Fingerprint, tlsConfig.Time)
		if err != nil {
			return nil, err
		}
		tlsConfig.InsecureSkipVerify = true
	}

	if len(opt.Certificate) > 0 || len(opt.PrivateKey) > 0 {
		var cert tls.Certificate
		cert, err = LoadTLSKeyPair(opt.Certificate, opt.PrivateKey, C.Path)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

var zeroTrustCertPool = func() *x509.CertPool {
	if len(_CaCertificates) != 0 { // always using embed cert first
		zeroTrustCertPool := x509.NewCertPool()
		if zeroTrustCertPool.AppendCertsFromPEM(_CaCertificates) {
			return zeroTrustCertPool
		}
	}
	return nil // fallback to system pool
}
