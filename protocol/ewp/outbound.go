package ewp

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

// RegisterOutbound is the registry hook called by include/registry.go.
func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.EWPOutboundOptions](registry, C.TypeEWP, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	logger     logger.ContextLogger
	dialer     N.Dialer
	client     ewpClient
	serverAddr M.Socksaddr
	tlsConfig  tls.Config
	transport  adapter.V2RayClientTransport
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger,
	tag string, options option.EWPOutboundOptions,
) (adapter.Outbound, error) {
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	o := &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(C.TypeEWP, tag,
			options.Network.Build(), options.DialerOptions),
		logger:     logger,
		dialer:     outboundDialer,
		serverAddr: options.ServerOptions.Build(),
	}
	if options.TLS != nil {
		o.tlsConfig, err = tls.NewClient(ctx, logger, options.Server, common.PtrValueOrDefault(options.TLS))
		if err != nil {
			return nil, err
		}
	}
	if options.Transport != nil {
		o.transport, err = v2ray.NewClientTransport(ctx, o.dialer, o.serverAddr,
			common.PtrValueOrDefault(options.Transport), o.tlsConfig)
		if err != nil {
			return nil, E.Cause(err, "create client transport: ", options.Transport.Type)
		}
	}
	if options.ServerStaticPublicKey != "" {
		o.client, err = sewp.NewClientV21(options.UUID, options.ServerStaticPublicKey)
		if err != nil {
			return nil, E.Cause(err, "parse EWP/v2.1 client config")
		}
	} else {
		// Legacy v2.0 (no server identity binding); the v2.1 server
		// will reject this. Kept for backwards compatibility with
		// existing v2.0 deployments only — new deployments SHOULD
		// configure server_static_public_key.
		o.client, err = sewp.NewClient(options.UUID)
		if err != nil {
			return nil, E.Cause(err, "parse EWP UUID")
		}
	}
	return o, nil
}

// dialUnderlying establishes the byte-stream channel that EWP runs on
// top of: optionally going through a v2ray transport (ws/grpc/...) and
// optionally wrapping in TLS.
func (h *Outbound) dialUnderlying(ctx context.Context) (net.Conn, error) {
	if h.transport != nil {
		return h.transport.DialContext(ctx)
	}
	conn, err := h.dialer.DialContext(ctx, N.NetworkTCP, h.serverAddr)
	if err != nil {
		return nil, err
	}
	if h.tlsConfig != nil {
		tlsConn, err := tls.ClientHandshake(ctx, conn, h.tlsConfig)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
	return conn, nil
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination

	switch N.NetworkName(network) {
	case N.NetworkTCP:
		h.logger.InfoContext(ctx, "outbound connection to ", destination)
		raw, err := h.dialUnderlying(ctx)
		if err != nil {
			return nil, err
		}
		conn, err := h.client.DialConn(ctx, raw, socksaddrToEWP(destination))
		if err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	case N.NetworkUDP:
		h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
		raw, err := h.dialUnderlying(ctx)
		if err != nil {
			return nil, err
		}
		pc, err := h.client.DialPacketConn(ctx, raw, socksaddrToEWP(destination))
		if err != nil {
			raw.Close()
			return nil, err
		}
		// Bind so the returned net.Conn writes to a fixed destination,
		// matching sing-box's expectations on UDP DialContext.
		return bufio.NewBindPacketConn(pc, destination), nil
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "outbound packet connection to ", destination)

	raw, err := h.dialUnderlying(ctx)
	if err != nil {
		return nil, err
	}
	pc, err := h.client.DialPacketConn(ctx, raw, socksaddrToEWP(destination))
	if err != nil {
		raw.Close()
		return nil, err
	}
	return pc, nil
}

func (h *Outbound) InterfaceUpdated() {
	if h.transport != nil {
		h.transport.Close()
	}
}

func (h *Outbound) Close() error {
	return common.Close(h.transport)
}
