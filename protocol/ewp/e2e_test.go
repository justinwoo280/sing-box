package ewp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

// fakeRouter captures the metadata + conn / pc that the EWP inbound
// handler dispatches via RouteConnectionEx, so the test can assert on
// them without depending on the rest of sing-box.
type fakeRouter struct {
	mu     sync.Mutex
	conns  []routedConn
	pkts   []routedPkt
	doneCh chan struct{}
}

type routedConn struct {
	conn net.Conn
	md   adapter.InboundContext
}

type routedPkt struct {
	pc N.PacketConn
	md adapter.InboundContext
}

func newFakeRouter() *fakeRouter {
	return &fakeRouter{doneCh: make(chan struct{}, 8)}
}

// Legacy ConnectionRouter methods (deprecated but still part of the
// interface). The EWP inbound never invokes them; satisfy them with a
// trivial implementation so the test fakeRouter type-checks.
func (r *fakeRouter) RouteConnection(ctx context.Context, conn net.Conn, md adapter.InboundContext) error {
	return nil
}
func (r *fakeRouter) RoutePacketConnection(ctx context.Context, pc N.PacketConn, md adapter.InboundContext) error {
	return nil
}

func (r *fakeRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, md adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.mu.Lock()
	r.conns = append(r.conns, routedConn{conn: conn, md: md})
	r.mu.Unlock()
	r.doneCh <- struct{}{}
}

func (r *fakeRouter) RoutePacketConnectionEx(ctx context.Context, pc N.PacketConn, md adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.mu.Lock()
	r.pkts = append(r.pkts, routedPkt{pc: pc, md: md})
	r.mu.Unlock()
	r.doneCh <- struct{}{}
}

// makeInbound builds a minimal *Inbound suitable for driving the
// inboundHandler bridge. We do NOT call NewInbound: that would require
// a full sing-box context (listener, registry...) which is far beyond
// the scope of these tests. Instead we hand-stitch the fields the
// handler actually reads.
func makeInbound(t *testing.T, router adapter.ConnectionRouterEx, uuid string) *Inbound {
	t.Helper()
	in := &Inbound{
		router: router,
		users:  []option.EWPUser{{Name: "alice", UUID: uuid}},
		logger: nopLogger{},
	}
	in.service = sewp.NewService(&inboundHandler{owner: in})
	if err := in.service.AddUser(uuid); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	return in
}

// nopLogger is a logger.ContextLogger satisfied by ignoring everything.
type nopLogger struct{}

func (nopLogger) Trace(args ...any)                                  {}
func (nopLogger) Debug(args ...any)                                  {}
func (nopLogger) Info(args ...any)                                   {}
func (nopLogger) Warn(args ...any)                                   {}
func (nopLogger) Error(args ...any)                                  {}
func (nopLogger) Fatal(args ...any)                                  {}
func (nopLogger) Panic(args ...any)                                  {}
func (nopLogger) TraceContext(ctx context.Context, args ...any)      {}
func (nopLogger) DebugContext(ctx context.Context, args ...any)      {}
func (nopLogger) InfoContext(ctx context.Context, args ...any)       {}
func (nopLogger) WarnContext(ctx context.Context, args ...any)       {}
func (nopLogger) ErrorContext(ctx context.Context, args ...any)      {}
func (nopLogger) FatalContext(ctx context.Context, args ...any)      {}
func (nopLogger) PanicContext(ctx context.Context, args ...any)      {}

// ----------------------------------------------------------------------
// TCP end-to-end via net.Pipe + Inbound.NewConnectionEx
// ----------------------------------------------------------------------

func TestEWP_EndToEnd_TCP(t *testing.T) {
	const uuid = "11111111-2222-3333-4444-555555555555"

	clientPipe, serverPipe := net.Pipe()
	router := newFakeRouter()
	in := makeInbound(t, router, uuid)

	// Server side: invoke the inbound handler exactly as the listener
	// would (no TLS — net.Pipe is already a clear byte channel).
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		var md adapter.InboundContext
		md.Source = M.SocksaddrFromNetIP(netip.MustParseAddrPort("127.0.0.1:1234"))
		in.NewConnectionEx(context.Background(), serverPipe, md, nil)
	}()

	// Client side: run the EWP client handshake exactly as
	// Outbound.DialContext would.
	client, err := sewp.NewClient(uuid)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	dst := sewp.Address{Addr: netip.MustParseAddrPort("8.8.8.8:443")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientConn, err := client.DialConn(ctx, clientPipe, dst)
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}

	// Wait for the inbound to dispatch into the router.
	select {
	case <-router.doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("router.RouteConnectionEx never invoked")
	}

	router.mu.Lock()
	if len(router.conns) != 1 {
		router.mu.Unlock()
		t.Fatalf("want 1 routed conn, got %d", len(router.conns))
	}
	got := router.conns[0]
	router.mu.Unlock()
	if got.md.Destination.Addr != dst.Addr.Addr() || got.md.Destination.Port != dst.Addr.Port() {
		t.Errorf("Destination mismatch: got %v want %v", got.md.Destination, dst.Addr)
	}
	if got.md.User != "alice" {
		t.Errorf("User mismatch: got %q want %q", got.md.User, "alice")
	}

	// Echo loop: the routed conn pretends to be an upstream socket and
	// reflects bytes back to the client.
	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		_, _ = io.Copy(got.conn, got.conn)
		_ = got.conn.Close()
	}()

	payload := []byte("end-to-end EWP through sing-box adapter")
	if _, err := clientConn.Write(payload); err != nil {
		t.Fatalf("client Write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatalf("client ReadFull: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Errorf("echo mismatch: got %q want %q", buf, payload)
	}

	_ = clientConn.Close()
	select {
	case <-echoDone:
	case <-time.After(2 * time.Second):
		t.Fatal("echo loop did not finish")
	}
	select {
	case <-srvDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not return")
	}
}

// ----------------------------------------------------------------------
// Helper conversions sanity (helpers.go round-trip)
// ----------------------------------------------------------------------

func TestSocksaddrEWPRoundTrip_IPv4(t *testing.T) {
	in := M.SocksaddrFromNetIP(netip.MustParseAddrPort("203.0.113.5:8443"))
	got := ewpToSocksaddr(socksaddrToEWP(in))
	if got.Addr != in.Addr || got.Port != in.Port {
		t.Errorf("round-trip mismatch: %v != %v", got, in)
	}
}

func TestSocksaddrEWPRoundTrip_FQDN(t *testing.T) {
	in := M.Socksaddr{Fqdn: "example.com", Port: 443}
	e := socksaddrToEWP(in)
	if e.Domain != "example.com" || e.Port != 443 {
		t.Fatalf("socksaddrToEWP fqdn: got %+v", e)
	}
	got := ewpToSocksaddr(e)
	if got.Fqdn != "example.com" || got.Port != 443 {
		t.Errorf("ewpToSocksaddr fqdn: got %+v", got)
	}
}

// guard against forgotten errors:
var _ = errors.New("")
