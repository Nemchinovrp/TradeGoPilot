package invest

import (
	"crypto/x509"
	_ "embed"
	"fmt"
)

// Downloaded over verified HTTPS from the Gosuslugi CDN; see certs/README.md.
//
//go:embed certs/russian_trusted_root_ca.pem
var russianTrustedRootCA []byte

// investRootCAs adds the API's CA only to this client's trust store.
func investRootCAs() (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system root certificates: %w", err)
	}
	if !roots.AppendCertsFromPEM(russianTrustedRootCA) {
		return nil, fmt.Errorf("invalid embedded Invest API root certificate")
	}
	return roots, nil
}
