package ewp

import (
	"context"
	"encoding/hex"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/mux"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio/deadline"
	E "github.com/sagernet/sing/common/exceptions"
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
	userNames map[[sewp.UUIDLen]byte]string
	service   *sewp.ServiceV23
	tlsConfig tls.ServerConfig
	transport adapter.V2RayServerTransport
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.EWPInboundOptions) (adapter.Inbound, error) {
	in := &Inbound{
		Adapter:   inbound.NewAdapter(C.TypeEWP, tag),
		ctx:       ctx,
		router:    router,
		logger:    logger,
		users:     options.Users,
		userNames: make(map[[sewp.UUIDLen]byte]string, len(options.Users)),
	}
	if options.ServerID == "" {
		return nil, E.New("missing server_id")
	}
	service, err := sewp.NewServiceV23(&inboundHandler{owner: in}, options.SigningPrivateKey, options.ServerID, options.RouteEpoch)
	if err != nil {
		return nil, E.Cause(err, "create EWP/v2.3 service")
	}
	in.service = service
	initialized := false
	defer func() {
		if !initialized {
			_ = in.Close()
		}
	}()
	for i, user := range options.Users {
		if err := in.service.AddUser(user.UUID); err != nil {
			return nil, E.Cause(err, "user[", i, "] (", user.Name, ") UUID")
		}
		uuid, _ := sewp.ParseUUID(user.UUID)
		if _, exists := in.userNames[uuid]; !exists {
			name := user.Name
			if name == "" {
				name = hex.EncodeToString(uuid[:])
			}
			in.userNames[uuid] = name
		}
	}
	in.router, err = mux.NewRouterWithOptions(in.router, logger, common.PtrValueOrDefault(options.Multiplex))
	if err != nil {
		return nil, err
	}
	if options.TLS != nil {
		in.tlsConfig, err = tls.NewServer(ctx, logger, common.PtrValueOrDefault(options.TLS))
		if err != nil {
			return nil, err
		}
	}
	if options.Transport != nil {
		in.transport, err = v2ray.NewServerTransport(ctx, logger, common.PtrValueOrDefault(options.Transport), in.tlsConfig, (*inboundTransportHandler)(in))
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
	initialized = true
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
			if serveErr := h.transport.Serve(tcpListener); serveErr != nil && !E.IsClosed(serveErr) {
				h.logger.Error("transport serve error: ", serveErr)
			}
		}()
	}
	if common.Contains(h.transport.Network(), N.NetworkUDP) {
		udpConn, err := h.listener.ListenUDP()
		if err != nil {
			return err
		}
		go func() {
			if serveErr := h.transport.ServePacket(udpConn); serveErr != nil && !E.IsClosed(serveErr) {
				h.logger.Error("transport serve error: ", serveErr)
			}
		}()
	}
	return nil
}

func (h *Inbound) Close() error {
	return common.Close(common.PtrOrNil(h.listener), common.PtrOrNil(h.service), h.transport, h.tlsConfig)
}

func (h *Inbound) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if h.tlsConfig != nil && h.transport == nil {
		tlsConn, err := tls.ServerHandshake(ctx, conn, h.tlsConfig)
		if err != nil {
			N.CloseOnHandshakeFailure(conn, onClose, err)
			h.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", metadata.Source, ": TLS handshake"))
			return
		}
		conn = tlsConn
	}
	ctx = adapter.WithContext(ctx, &metadata)
	if deadline.NeedAdditionalReadDeadline(conn) {
		conn = deadline.NewConn(conn)
	}
	// Message transports such as gRPC expose a placeholder RemoteAddr. Keep
	// the listener's real source for cookies and per-source admission limits,
	// while framing the original connection so Flush remains available.
	underlying := conn
	if metadata.Source.IsValid() {
		underlying = &ewpSourceConn{Conn: conn, source: metadata.Source}
	}
	if err := h.service.HandleMessageTransport(ctx, sewp.NewLengthFramer(conn), underlying); err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		h.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", metadata.Source))
		return
	}
	if onClose != nil {
		onClose(nil)
	}
}

type ewpSourceConn struct {
	net.Conn
	source net.Addr
}

func (c *ewpSourceConn) RemoteAddr() net.Addr { return c.source }

func (h *Inbound) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	var metadata adapter.InboundContext
	metadata.Source = source
	metadata.Destination = destination
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	h.NewConnection(ctx, conn, metadata, onClose)
}

type inboundHandler struct{ owner *Inbound }

func (h *inboundHandler) NewConnection(ctx context.Context, conn net.Conn, md sewp.Metadata) error {
	parent := adapter.ContextFrom(ctx)
	metadata := h.owner.baseMetadata(parent)
	metadata.Destination = ewpToSocksaddr(md.Destination)
	metadata.User = h.owner.userName(md.UserUUID)
	h.owner.logger.InfoContext(ctx, "[", metadata.User, "] inbound connection to ", metadata.Destination)
	h.owner.router.RouteConnectionEx(ctx, conn, metadata, nil)
	return nil
}

func (h *inboundHandler) NewPacketConnection(ctx context.Context, pc net.PacketConn, md sewp.Metadata) error {
	parent := adapter.ContextFrom(ctx)
	metadata := h.owner.baseMetadata(parent)
	metadata.Destination = ewpToSocksaddr(md.Destination)
	metadata.User = h.owner.userName(md.UserUUID)
	h.owner.logger.InfoContext(ctx, "[", metadata.User, "] inbound packet connection to ", metadata.Destination)
	h.owner.router.RoutePacketConnectionEx(ctx, newEWPPacketConn(pc), metadata, nil)
	return nil
}

func (h *Inbound) baseMetadata(parent *adapter.InboundContext) adapter.InboundContext {
	var metadata adapter.InboundContext
	if parent != nil {
		metadata = *parent
	}
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	return metadata
}

func (h *Inbound) userName(uuid [sewp.UUIDLen]byte) string {
	if h.userNames != nil {
		if name, exists := h.userNames[uuid]; exists {
			return name
		}
		return hex.EncodeToString(uuid[:])
	}
	for _, user := range h.users {
		configured, err := sewp.ParseUUID(user.UUID)
		if err == nil && configured == uuid {
			if user.Name != "" {
				return user.Name
			}
			break
		}
	}
	return hex.EncodeToString(uuid[:])
}

var _ adapter.V2RayServerTransportHandler = (*inboundTransportHandler)(nil)

type inboundTransportHandler Inbound

func (h *inboundTransportHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	var metadata adapter.InboundContext
	metadata.Source = source
	metadata.Destination = destination
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	h.logger.InfoContext(ctx, "inbound connection from ", metadata.Source)
	(*Inbound)(h).NewConnection(ctx, conn, metadata, onClose)
}
