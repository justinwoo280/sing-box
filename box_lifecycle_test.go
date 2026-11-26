package box_test

import (
	"context"
	"errors"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
)

// Native transports can allocate resources while their outbound is constructed,
// before Start registers any lifecycle cleanup. Their context must still be
// canceled by an unstarted Box.Close or a later constructor failure.
func TestBoxConstructorContextLifetime(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "close-before-start"
		if fail {
			name = "construction-failure"
		}
		t.Run(name, func(t *testing.T) {
			registry := include.OutboundRegistry()
			var resourceContext context.Context
			outbound.Register[option.DirectOutboundOptions](registry, "context-probe", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.DirectOutboundOptions) (adapter.Outbound, error) {
				resourceContext = ctx
				return direct.NewOutbound(ctx, router, logger, tag, options)
			})
			ctx := box.Context(context.Background(), include.InboundRegistry(), registry, include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
			options := option.Options{Outbounds: []option.Outbound{{Type: "context-probe", Tag: "probe", Options: &option.DirectOutboundOptions{}}}}
			if fail {
				options.Outbounds = append(options.Outbounds, option.Outbound{Type: "missing-outbound", Tag: "invalid"})
			}
			instance, err := box.New(box.Options{Context: ctx, Options: options})
			if resourceContext == nil {
				t.Fatalf("probe was not constructed: %v", err)
			}
			if fail {
				if err == nil {
					instance.Close()
					t.Fatal("expected constructor failure")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err = resourceContext.Err(); err != nil {
					t.Fatalf("constructor canceled a usable instance: %v", err)
				}
				if err = instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if !errors.Is(resourceContext.Err(), context.Canceled) {
				t.Fatal("constructed resources retained a live context")
			}
		})
	}
}
