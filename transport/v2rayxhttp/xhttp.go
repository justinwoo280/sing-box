package v2rayxhttp

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	xhttp "github.com/justinwoo280/sing-xhttp/xhttp"
)

// NewServer / NewClient wrap sing-xhttp so the v2ray transport
// factory can treat xhttp like any other transport. The xhttp package
// intentionally has no sing-box dependency; this file is the only
// integration point.

func NewServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayXHTTPOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler) (adapter.V2RayServerTransport, error) {
	srv, err := xhttp.NewServer(ctx, logger, convertXHTTPOptions(options), tlsConfig, xhttpServerHandlerAdapter{handler})
	if err != nil {
		return nil, err
	}
	return xhttpServerAdapter{srv}, nil
}

func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	cli, err := xhttp.NewClient(ctx, dialer, serverAddr, convertXHTTPOptions(options), tlsConfig)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

// --- option conversion ---------------------------------------------------

func convertXHTTPOptions(o option.V2RayXHTTPOptions) xhttp.Options {
	return xhttp.Options{
		Mode:                 o.Mode,
		Host:                 o.Host,
		Path:                 o.Path,
		Method:               o.Method,
		Headers:              o.Headers,
		NoGRPCHeader:         o.NoGRPCHeader,
		NoSSEHeader:          o.NoSSEHeader,
		XPaddingBytes:        convertXHTTPRange(o.XPaddingBytes),
		ScMaxEachPostBytes:   convertXHTTPRange(o.ScMaxEachPostBytes),
		ScMaxBufferedPosts:   o.ScMaxBufferedPosts,
		ScMinPostsIntervalMs: convertXHTTPRange(o.ScMinPostsIntervalMs),
		ScStreamUpServerSecs: convertXHTTPRange(o.ScStreamUpServerSecs),
		XPaddingObfsMode:     o.XPaddingObfsMode,
		XPaddingPlacement:    o.XPaddingPlacement,
		XPaddingKey:          o.XPaddingKey,
		XPaddingHeader:       o.XPaddingHeader,
		XPaddingMethod:       o.XPaddingMethod,
		SessionPlacement:     o.SessionPlacement,
		SessionKey:           o.SessionKey,
		SeqPlacement:         o.SeqPlacement,
		SeqKey:               o.SeqKey,
		Xmux:                 convertXHTTPXmux(o.Xmux),
	}
}

func convertXHTTPRange(r *option.XHTTPRange) *xhttp.Range {
	if r == nil {
		return nil
	}
	return &xhttp.Range{From: r.From, To: r.To}
}

func convertXHTTPXmux(m *option.XHTTPXmuxConfig) *xhttp.XmuxConfig {
	if m == nil {
		return nil
	}
	return &xhttp.XmuxConfig{
		MaxConcurrency:   convertXHTTPRange(m.MaxConcurrency),
		MaxConnections:   convertXHTTPRange(m.MaxConnections),
		CMaxReuseTimes:   convertXHTTPRange(m.CMaxReuseTimes),
		HMaxRequestTimes: convertXHTTPRange(m.HMaxRequestTimes),
		HMaxReusableSecs: convertXHTTPRange(m.HMaxReusableSecs),
	}
}

// --- structural-typing adapters -----------------------------------------

// xhttp.ServerTransport and adapter.V2RayServerTransport are the same
// method set; this wrapper just gives go the static cast.
type xhttpServerAdapter struct {
	xhttp.ServerTransport
}

func (a xhttpServerAdapter) Network() []string                       { return a.ServerTransport.Network() }
func (a xhttpServerAdapter) Serve(l net.Listener) error              { return a.ServerTransport.Serve(l) }
func (a xhttpServerAdapter) ServePacket(l net.PacketConn) error      { return a.ServerTransport.ServePacket(l) }
func (a xhttpServerAdapter) Close() error                            { return a.ServerTransport.Close() }

// xhttpServerHandlerAdapter exposes adapter.V2RayServerTransportHandler
// as xhttp.ServerHandler (both are exactly N.TCPConnectionHandlerEx).
type xhttpServerHandlerAdapter struct {
	adapter.V2RayServerTransportHandler
}

var _ xhttp.ServerHandler = xhttpServerHandlerAdapter{}
