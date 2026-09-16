package main

import (
	"net/netip"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

func xhttp14Transport() *option.V2RayTransportOptions {
	return &option.V2RayTransportOptions{
		Type: C.V2RayTransportTypeXHTTP,
		XHTTPOptions: option.V2RayXHTTPOptions{
			V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{
				Mode: "packet-up",
				Path: "/xhttp",
			},
		},
	}
}

func TestVLESSXHTTP14PacketUp(t *testing.T) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	_, certPem, keyPem := createSelfSignedCertificate(t, "example.org")
	transport := xhttp14Transport()

	startInstance(t, option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "mixed-in",
				Options: &option.HTTPMixedInboundOptions{ListenOptions: option.ListenOptions{
					Listen: common.Ptr(badoption.Addr(netip.IPv4Unspecified())), ListenPort: clientPort,
				}},
			},
			{
				Type: C.TypeVLESS,
				Options: &option.VLESSInboundOptions{
					ListenOptions: option.ListenOptions{Listen: common.Ptr(badoption.Addr(netip.IPv4Unspecified())), ListenPort: serverPort},
					Users:         []option.VLESSUser{{UUID: user.String()}},
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: &option.InboundTLSOptions{
						Enabled: true, ServerName: "example.org", CertificatePath: certPem, KeyPath: keyPem,
					}},
					Transport: transport,
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect},
			{Type: C.TypeVLESS, Tag: "proxy-out", Options: &option.VLESSOutboundOptions{
				ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
				UUID:          user.String(),
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
					Enabled: true, ServerName: "example.org", CertificatePath: certPem,
				}},
				Transport: transport,
			}},
		},
		Route: &option.RouteOptions{Rules: []option.Rule{{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{Inbound: []string{"mixed-in"}},
			RuleAction: option.RuleAction{Action: C.RuleActionTypeRoute, RouteOptions: option.RouteActionOptions{Outbound: "proxy-out"}},
		}}}},
	})

	testTCP(t, clientPort, testPort)
}
