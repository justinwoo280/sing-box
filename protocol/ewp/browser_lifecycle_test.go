//go:build with_cronet && with_purego && with_cronet_test

package ewp

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/logger"
)

func TestInvalidOutboundClosesBrowserTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := option.ServerOptions{Server: "127.0.0.1", ServerPort: 443}
	transport := &option.V2RayTransportOptions{
		Type:         "xhttp",
		XHTTPOptions: option.V2RayXHTTPOptions{Browser: true},
	}
	invalidEncoding := "invalid"
	cases := []struct {
		name      string
		wantError string
		create    func() (adapter.Outbound, error)
	}{
		{"ewp", "parse EWP/v2.3 client config", func() (adapter.Outbound, error) {
			return NewOutbound(ctx, nil, logger.NOP(), "invalid-browser", option.EWPOutboundOptions{ServerOptions: server, Transport: transport})
		}},
		{"vless", "unsupported flow", func() (adapter.Outbound, error) {
			return vless.NewOutbound(ctx, nil, logger.NOP(), "invalid-browser", option.VLESSOutboundOptions{ServerOptions: server, Transport: transport, Flow: "invalid"})
		}},
		{"vmess", "unknown packet encoding", func() (adapter.Outbound, error) {
			return vmess.NewOutbound(ctx, nil, logger.NOP(), "invalid-browser", option.VMessOutboundOptions{
				ServerOptions: server, Transport: transport, PacketEncoding: invalidEncoding,
				Multiplex: &option.OutboundMultiplexOptions{Enabled: true},
			})
		}},
		{"trojan", "brutal: invalid upload speed", func() (adapter.Outbound, error) {
			return trojan.NewOutbound(ctx, nil, logger.NOP(), "invalid-browser", option.TrojanOutboundOptions{
				ServerOptions: server, Transport: transport,
				Multiplex: &option.OutboundMultiplexOptions{Enabled: true, Brutal: &option.BrutalOptions{Enabled: true}},
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseline := runtime.NumGoroutine()
			for i := 0; i < 20; i++ {
				outbound, err := tc.create()
				_ = common.Close(outbound)
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q error, got %v", tc.wantError, err)
				}
			}
			// A Browser transport waits on its owning context until Close is called.
			deadline := time.Now().Add(time.Second)
			for {
				if got := runtime.NumGoroutine(); got <= baseline+4 {
					return
				} else if time.Now().After(deadline) {
					t.Fatalf("goroutines retained after outbound creation failed: %d -> %d", baseline, got)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
