package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/netip"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

// xhttpTransport builds an xhttp v2ray transport with the given uplink mode.
// stream-up requires TLS (HTTP/2) by design, so passing it without TLS must
// fail at outbound construction.
func xhttpTransport(mode string) *option.V2RayTransportOptions {
	return &option.V2RayTransportOptions{
		Type: C.V2RayTransportTypeXHTTP,
		XHTTPOptions: option.V2RayXHTTPOptions{
			V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{
				Mode: mode,
				Path: "/xhttp",
			},
		},
	}
}

// generateEWPKeypair mirrors sewp.GenerateServerStaticKeypair (X25519, base64
// std) inline so the test module needs no extra dependency.
func generateEWPKeypair(t *testing.T) (privB64, pubB64 string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(key.Bytes()),
		base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}

func xhttpRoute() *option.RouteOptions {
	return &option.RouteOptions{
		Rules: []option.Rule{
			{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{Inbound: []string{"mixed-in"}},
					RuleAction: option.RuleAction{
						Action:       C.RuleActionTypeRoute,
						RouteOptions: option.RouteActionOptions{Outbound: "proxy-out"},
					},
				},
			},
		},
	}
}

func mixedInbound() option.Inbound {
	return option.Inbound{
		Type: C.TypeMixed,
		Tag:  "mixed-in",
		Options: &option.HTTPMixedInboundOptions{
			ListenOptions: option.ListenOptions{
				Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
				ListenPort: clientPort,
			},
		},
	}
}

func TestVLESSXHTTP(t *testing.T) {
	t.Run("packet-up-tls", func(t *testing.T) { testVLESSXHTTP(t, "packet-up", true) })
	t.Run("stream-up-tls", func(t *testing.T) { testVLESSXHTTP(t, "stream-up", true) })
	t.Run("packet-up-plain", func(t *testing.T) { testVLESSXHTTP(t, "packet-up", false) })
}

func TestVLESSXHTTPAutoMode(t *testing.T) {
	t.Run("empty-mode-plain", func(t *testing.T) { testVLESSXHTTP(t, "", false) })
	t.Run("empty-mode-tls", func(t *testing.T) { testVLESSXHTTP(t, "", true) })
	t.Run("auto-string-plain", func(t *testing.T) { testVLESSXHTTP(t, "auto", false) })
	t.Run("auto-string-tls", func(t *testing.T) { testVLESSXHTTP(t, "auto", true) })
}

func TestVLESSXHTTPH3(t *testing.T) {
	t.Run("packet-up", func(t *testing.T) { testVLESSXHTTPWithALPN(t, "packet-up", true, "h3") })
	t.Run("stream-up", func(t *testing.T) { testVLESSXHTTPWithALPN(t, "stream-up", true, "h3") })
	t.Run("stream-one", func(t *testing.T) { testVLESSXHTTPWithALPN(t, "stream-one", true, "h3") })
}

func TestVLESSXHTTPStreamOne(t *testing.T) {
	t.Run("stream-one-plain", func(t *testing.T) { testVLESSXHTTP(t, "stream-one", false) })
	t.Run("stream-one-tls", func(t *testing.T) { testVLESSXHTTP(t, "stream-one", true) })
}

func TestVLESSXHTTPAllModes(t *testing.T) {
	modes := []struct {
		name   string
		mode   string
		useTLS bool
	}{
		{"packet-up/plain", "packet-up", false},
		{"packet-up/tls", "packet-up", true},
		{"stream-up/tls", "stream-up", true},
		{"stream-one/plain", "stream-one", false},
		{"stream-one/tls", "stream-one", true},
		{"auto/plain", "", false},
		{"auto/tls", "", true},
		{"auto-string/plain", "auto", false},
		{"auto-string/tls", "auto", true},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) { testVLESSXHTTP(t, m.mode, m.useTLS) })
	}
}

func testVLESSXHTTP(t *testing.T, mode string, useTLS bool) {
	testVLESSXHTTPWithALPN(t, mode, useTLS, "")
}

func testVLESSXHTTPWithALPN(t *testing.T, mode string, useTLS bool, alpn string) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	transport := xhttpTransport(mode)

	var inTLS option.InboundTLSOptionsContainer
	var outTLS option.OutboundTLSOptionsContainer
	if useTLS {
		_, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
		inTLS.TLS = &option.InboundTLSOptions{
			Enabled: true, ServerName: "example.org", CertificatePath: certPem, KeyPath: keyPem,
		}
		outTLS.TLS = &option.OutboundTLSOptions{
			Enabled: true, ServerName: "example.org", CertificatePath: certPem,
		}
		if alpn != "" {
			inTLS.TLS.ALPN = badoption.Listable[string]{alpn}
			outTLS.TLS.ALPN = badoption.Listable[string]{alpn}
		}
	}

	startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			mixedInbound(),
			{
				Type: C.TypeVLESS,
				Options: &option.VLESSInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users:                      []option.VLESSUser{{UUID: user.String()}},
					InboundTLSOptionsContainer: inTLS,
					Transport:                  transport,
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect},
			{
				Type: C.TypeVLESS,
				Tag:  "proxy-out",
				Options: &option.VLESSOutboundOptions{
					ServerOptions:               option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
					UUID:                        user.String(),
					OutboundTLSOptionsContainer: outTLS,
					Transport:                   transport,
				},
			},
		},
		Route: xhttpRoute(),
	})
	testSuit(t, clientPort, testPort)
}

func TestEWPXHTTP(t *testing.T) {
	t.Run("packet-up-tls", func(t *testing.T) { testEWPXHTTP(t, "packet-up") })
	t.Run("stream-up-tls", func(t *testing.T) { testEWPXHTTP(t, "stream-up") })
}

// testEWPXHTTP runs EWP/v2.1 (server identity bound) over xhttp + TLS. EWP
// layers its own AEAD handshake on top of the xhttp HTTP/2 stream, so a
// passing run proves both the transport and the protocol stacking work.
func testEWPXHTTP(t *testing.T, mode string) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	priv, pub := generateEWPKeypair(t)
	transport := xhttpTransport(mode)
	_, certPem, keyPem := createSelfSignedCertificate(t, "example.org")

	startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			mixedInbound(),
			{
				Type: C.TypeEWP,
				Options: &option.EWPInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: serverPort,
					},
					Users:                  []option.EWPUser{{UUID: user.String()}},
					ServerStaticPrivateKey: priv,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled: true, ServerName: "example.org", CertificatePath: certPem, KeyPath: keyPem,
						},
					},
					Transport: transport,
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect},
			{
				Type: C.TypeEWP,
				Tag:  "proxy-out",
				Options: &option.EWPOutboundOptions{
					ServerOptions:         option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
					UUID:                  user.String(),
					ServerStaticPublicKey: pub,
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled: true, ServerName: "example.org", CertificatePath: certPem,
						},
					},
					Transport: transport,
				},
			},
		},
		Route: xhttpRoute(),
	})
	testSuit(t, clientPort, testPort)
}
