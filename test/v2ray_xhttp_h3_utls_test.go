//go:build with_utls

package main

import (
	"testing"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

// TestVLESSXHTTPH3UTLSRejected pins the guard against a silent fallback.
//
// quic-go performs the TLS 1.3 handshake through crypto/tls and offers no hook
// for a caller-supplied ClientHello, so uTLS cannot shape the handshake of an
// HTTP/3 connection. Before the guard, requesting alpn h3 together with uTLS
// left the client on HTTP/2 over TCP while the server listened for QUIC, which
// surfaced only as an opaque "connection refused". Outbound construction must
// fail loudly instead.
func TestVLESSXHTTPH3UTLSRejected(t *testing.T) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	_, certPem, _ := createSelfSignedCertificate(t, "example.org")

	_, err = box.New(box.Options{
		Context: globalCtx,
		Options: option.Options{
			Log: &option.LogOptions{Disabled: true},
			Outbounds: []option.Outbound{
				{
					Type: C.TypeVLESS,
					Tag:  "proxy-out",
					Options: &option.VLESSOutboundOptions{
						ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
						UUID:          user.String(),
						OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
							TLS: &option.OutboundTLSOptions{
								Enabled:         true,
								ServerName:      "example.org",
								CertificatePath: certPem,
								ALPN:            badoption.Listable[string]{"h3"},
								UTLS:            &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"},
							},
						},
						Transport: xhttpTransport("packet-up"),
					},
				},
			},
		},
	})
	require.ErrorContains(t, err, "HTTP/3 requires a standard TLS client")
}
