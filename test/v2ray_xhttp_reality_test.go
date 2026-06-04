//go:build with_utls

package main

import (
	"net/netip"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/gofrs/uuid/v5"
)

func xhttpRealityTransport(mode string) *option.V2RayTransportOptions {
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

func TestVLESSXHTTPReality(t *testing.T) {
	t.Run("auto-mode", func(t *testing.T) { testVLESSXHTTPReality(t, "") })
	t.Run("auto-string", func(t *testing.T) { testVLESSXHTTPReality(t, "auto") })
	t.Run("stream-one", func(t *testing.T) { testVLESSXHTTPReality(t, "stream-one") })
	t.Run("packet-up", func(t *testing.T) { testVLESSXHTTPReality(t, "packet-up") })
}

func testVLESSXHTTPReality(t *testing.T, mode string) {
	user, _ := uuid.NewV4()
	transport := xhttpRealityTransport(mode)

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
					Users: []option.VLESSUser{{UUID: user.String()}},
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:    true,
							ServerName: "google.com",
							Reality: &option.InboundRealityOptions{
								Enabled: true,
								Handshake: option.InboundRealityHandshakeOptions{
									ServerOptions: option.ServerOptions{
										Server:     "google.com",
										ServerPort: 443,
									},
								},
								ShortID:    []string{"0123456789abcdef"},
								PrivateKey: "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc",
							},
						},
					},
					Transport: transport,
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect},
			{
				Type: C.TypeVLESS,
				Tag:  "proxy-out",
				Options: &option.VLESSOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "127.0.0.1",
						ServerPort: serverPort,
					},
					UUID: user.String(),
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:    true,
							ServerName: "google.com",
							Reality: &option.OutboundRealityOptions{
								Enabled:   true,
								ShortID:   "0123456789abcdef",
								PublicKey: "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
							},
							UTLS: &option.OutboundUTLSOptions{
								Enabled: true,
							},
						},
					},
					Transport: transport,
				},
			},
		},
		Route: xhttpRoute(),
	})
	testTCP(t, clientPort, testPort)
}
