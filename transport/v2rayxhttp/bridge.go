package xhttp

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/justinwoo280/sing-xhttp/xhttp"
)

func NewServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayXHTTPOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler) (adapter.V2RayServerTransport, error) {
	if options.Browser {
		return nil, E.New("xhttp: browser is a client-only option")
	}
	if options.Download != nil {
		return nil, E.New("xhttp: download is a client-only option")
	}
	return xhttp.NewServer(ctx, logger, buildOptions(&options.V2RayXHTTPBaseOptions), tlsConfig, handler)
}

func NewClient(ctx context.Context, uploadDialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	return NewClientWithDialerOptions(ctx, logger.NOP(), uploadDialer, nil, serverAddr, options, tlsConfig)
}

// NewClientWithDialerOptions also receives the outbound's dialer policy, so
// the download can use the same resolver and bind settings even when only its
// endpoint is a domain.
func NewClientWithDialerOptions(ctx context.Context, logger logger.ContextLogger, uploadDialer N.Dialer, dialOptions *option.DialerOptions, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	if options.Browser {
		if options.Download != nil {
			return nil, E.New("xhttp: browser does not support separate download settings")
		}
		if field := ordinaryOnlyField(&options.V2RayXHTTPBaseOptions); field != "" {
			return nil, E.New("xhttp: browser does not support ", field)
		}
		return newBrowserClient(ctx, uploadDialer, serverAddr, options, tlsConfig)
	}
	opts := buildOptions(&options.V2RayXHTTPBaseOptions)
	dl := options.Download
	if dl == nil {
		return xhttp.NewClient(ctx, uploadDialer, serverAddr, opts, tlsConfig)
	}
	if opts.Mode == xhttp.ModeStreamOne {
		return nil, E.New("xhttp: stream-one does not support separate download settings")
	}
	separateServer := dl.Server != ""
	if separateServer != (dl.ServerPort != 0) {
		return nil, E.New("xhttp: download.server and download.server_port must be set together")
	}
	downloadAddr := serverAddr
	if separateServer {
		downloadAddr = M.ParseSocksaddrHostPort(dl.Server, dl.ServerPort)
		if !downloadAddr.IsValid() {
			return nil, E.New("xhttp: invalid download.server")
		}
	}
	separateDialer := dl.Detour != "" || dl.DomainResolver != nil || dl.DomainStrategy != option.DomainStrategy(C.DomainStrategyAsIS)
	downloadDialer := uploadDialer
	if separateDialer || dialOptions != nil && downloadAddr.IsDomain() && !serverAddr.IsDomain() {
		var effectiveDialOptions option.DialerOptions
		if dl.Detour != "" {
			// A different outbound owns its own socket settings and, unless
			// overridden, handles its own DNS. The tag is resolved on first use.
			effectiveDialOptions.Detour = dl.Detour
		} else if dialOptions != nil {
			effectiveDialOptions = *dialOptions
		}
		if dl.DomainResolver != nil {
			effectiveDialOptions.DomainResolver = dl.DomainResolver
		}
		if dl.DomainStrategy != option.DomainStrategy(C.DomainStrategyAsIS) {
			effectiveDialOptions.DomainStrategy = dl.DomainStrategy
		}
		var err error
		downloadDialer, err = dialer.NewWithOptions(dialer.Options{
			Context: ctx, Options: effectiveDialOptions, RemoteIsDomain: downloadAddr.IsDomain(),
			DisableEmptyDirectCheck: dl.Detour != "",
		})
		if err != nil {
			return nil, E.Cause(err, "xhttp: create download dialer")
		}
	}
	downloadTLS := tlsConfig
	if separateServer {
		// An explicitly configured server historically used plain HTTP with
		// no download.tls. Never copy the uplink SNI to a different server.
		downloadTLS = nil
	}
	if dl.TLS != nil {
		var err error
		downloadTLS, err = tls.NewClient(ctx, logger, downloadAddr.AddrString(), *dl.TLS)
		if err != nil {
			return nil, E.Cause(err, "xhttp: create download TLS config")
		}
	}
	opts.DownloadSettings = buildDownloadOptions(dl)
	if !separateServer && !separateDialer && dl.TLS == nil {
		return xhttp.NewClient(ctx, uploadDialer, serverAddr, opts, tlsConfig)
	}
	if !separateServer && opts.DownloadSettings.Host == "" {
		opts.DownloadSettings.Host = opts.Host
	}
	return xhttp.NewClientWithDownload(ctx, uploadDialer, serverAddr, opts, tlsConfig, downloadDialer, downloadAddr, downloadTLS)
}

func buildDownloadOptions(o *option.V2RayXHTTPDownloadOptions) *xhttp.DownloadConfig {
	options := &xhttp.DownloadConfig{
		Host: o.Host, Path: o.Path, Headers: o.Headers, Xmux: buildXmux(o.Xmux),
		XPaddingObfsMode: o.XPaddingObfsMode,
		XPaddingKey:      o.XPaddingKey,
		XPaddingHeader:   o.XPaddingHeader,
		XPaddingMethod:   o.XPaddingMethod,
		SessionPlacement: o.SessionPlacement,
		SessionKey:       o.SessionKey,
	}
	if o.XPaddingBytes != nil {
		// An explicit 0-0 overrides inherited padding with the library default.
		options.XPaddingBytes = &xhttp.Range{From: o.XPaddingBytes.From, To: o.XPaddingBytes.To}
	}
	if o.XPaddingPlacement != nil {
		placement := paddingPlacement(*o.XPaddingPlacement)
		options.XPaddingPlacement = &placement
	}
	return options
}

func buildOptions(o *option.V2RayXHTTPBaseOptions) xhttp.Options {
	return xhttp.Options{
		Mode:                 o.Mode,
		Host:                 o.Host,
		Path:                 o.Path,
		Method:               o.UplinkHTTPMethod,
		Headers:              o.Headers,
		NoGRPCHeader:         o.NoGRPCHeader,
		NoSSEHeader:          o.NoSSEHeader,
		GRPCFraming:          o.GRPCFraming,
		XPaddingBytes:        rangePtr(o.XPaddingBytes),
		ScMaxEachPostBytes:   rangePtr(o.ScMaxEachPostBytes),
		ScMinPostsIntervalMs: rangePtr(o.ScMinPostsIntervalMs),
		ScMaxBufferedPosts:   o.ScMaxBufferedPosts,
		ScStreamUpServerSecs: rangePtr(o.ScStreamUpServerSecs),
		Xmux:                 buildXmux(o.Xmux),

		XPaddingObfsMode:  o.XPaddingObfsMode,
		XPaddingPlacement: paddingPlacement(o.XPaddingPlacement),
		XPaddingKey:       o.XPaddingKey,
		XPaddingHeader:    o.XPaddingHeader,
		XPaddingMethod:    o.XPaddingMethod,

		SessionPlacement: o.SessionPlacement,
		SessionKey:       o.SessionKey,
		SeqPlacement:     o.SeqPlacement,
		SeqKey:           o.SeqKey,
		SessionIDTable:   o.SessionIDTable,
		SessionIDLength:  rangePtr(o.SessionIDLength),

		UplinkDataPlacement:  o.UplinkDataPlacement,
		UplinkDataKey:        o.UplinkDataKey,
		UplinkChunkSize:      rangePtr(o.UplinkChunkSize),
		ServerMaxHeaderBytes: o.ServerMaxHeaderBytes,
	}
}

func buildXmux(o *option.V2RayXHTTPXmuxOptions) *xhttp.XmuxConfig {
	if o == nil {
		return nil
	}
	return &xhttp.XmuxConfig{
		MaxConcurrency:   rangePtr(o.MaxConcurrency),
		MaxConnections:   rangePtr(o.MaxConnections),
		CMaxReuseTimes:   rangePtr(o.CMaxReuseTimes),
		HMaxRequestTimes: rangePtr(o.HMaxRequestTimes),
		HMaxReusableSecs: rangePtr(o.HMaxReusableSecs),
		HKeepAlivePeriod: o.HKeepAlivePeriod,
	}
}

func rangePtr(r option.V2RayXHTTPRange) *xhttp.Range {
	if r.From == 0 && r.To == 0 {
		return nil
	}
	return &xhttp.Range{From: r.From, To: r.To}
}

func paddingPlacement(placement string) string {
	if placement == "queryInHeader" {
		return xhttp.PlacementQueryInHeader
	}
	return placement
}

func ordinaryOnlyField(o *option.V2RayXHTTPBaseOptions) string {
	for _, field := range []struct {
		name string
		set  bool
	}{
		{"uplink_http_method", o.UplinkHTTPMethod != ""},
		{"x_padding_obfs_mode", o.XPaddingObfsMode},
		{"x_padding_placement", o.XPaddingPlacement != ""},
		{"x_padding_key", o.XPaddingKey != ""},
		{"x_padding_header", o.XPaddingHeader != ""},
		{"x_padding_method", o.XPaddingMethod != ""},
		{"session_placement", o.SessionPlacement != ""},
		{"session_key", o.SessionKey != ""},
		{"seq_placement", o.SeqPlacement != ""},
		{"seq_key", o.SeqKey != ""},
		{"session_id_table", o.SessionIDTable != ""},
		{"session_id_length", o.SessionIDLength != (option.V2RayXHTTPRange{})},
		{"uplink_data_placement", o.UplinkDataPlacement != ""},
		{"uplink_data_key", o.UplinkDataKey != ""},
		{"uplink_chunk_size", o.UplinkChunkSize != (option.V2RayXHTTPRange{})},
		{"server_max_header_bytes", o.ServerMaxHeaderBytes != 0},
	} {
		if field.set {
			return field.name
		}
	}
	return ""
}
