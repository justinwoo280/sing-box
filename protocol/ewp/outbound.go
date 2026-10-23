package ewp

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/mux"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/bufio/deadline"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.EWPOutboundOptions](registry, C.TypeEWP, NewOutbound)
}

var _ adapter.InterfaceUpdateListener = (*Outbound)(nil)

type Outbound struct {
	outbound.Adapter
	logger          logger.ContextLogger
	dialer          N.Dialer
	client          ewpClient
	serverAddr      M.Socksaddr
	tlsConfig       tls.Config
	transport       adapter.V2RayClientTransport
	multiplexDialer *mux.Client
}

func newOutboundTLS(ctx context.Context, logger logger.ContextLogger, options option.EWPOutboundOptions) (tls.Config, error) {
	return tls.NewClientWithOptions(tls.ClientOptions{
		Context:       ctx,
		Logger:        logger,
		ServerAddress: options.Server,
		Options:       common.PtrValueOrDefault(options.TLS),
		Browser:       options.Transport.IsBrowserXHTTP(),
	})
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.EWPOutboundOptions) (adapter.Outbound, error) {
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	o := &Outbound{
		Adapter:    outbound.NewAdapterWithDialerOptions(C.TypeEWP, tag, options.Network.Build(), options.DialerOptions),
		logger:     logger,
		dialer:     outboundDialer,
		serverAddr: options.ServerOptions.Build(),
	}
	if options.TLS != nil {
		o.tlsConfig, err = newOutboundTLS(ctx, logger, options)
		if err != nil {
			return nil, err
		}
	}
	if options.Transport != nil {
		o.transport, err = v2ray.NewClientTransport(ctx, o.dialer, o.serverAddr, common.PtrValueOrDefault(options.Transport), o.tlsConfig)
		if err != nil {
			return nil, E.Cause(err, "create client transport: ", options.Transport.Type)
		}
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = o.Close()
		}
	}()
	o.client, err = sewp.NewClientV23WithOptions(options.UUID, options.ServerID, options.ServerPublicKey, options.RouteEpoch, sewp.V23ClientOptions{
		EnableEarlyServerFinished: options.EarlyServerFinished,
	})
	if err != nil {
		return nil, E.Cause(err, "parse EWP/v2.3 client config")
	}
	o.client.SetTicketStore(sewp.NewMemoryV23TicketStore())
	o.multiplexDialer, err = mux.NewClientWithOptions((*ewpDialer)(o), logger, common.PtrValueOrDefault(options.Multiplex))
	if err != nil {
		return nil, err
	}
	initialized = true
	return o, nil
}

func (h *Outbound) dialUnderlying(ctx context.Context) (net.Conn, error) {
	var conn net.Conn
	if h.transport != nil {
		var err error
		conn, err = h.transport.DialContext(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		conn, err = h.dialer.DialContext(ctx, N.NetworkTCP, h.serverAddr)
		if err != nil {
			return nil, err
		}
		if h.tlsConfig != nil {
			tlsConn, err := tls.ClientHandshake(ctx, conn, h.tlsConfig)
			if err != nil {
				conn.Close()
				return nil, err
			}
			conn = tlsConn
		}
	}
	if deadline.NeedAdditionalReadDeadline(conn) {
		conn = deadline.NewConn(conn)
	}
	return conn, nil
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if h.multiplexDialer != nil {
		return h.multiplexDialer.DialContext(ctx, network, destination)
	}
	return (*ewpDialer)(h).DialContext(ctx, network, destination)
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if h.multiplexDialer != nil {
		return h.multiplexDialer.ListenPacket(ctx, destination)
	}
	return (*ewpDialer)(h).ListenPacket(ctx, destination)
}

func (h *Outbound) MultiplexEnabled() bool {
	return h.multiplexDialer != nil
}

type ewpDialer Outbound

func (h *ewpDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		h.logger.InfoContext(ctx, "outbound connection to ", destination)
		raw, err := (*Outbound)(h).dialUnderlying(ctx)
		if err != nil {
			return nil, err
		}
		hsCtx, cancel := context.WithTimeout(ctx, ewpHandshakeTimeout)
		defer cancel()
		conn, err := h.client.DialConn(hsCtx, raw, socksaddrToEWP(destination))
		if err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	case N.NetworkUDP:
		h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
		raw, err := (*Outbound)(h).dialUnderlying(ctx)
		if err != nil {
			return nil, err
		}
		hsCtx, cancel := context.WithTimeout(ctx, ewpHandshakeTimeout)
		defer cancel()
		pc, err := h.client.DialPacketConn(hsCtx, raw, socksaddrToEWP(destination))
		if err != nil {
			raw.Close()
			return nil, err
		}
		return bufio.NewBindPacketConn(newEWPPacketConn(pc), destination), nil
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
}

func (h *ewpDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
	raw, err := (*Outbound)(h).dialUnderlying(ctx)
	if err != nil {
		return nil, err
	}
	hsCtx, cancel := context.WithTimeout(ctx, ewpHandshakeTimeout)
	defer cancel()
	pc, err := h.client.DialPacketConn(hsCtx, raw, socksaddrToEWP(destination))
	if err != nil {
		raw.Close()
		return nil, err
	}
	return newEWPPacketConn(pc), nil
}

func (h *Outbound) InterfaceUpdated(ctx context.Context) {
	if h.transport != nil {
		_ = v2ray.ResetClientTransport(h.transport)
	}
	if h.multiplexDialer != nil {
		h.multiplexDialer.Reset()
	}
}

func (h *Outbound) Close() error {
	return common.Close(common.PtrOrNil(h.multiplexDialer), h.transport)
}
