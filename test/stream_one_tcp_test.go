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

func TestStreamOneTCPOnly(t *testing.T) {
	user, err := uuid.DefaultGenerator.NewV4()
	require.NoError(t, err)
	transport := xhttpTransport("stream-one")

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
					Users:     []option.VLESSUser{{UUID: user.String()}},
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
					ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: serverPort},
					UUID:          user.String(),
					Transport:     transport,
				},
			},
		},
		Route: xhttpRoute(),
	})
	testTCP(t, clientPort, testPort)
}
