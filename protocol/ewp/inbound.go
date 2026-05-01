package ewp

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[option.EWPInboundOptions](registry, C.TypeEWP, NewInbound)
}

var _ adapter.TCPInjectableInbound = (*Inbound)(nil)

type Inbound struct {
	inbound.Adapter
	ctx       context.Context
	router    adapter.ConnectionRouterEx
	logger    logger.ContextLogger
	listener  *listener.Listener
	users     []option.EWPUser
	service   *sewp.Service
	tlsConfig tls.ServerConfig
	transport adapter.V2RayServerTransport
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger,
	tag string, options option.EWPInboundOptions) (adapter.Inbound, error) {

	in := &Inbound{
		Adapter: inbound.NewAdapter(C.TypeEWP, tag),
		ctx:     ctx,
		router:  router,
		logger:  logger,
		users:   options.Users,
	}
	// Build the EWP v2 service and register all configured users.
	in.service = sewp.NewService(&inboundHandler{owner: in})
	for i, u := range options.Users {
		if err := in.service.AddUser(u.UUID); err != nil {
			return nil, E.Cause(err, "user[", i, "] (", u.Name, ") UUID")
		}
	}

	var err error
	if options.TLS != nil {
		in.tlsConfig, err = tls.NewServer(ctx, logger, common.PtrValueOrDefault(options.TLS))
		if err != nil {
			return nil, err
		}
	}
	if options.Transport != nil {
		in.transport, err = v2ray.NewServerTransport(ctx, logger,
			common.PtrValueOrDefault(options.Transport),
			in.tlsConfig, (*inboundTransportHandler)(in))
		if err != nil {
			return nil, E.Cause(err, "create server transport: ", options.Transport.Type)
		}
	}

	in.listener = listener.New(listener.Options{
		Context:           ctx,
		Logger:            logger,
		Network:           []string{N.NetworkTCP},
		Listen:            options.ListenOptions,
		ConnectionHandler: in,
	})
	return in, nil
}

func (h *Inbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if h.tlsConfig != nil {
		if err := h.tlsConfig.Start(); err != nil {
			return err
		}
	}
	if h.transport == nil {
		return h.listener.Start()
	}
	if common.Contains(h.transport.Network(), N.NetworkTCP) {
		tcpListener, err := h.listener.ListenTCP()
		if err != nil {
			return err
		}
		go func() {
			if sErr := h.transport.Serve(tcpListener); sErr != nil && !E.IsClosed(sErr) {
				h.logger.Error("transport serve error: ", sErr)
			}
		}()
	}
	if common.Contains(h.transport.Network(), N.NetworkUDP) {
		udpConn, err := h.listener.ListenUDP()
		if err != nil {
			return err
		}
		go func() {
			if sErr := h.transport.ServePacket(udpConn); sErr != nil && !E.IsClosed(sErr) {
				h.logger.Error("transport serve error: ", sErr)
			}
		}()
	}
	return nil
}

func (h *Inbound) Close() error {
	return common.Close(
		h.listener,
		h.tlsConfig,
		h.transport,
	)
}

// NewConnectionEx is invoked by the listener for raw incoming
// connections. We perform TLS termination (if no v2ray transport is
// configured) then hand off to the EWP service for handshake +
// dispatch.
func (h *Inbound) NewConnectionEx(ctx context.Context, conn net.Conn,
	metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {

	if h.tlsConfig != nil && h.transport == nil {
		tlsConn, err := tls.ServerHandshake(ctx, conn, h.tlsConfig)
		if err != nil {
			N.CloseOnHandshakeFailure(conn, onClose, err)
			h.logger.ErrorContext(ctx,
				E.Cause(err, "process connection from ", metadata.Source, ": TLS handshake"))
			return
		}
		conn = tlsConn
	}
	// Stash the inbound metadata in ctx so the EWP handler can read
	// it back when dispatching to the router.
	ctx = adapter.WithContext(ctx, &metadata)
	if err := h.service.HandleConn(ctx, conn); err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		h.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", metadata.Source))
		return
	}
	if onClose != nil {
		onClose(nil)
	}
}

// ----------------------------------------------------------------------
// inboundHandler implements sewp.Handler and bridges into sing-box's
// router. It is owned by the parent Inbound; we use a back-pointer so
// per-flow handlers see live router/logger references.
// ----------------------------------------------------------------------

type inboundHandler struct {
	owner *Inbound
}

func (h *inboundHandler) NewConnection(ctx context.Context, conn net.Conn, md sewp.Metadata) error {
	parentMD := adapter.ContextFrom(ctx)
	metadata := h.owner.baseMetadata(parentMD)
	metadata.Destination = ewpToSocksaddr(md.Destination)
	metadata.User = h.owner.userName(md.UserUUID)
	h.owner.logger.InfoContext(ctx, "[", metadata.User, "] inbound connection to ", metadata.Destination)
	h.owner.router.RouteConnectionEx(ctx, conn, metadata, nil)
	return nil
}

func (h *inboundHandler) NewPacketConnection(ctx context.Context, pc net.PacketConn, md sewp.Metadata) error {
	parentMD := adapter.ContextFrom(ctx)
	metadata := h.owner.baseMetadata(parentMD)
	metadata.Destination = ewpToSocksaddr(md.Destination)
	metadata.User = h.owner.userName(md.UserUUID)
	h.owner.logger.InfoContext(ctx, "[", metadata.User, "] inbound packet connection to ", metadata.Destination)
	h.owner.router.RoutePacketConnectionEx(ctx, bufio.NewPacketConn(pc), metadata, nil)
	return nil
}

// baseMetadata fills in inbound tag/type and copies any pre-existing
// fields (Source, InboundDetour, InboundOptions) propagated from the
// listener.
func (in *Inbound) baseMetadata(parent *adapter.InboundContext) adapter.InboundContext {
	var md adapter.InboundContext
	if parent != nil {
		md = *parent
	}
	md.Inbound = in.Tag()
	md.InboundType = in.Type()
	return md
}

// userName returns the configured Name for the given UUID, falling back
// to a hex digest of the UUID if no Name was set.
func (in *Inbound) userName(uuid [sewp.UUIDLen]byte) string {
	for _, u := range in.users {
		uu, err := sewp.ParseUUID(u.UUID)
		if err == nil && uu == uuid {
			if u.Name != "" {
				return u.Name
			}
			return F.ToString(uuid[:])
		}
	}
	return F.ToString(uuid[:])
}

// ----------------------------------------------------------------------
// inboundTransportHandler glues a v2ray ServerTransport (ws/grpc/...)
// back to the same Inbound handshake path.
// ----------------------------------------------------------------------

var _ adapter.V2RayServerTransportHandler = (*inboundTransportHandler)(nil)

type inboundTransportHandler Inbound

func (h *inboundTransportHandler) NewConnectionEx(ctx context.Context, conn net.Conn,
	source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	var metadata adapter.InboundContext
	metadata.Source = source
	metadata.Destination = destination
	//nolint:staticcheck
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	//nolint:staticcheck
	metadata.InboundOptions = h.listener.ListenOptions().InboundOptions
	h.logger.InfoContext(ctx, "inbound connection from ", metadata.Source)
	(*Inbound)(h).NewConnectionEx(ctx, conn, metadata, onClose)
}
