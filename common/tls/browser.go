package tls

import (
	"bytes"
	"context"
	"errors"
	"runtime"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
)

type BrowserTLSOptions struct {
	ServerName                 string
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
	if len(t.NextProtos) != 0 && !(len(t.NextProtos) == 2 && t.NextProtos[0] == "h2" && t.NextProtos[1] == "http/1.1") {
		return options, errors.New("ALPN must be unset or [h2, http/1.1]")
	}
	if len(t.Certificates) != 0 || t.GetClientCertificate != nil {
		return options, errors.New("client certificates are unsupported")
	}
	options = BrowserTLSOptions{
		ServerName:    c.serverName,
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
