package invest

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
)

func TestEmbeddedRootCertificate(t *testing.T) {
	block, rest := pem.Decode(russianTrustedRootCA)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatal("expected exactly one PEM certificate")
	}
	if got := fmt.Sprintf("%X", sha256.Sum256(block.Bytes)); got != "D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31" {
		t.Fatalf("unexpected root certificate fingerprint: %s", got)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA {
		t.Fatal("embedded certificate is not a CA")
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Fatal(err)
	}
	roots, err := investRootCAs()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		t.Fatalf("embedded root is not trusted or has expired: %v", err)
	}
}
