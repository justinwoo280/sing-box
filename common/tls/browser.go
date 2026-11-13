package tls

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"slices"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
)

type BrowserTLSOptions struct {
	ServerName                 string
	ALPN                       []string
	CertificatePEM             string
	UseTrustedRootCertificates bool
	ECHConfigList              []byte
	GetECHConfigList           func(context.Context) ([]byte, error)
	Reality                    *BrowserRealityOptions
}

type BrowserRealityOptions struct {
	PublicKey [32]byte
	ShortID   [8]byte
}

type browserCertificateStore interface {
	adapter.CertificateStore
	BrowserRootCertificates() (string, bool)
	IsDefaultSystemStore() bool
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
	for i, protocol := range t.NextProtos {
		if protocol != "h2" && protocol != "http/1.1" && protocol != "h3" {
			return options, errors.New("ALPN must contain h2, http/1.1, or h3")
		}
		if slices.Contains(t.NextProtos[:i], protocol) {
			return options, errors.New("duplicate ALPN protocol")
		}
		if protocol == "h3" && len(t.NextProtos) != 1 {
			return options, errors.New("h3 ALPN cannot be combined with TCP protocols")
		}
	}
	if len(t.Certificates) != 0 || t.GetClientCertificate != nil {
		return options, errors.New("client certificates are unsupported")
	}
	options = BrowserTLSOptions{
		ServerName:    c.serverName,
		ALPN:          slices.Clone(t.NextProtos),
		ECHConfigList: bytes.Clone(t.EncryptedClientHelloConfigList),
	}
	if c.certificatePEM != "" {
		options.CertificatePEM = c.certificatePEM
		options.UseTrustedRootCertificates = true
		return options, nil
	}
	if t.RootCAs == nil {
		return options, nil
	}
	store, ok := service.FromContext[adapter.CertificateStore](c.ctx).(browserCertificateStore)
	if !ok || t.RootCAs != store.Pool() {
		return options, errors.New("a custom certificate store requires an exportable PEM trust set for Cronet")
	}
	if roots, exportable := store.BrowserRootCertificates(); exportable {
		options.CertificatePEM = roots
		options.UseTrustedRootCertificates = true
		return options, nil
	}
	if store.IsDefaultSystemStore() && runtime.GOOS != "android" {
		return options, nil
	}
	return options, errors.New("a custom certificate store requires an exportable PEM trust set for Cronet")
}
