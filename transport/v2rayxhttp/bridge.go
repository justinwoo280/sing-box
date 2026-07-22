// Package xhttp bridges sing-box's V2Ray transport layer to the standalone
// sing-xhttp library. sing-box's option.V2RayXHTTPOptions is mapped onto
// xhttp.Options and the concrete sing-box tls / dialer / handler types satisfy
// the sing-xhttp interfaces by structural typing.
package xhttp

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	Xbadoption "github.com/sagernet/sing-box/common/xray/json/badoption"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"

	"github.com/justinwoo280/sing-xhttp/xhttp"
)

// NewServer builds an XHTTP server transport backed by sing-xhttp.
func NewServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayXHTTPOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler) (adapter.V2RayServerTransport, error) {
	return xhttp.NewServer(ctx, logger, buildOptions(&options.V2RayXHTTPBaseOptions), tlsConfig, handler)
}

// NewClient builds an XHTTP client transport backed by sing-xhttp. When the
// options carry a separate download transport (with its own detour/TLS/server),
// it wires that through xhttp.NewClientWithDownload.
func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	opts := buildOptions(&options.V2RayXHTTPBaseOptions)

	if options.Download == nil {
		return xhttp.NewClient(ctx, dialer, serverAddr, opts, tlsConfig)
	}

	// Separate download transport: resolve its dialer (detour), dest and TLS.
	dl := options.Download
	downloadDialer := dialer
	if dl.Detour != "" {
		outboundManager := service.FromContext[adapter.OutboundManager](ctx)
		if outboundManager == nil {
			return nil, E.New("outbound manager not found for download detour")
		}
		detour, loaded := outboundManager.Outbound(dl.Detour)
		if !loaded {
			return nil, E.New("download detour outbound not found: ", dl.Detour)
		}
		downloadDialer = detour
	}
	downloadAddr := dl.ServerOptions.Build()

	var downloadTLS tls.Config
	if dl.TLS != nil && dl.TLS.Enabled {
		var err error
		downloadTLS, err = tls.NewClient(ctx, contextLogger(ctx), dl.Server, common.PtrValueOrDefault(dl.TLS))
		if err != nil {
			return nil, E.Cause(err, "create download TLS config")
		}
	}

	opts.DownloadSettings = &xhttp.DownloadConfig{
		Host: dl.Host,
		Path: dl.Path,
	}

	return xhttp.NewClientWithDownload(ctx, dialer, serverAddr, opts, tlsConfig, downloadDialer, downloadAddr, downloadTLS)
}

func contextLogger(ctx context.Context) logger.ContextLogger {
	return service.FromContext[logger.ContextLogger](ctx)
}

// buildOptions maps sing-box's V2RayXHTTPBaseOptions onto xhttp.Options.
func buildOptions(o *option.V2RayXHTTPBaseOptions) xhttp.Options {
	opts := xhttp.Options{
		Mode:                 o.Mode,
		Host:                 o.Host,
		Path:                 o.Path,
		Headers:              headerToOption(o.Headers),
		NoGRPCHeader:         o.NoGRPCHeader,
		NoSSEHeader:          o.NoSSEHeader,
		XPaddingBytes:        rangePtr(o.XPaddingBytes),
		ScMaxEachPostBytes:   rangePtr(o.ScMaxEachPostBytes),
		ScMinPostsIntervalMs: rangePtr(o.ScMinPostsIntervalMs),
		ScMaxBufferedPosts:   int32(o.ScMaxBufferedPosts),
		ScStreamUpServerSecs: rangePtr(o.ScStreamUpServerSecs),
	}
	if o.Xmux != nil {
		opts.Xmux = &xhttp.XmuxConfig{
			MaxConcurrency:   rangePtr(o.Xmux.MaxConcurrency),
			MaxConnections:   rangePtr(o.Xmux.MaxConnections),
			CMaxReuseTimes:   rangePtr(o.Xmux.CMaxReuseTimes),
			HMaxRequestTimes: rangePtr(o.Xmux.HMaxRequestTimes),
			HMaxReusableSecs: rangePtr(o.Xmux.HMaxReusableSecs),
			HKeepAlivePeriod: int32(o.Xmux.HKeepAlivePeriod),
		}
	}
	return opts
}

// rangePtr converts a sing-box Xbadoption.Range value to a sing-xhttp *Range.
// A zero range ({0,0}) maps to nil so sing-xhttp defaults apply.
func rangePtr(r Xbadoption.Range) *xhttp.Range {
	if r.From == 0 && r.To == 0 {
		return nil
	}
	return &xhttp.Range{From: r.From, To: r.To}
}

// headerToOption converts a plain header map to sing's badoption.HTTPHeader.
func headerToOption(h map[string]string) badoption.HTTPHeader {
	if len(h) == 0 {
		return nil
	}
	out := make(badoption.HTTPHeader, len(h))
	for k, v := range h {
		out[k] = badoption.Listable[string]{v}
	}
	return out
}
