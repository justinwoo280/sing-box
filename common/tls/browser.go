package tls

import (
	"bytes"
	"context"
	"errors"
)

type BrowserTLSOptions struct {
	ServerName       string
	CertificatePEM   string
	ECHConfigList    []byte
	GetECHConfigList func(context.Context) ([]byte, error)
	Reality          *BrowserRealityOptions
}

type BrowserRealityOptions struct {
	PublicKey [32]byte
	ShortID   [8]byte
}

// BrowserTLSConfig exports the TLS settings that Cronet can express. Keeping
// validation here also covers options that are absent from crypto/tls.Config,
// such as fragmentation and SNI spoofing.
func (c *STDClientConfig) BrowserTLSConfig() (options BrowserTLSOptions, err error) {
	t := c.config
	if c.disableSNI || t.InsecureSkipVerify || t.VerifyConnection != nil || t.VerifyPeerCertificate != nil {
		return options, errors.New("custom verification, insecure and disable_sni are unsupported")
	}
	if c.fragment || c.recordFragment || c.spoof != "" {
		return options, errors.New("TLS fragmentation and spoof are unsupported")
	}
	if t.MinVersion != 0 || t.MaxVersion != 0 || len(t.CipherSuites) != 0 || len(t.CurvePreferences) != 0 {
		return options, errors.New("TLS version, cipher and curve overrides are unsupported")
	}
	if len(t.NextProtos) != 0 && !(len(t.NextProtos) == 2 && t.NextProtos[0] == "h2" && t.NextProtos[1] == "http/1.1") {
		return options, errors.New("ALPN must be unset or [h2, http/1.1]")
	}
	if len(t.Certificates) != 0 || t.GetClientCertificate != nil {
		return options, errors.New("client certificates are unsupported")
	}
	if t.RootCAs != nil && c.certificatePEM == "" {
		return options, errors.New("a custom certificate store requires explicit TLS certificate PEM for Cronet")
	}
	return BrowserTLSOptions{ServerName: c.serverName, CertificatePEM: c.certificatePEM, ECHConfigList: bytes.Clone(t.EncryptedClientHelloConfigList)}, nil
}
