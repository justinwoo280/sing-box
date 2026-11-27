package certificate

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"maps"
	"os"
	"strings"
	"testing"
)

func TestBundledCertificateStores(t *testing.T) {
	for _, test := range []struct {
		name string
		pem  string
	}{
		{"mozilla", mozillaIncludedPEM()},
		{"chrome", chromeIncludedPEM()},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle, err := os.ReadFile(test.name + ".pem")
			if err != nil {
				t.Fatal(err)
			}
			embedded := bundledCertificateSet(t, []byte(test.pem))
			standalone := bundledCertificateSet(t, bundle)
			if !maps.Equal(embedded, standalone) {
				t.Fatal("embedded Go certificate store differs from its standalone PEM bundle")
			}
		})
	}
}

func bundledCertificateSet(t *testing.T, data []byte) map[[sha256.Size]byte]bool {
	t.Helper()
	certificates := make(map[[sha256.Size]byte]bool)
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			if strings.TrimSpace(string(data)) != "" {
				t.Fatal("invalid trailing data in certificate bundle")
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			t.Fatalf("unexpected PEM block type %q", block.Type)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if certificate.Subject.CommonName == "emSign ECC Root CA - G3" {
			t.Error("bundle contains the emSign root excluded by the certificate generator")
		}
		digest := sha256.Sum256(certificate.Raw)
		if certificates[digest] {
			t.Fatalf("duplicate certificate %x", digest)
		}
		certificates[digest] = true
		data = rest
	}
	if len(certificates) == 0 {
		t.Fatal("empty certificate bundle")
	}
	return certificates
}
