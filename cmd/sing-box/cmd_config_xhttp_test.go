package main

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badjson"

	"github.com/stretchr/testify/require"
)

func TestXHTTPDownloadOverridesSurviveConfigCommands(t *testing.T) {
	previousContext := globalCtx
	globalCtx = include.Context(context.Background())
	t.Cleanup(func() { globalCtx = previousContext })
	input, err := json.UnmarshalExtendedContext[option.Options](globalCtx, []byte(`{"outbounds":[
		{"type":"vless","server":"127.0.0.1","server_port":80,"transport":{"type":"xhttp","download":{}}},
		{"type":"trojan","server":"127.0.0.1","server_port":80,"transport":{"type":"xhttp","download":{
			"headers":{}, "tls":{"enabled":false}
		}}}
	]}`))
	require.NoError(t, err)
	assertDownloads := func(t *testing.T, config option.Options) {
		t.Helper()
		require.NotNil(t, outboundXHTTPOptions(config.Outbounds[0]).Download)
		download := outboundXHTTPOptions(config.Outbounds[1]).Download
		require.NotNil(t, download.Headers)
		require.Empty(t, download.Headers)
		require.NotNil(t, download.TLS)
		require.False(t, download.TLS.Enabled)
	}
	t.Run("format", func(t *testing.T) {
		formatted, err := badjson.Omitempty(globalCtx, input)
		require.NoError(t, err)
		preserveXHTTPDownloads(formatted.Outbounds, input.Outbounds)
		assertDownloads(t, formatted)
	})
	t.Run("merge", func(t *testing.T) {
		more, err := json.UnmarshalExtendedContext[option.Options](globalCtx, []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`))
		require.NoError(t, err)
		merged, err := mergeOptionsList([]*OptionsEntry{{options: input}, {options: more}})
		require.NoError(t, err)
		require.Len(t, merged.Outbounds, 3)
		assertDownloads(t, merged)
	})
}
