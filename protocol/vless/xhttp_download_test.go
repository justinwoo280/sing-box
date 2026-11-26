package vless

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/option"
	SJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"

	"github.com/stretchr/testify/require"
)

func TestVLESSXHTTPDownloadDependencies(t *testing.T) {
	const outboundJSON = `{
		"type":"vless", "tag":"client", "server":"127.0.0.1", "server_port":10001,
		"uuid":"87dd5b75-aaf3-46a7-b190-ecccc73cd4b8", "transport":{
			"type":"xhttp", "headers":{"Authorization":"loopback-only"}, "xmux":{"max_concurrency":"2-9"},
			"download":{"detour":"later","headers":{},"xmux":{},"tls":{"enabled":false}}
		}
	}`
	const laterOutbound = `,{
		"type":"vless", "tag":"later", "server":"127.0.0.1", "server_port":10002,
		"uuid":"439b7fc2-6f56-4d9a-9355-2efae947ed47"`
	for _, test := range []struct{ name, tail, error string }{
		{"forward reference", laterOutbound + `}`, ""},
		{"missing tag", "", "dependency[later] not found"},
		{"circular dependency", laterOutbound + `,"detour":"client"}`, "circular outbound dependency"},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := logger.NOP()
			registry := outbound.NewRegistry()
			RegisterOutbound(registry)
			manager := outbound.NewManager(registry, endpoint.NewManager(nil), "client")
			scope := adapter.NewScope(context.Background(), log)
			defer scope.Close()
			ctx := service.ContextWith[option.OutboundOptionsRegistry](context.Background(), registry)
			ctx = service.ContextWith[adapter.OutboundManager](ctx, manager)

			configuration := []byte(`{"outbounds":[` + outboundJSON + test.tail + `]}`)
			config, err := SJSON.UnmarshalExtendedContext[option.Options](ctx, configuration)
			require.NoError(t, err)
			encoded, err := SJSON.MarshalContext(ctx, config)
			require.NoError(t, err)
			// Exercise the same path as sing-box format -> check, not only
			// unmarshaling the inner transport object in isolation.
			require.True(t, json.Valid(encoded))
			config, err = SJSON.UnmarshalExtendedContext[option.Options](ctx, encoded)
			require.NoError(t, err)
			download := config.Outbounds[0].Options.(*option.VLESSOutboundOptions).Transport.XHTTPOptions.Download
			require.NotNil(t, download.Headers)
			require.Empty(t, download.Headers)
			require.NotNil(t, download.Xmux)
			require.NotNil(t, download.TLS)
			require.False(t, download.TLS.Enabled)
			for _, entry := range config.Outbounds {
				require.NoError(t, manager.Create(ctx, nil, log, entry.Tag, entry.Type, entry.Options))
			}
			client, ok := manager.Outbound("client")
			require.True(t, ok)
			require.Equal(t, []string{"later"}, client.Dependencies())
			err = manager.Initialize(nil)
			if test.error == "" {
				require.NoError(t, err)
				require.NoError(t, manager.Start(adapter.StartStateInitialize, scope))
				require.NoError(t, manager.Start(adapter.StartStateStart, scope))
			} else {
				require.ErrorContains(t, err, test.error)
			}
		})
	}
}
