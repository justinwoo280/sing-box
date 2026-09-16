package ewp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

type ewpTestRouter struct {
	mu   sync.Mutex
	conn net.Conn
	md   adapter.InboundContext
	done chan struct{}
}

func (r *ewpTestRouter) RouteConnection(context.Context, net.Conn, adapter.InboundContext) error {
	return nil
}

func (r *ewpTestRouter) RoutePacketConnection(context.Context, N.PacketConn, adapter.InboundContext) error {
	return nil
}

func (r *ewpTestRouter) RouteConnectionEx(_ context.Context, conn net.Conn, md adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.mu.Lock()
	r.conn = conn
	r.md = md
	r.mu.Unlock()
	close(r.done)
}

func (r *ewpTestRouter) RoutePacketConnectionEx(_ context.Context, _ N.PacketConn, _ adapter.InboundContext, _ N.CloseHandlerFunc) {
	close(r.done)
}

func TestEWPV23TCPAdapter(t *testing.T) {
	const (
		testUUID = "11111111-2222-3333-4444-555555555555"
		serverID = "singbox-1.14-test"
	)
	privateKey, publicKey, err := sewp.GenerateSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	router := &ewpTestRouter{done: make(chan struct{})}
	in := &Inbound{
		router: router,
		logger: logger.NOP(),
		users:  []option.EWPUser{{Name: "alice", UUID: testUUID}},
	}
	in.service, err = sewp.NewServiceV23(&inboundHandler{owner: in}, privateKey, serverID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer in.service.Close()
	if err := in.service.AddUser(testUUID); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		in.NewConnection(context.Background(), serverConn, adapter.InboundContext{
			Source: M.SocksaddrFromNetIP(netip.MustParseAddrPort("127.0.0.1:1234")),
		}, nil)
	}()

	client, err := sewp.NewClientV23(testUUID, serverID, publicKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	destination := sewp.Address{Addr: netip.MustParseAddrPort("8.8.8.8:443")}
	flow, err := client.DialConn(ctx, clientConn, destination)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-router.done:
	case <-time.After(2 * time.Second):
		t.Fatal("EWP inbound did not route the connection")
	}
	router.mu.Lock()
	routedConn, metadata := router.conn, router.md
	router.mu.Unlock()
	if routedConn == nil {
		t.Fatal("router received nil connection")
	}
	if metadata.Destination.Addr != destination.Addr.Addr() || metadata.Destination.Port != destination.Addr.Port() {
		t.Fatalf("destination = %v, want %v", metadata.Destination, destination.Addr)
	}
	if metadata.User != "alice" {
		t.Fatalf("user = %q, want alice", metadata.User)
	}

	go func() {
		_, _ = io.Copy(routedConn, routedConn)
		_ = routedConn.Close()
	}()
	payload := []byte("EWP v2.3.1 over sing-box 1.14")
	if _, err := flow.Write(payload); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(flow, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatalf("echo = %q, want %q", received, payload)
	}
	_ = flow.Close()
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("EWP server handler did not return")
	}
}

func TestEWPSocksaddrConversion(t *testing.T) {
	for _, input := range []M.Socksaddr{
		M.SocksaddrFromNetIP(netip.MustParseAddrPort("203.0.113.5:8443")),
		{Fqdn: "example.com", Port: 443},
	} {
		converted := ewpToSocksaddr(socksaddrToEWP(input))
		if converted != input {
			t.Fatalf("round trip = %v, want %v", converted, input)
		}
	}
}
