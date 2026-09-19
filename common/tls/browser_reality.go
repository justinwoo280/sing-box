package tls

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
)

// Browser REALITY exports credentials; it never opens a Go TLS connection.
type browserRealityConfig struct {
	*STDClientConfig
	reality BrowserRealityOptions
}

func newBrowserRealityClient(options ClientOptions) (Config, error) {
	o := options.Options
	if o.ECH != nil && o.ECH.Enabled {
		return nil, errors.New("browser REALITY is incompatible with ECH")
	}
	if o.KernelRx || o.KernelTx || len(o.Certificate) != 0 || o.CertificatePath != "" {
		return nil, errors.New("browser REALITY does not support kTLS or custom root certificates")
	}
	if o.UTLS != nil && o.UTLS.Enabled && o.UTLS.Fingerprint != "" && o.UTLS.Fingerprint != "chrome" {
		return nil, errors.New("browser REALITY uses Chromium's native fingerprint; only an unset or chrome uTLS fingerprint is compatible")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(o.Reality.PublicKey)
	if err != nil || len(publicKey) != 32 {
		return nil, errors.New("browser REALITY: invalid public_key")
	}
	shortID, err := hex.DecodeString(o.Reality.ShortID)
	if err != nil || len(shortID) > 8 {
		return nil, errors.New("browser REALITY: invalid short_id")
	}
	base, err := newSTDClient(options.Context, options.Logger, options.ServerAddress, o, options.AllowEmptyServerName)
	if err != nil {
		return nil, err
	}
	c := &browserRealityConfig{STDClientConfig: base.(*STDClientConfig)}
	// REALITY supplies its own trust anchor, independent of the application CA pool.
	c.config.RootCAs = nil
	copy(c.reality.PublicKey[:], publicKey)
	copy(c.reality.ShortID[:], shortID)
	if _, err := c.BrowserTLSConfig(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *browserRealityConfig) BrowserTLSConfig() (BrowserTLSOptions, error) {
	options, err := c.STDClientConfig.BrowserTLSConfig()
	if err != nil {
		return options, err
	}
	reality := c.reality
	options.Reality = &reality
	return options, nil
}

func (c *browserRealityConfig) STDConfig() (*STDConfig, error) {
	return nil, errors.New("browser REALITY requires the Cronet Browser transport")
}

func (c *browserRealityConfig) Client(net.Conn) (Conn, error) {
	return nil, errors.New("browser REALITY requires the Cronet Browser transport")
}

func (c *browserRealityConfig) Clone() Config {
	return &browserRealityConfig{STDClientConfig: c.STDClientConfig.Clone().(*STDClientConfig), reality: c.reality}
}
