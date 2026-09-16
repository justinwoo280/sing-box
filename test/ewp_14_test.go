package main

import (
	"net/netip"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

func TestEWP14TCP(t *testing.T) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	privateKey, publicKey, err := sewp.GenerateSigningIdentity()
	require.NoError(t, err)
	_, certPem, keyPem := createSelfSignedCertificate(t, "example.org")

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
				Type: C.TypeEWP,
				Tag:  "ewp-in",
				Options: &option.EWPInboundOptions{
					ListenOptions:      option.ListenOptions{Listen: common.Ptr(badoption.Addr(netip.IPv4Unspecified())), ListenPort: serverPort},
					Users:              []option.EWPUser{{Name: "test", UUID: user.String()}},
					ServerID:           "example.org",
					SigningPrivateKey:  privateKey,
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: &option.InboundTLSOptions{
						Enabled: true, ServerName: "example.org", CertificatePath: certPem, KeyPath: keyPem,
					}},
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect},
			{
				Type: C.TypeEWP,
				Tag:  "proxy-out",
				Options: &option.EWPOutboundOptions{
					ServerOptions:   option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
					UUID:            user.String(),
					ServerID:        "example.org",
					ServerPublicKey: publicKey,
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
						Enabled: true, ServerName: "example.org", CertificatePath: certPem,
					}},
				},
			},
		},
		Route: &option.RouteOptions{Rules: []option.Rule{{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{Inbound: []string{"mixed-in"}},
			RuleAction: option.RuleAction{Action: C.RuleActionTypeRoute, RouteOptions: option.RouteActionOptions{Outbound: "proxy-out"}},
		}}}},
	})

	testTCP(t, clientPort, testPort)
}
