package wss

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"time"
)

var globalFingerprints = make([][32]byte, 0, 0)

func verifyPeerCertificateAndFingerprints(fingerprints *[][32]byte, insecureSkipVerify bool) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if insecureSkipVerify {
			return nil
		}

		var preErr error
		for i := range rawCerts {
			rawCert := rawCerts[i]
			cert, err := x509.ParseCertificate(rawCert)
			if err == nil {
				opts := x509.VerifyOptions{
					CurrentTime: time.Now(),
				}

				if _, err := cert.Verify(opts); err == nil {
					return nil
				} else {
					fingerprint := sha256.Sum256(cert.Raw)
					for _, fp := range *fingerprints {
						if bytes.Equal(fingerprint[:], fp[:]) {
							return nil
						}
					}

					preErr = err
				}
			}
		}

		return preErr
	}
}

func GetGlobalFingerprintTLCConfig(tlsConfig *tls.Config) *tls.Config {
	if tlsConfig == nil {
		return &tls.Config{
			InsecureSkipVerify:    true,
			VerifyPeerCertificate: verifyPeerCertificateAndFingerprints(&globalFingerprints, false),
		}
	}

	tlsConfig.VerifyPeerCertificate = verifyPeerCertificateAndFingerprints(&globalFingerprints, tlsConfig.InsecureSkipVerify)
	tlsConfig.InsecureSkipVerify = true
	return tlsConfig
}
