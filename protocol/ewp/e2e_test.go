package ewp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
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
	adapter.Router
	mu         sync.Mutex
	conn       net.Conn
	packetConn N.PacketConn
	md         adapter.InboundContext
	done       chan struct{}
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

func (r *ewpTestRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, md adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.mu.Lock()
	r.packetConn = conn
	r.md = md
	r.mu.Unlock()
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

func TestEWPEarlyServerFinishedOptions(t *testing.T) {
	const testUUID = "11111111-2222-3333-4444-555555555555"

	for _, tc := range []struct {
		name          string
		client        bool
		server        bool
		wantHandshake bool
	}{
		{name: "both_enabled", client: true, server: true, wantHandshake: true},
		{name: "client_only_rejected", client: true, server: false, wantHandshake: false},
		{name: "server_only_legacy_compatibility", client: false, server: true, wantHandshake: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			privateKey, publicKey, err := sewp.GenerateSigningIdentity()
			if err != nil {
				t.Fatal(err)
			}
			router := &ewpTestRouter{done: make(chan struct{})}
			inbound, err := NewInbound(ctx, router, logger.NOP(), "ewp-in", option.EWPInboundOptions{
				ServerID:            "early-option-test",
				SigningPrivateKey:   privateKey,
				EarlyServerFinished: tc.server,
				Users:               []option.EWPUser{{Name: "alice", UUID: testUUID}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer inbound.Close()

			backend, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			go func() {
				conn, acceptErr := backend.Accept()
				if acceptErr != nil {
					return
				}
				inbound.(*Inbound).NewConnection(ctx, conn, adapter.InboundContext{
					Source: M.SocksaddrFromNetIP(netip.MustParseAddrPort("127.0.0.1:1234")),
				}, nil)
			}()

			proxy, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close()
			firstWire := make(chan []byte, 1)
			go forwardEWPProxy(t, proxy, backend.Addr().String(), firstWire)

			outbound, err := NewOutbound(ctx, nil, logger.NOP(), "ewp-out", option.EWPOutboundOptions{
				UUID:                testUUID,
				ServerID:            "early-option-test",
				ServerPublicKey:     publicKey,
				EarlyServerFinished: tc.client,
				ServerOptions:       option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(proxy.Addr().(*net.TCPAddr).Port)},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer outbound.(*Outbound).Close()

			flow, dialErr := outbound.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr("destination.test:443"))
			first := <-firstWire
			if len(first) < 32 {
				t.Fatalf("captured ClientInitExt is too short: %d", len(first))
			}
			gotEarlyFlag := len(first) > 32 && first[32]&0x02 != 0
			if gotEarlyFlag != tc.client {
				t.Fatalf("ClientInit early flag = %v, want %v", gotEarlyFlag, tc.client)
			}
			if tc.wantHandshake {
				if dialErr != nil {
					t.Fatalf("DialContext: %v", dialErr)
				}
				defer flow.Close()
				select {
				case <-router.done:
				case <-ctx.Done():
					t.Fatal("EWP handler was not reached")
				}
				router.mu.Lock()
				routed := router.conn
				router.mu.Unlock()
				go func() {
					_, _ = io.Copy(routed, routed)
					_ = routed.Close()
				}()
				payload := []byte("early server finished option")
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
			} else if dialErr == nil {
				flow.Close()
				t.Fatal("client-only early opt-in unexpectedly succeeded")
			}
		})
	}
}

func forwardEWPProxy(t *testing.T, listener net.Listener, backend string, firstWire chan<- []byte) {
	t.Helper()
	front, err := listener.Accept()
	if err != nil {
		firstWire <- nil
		return
	}
	defer front.Close()
	back, err := net.Dial("tcp", backend)
	if err != nil {
		firstWire <- nil
		return
	}
	defer back.Close()
	go func() {
		_, _ = io.Copy(front, back)
		_ = front.Close()
	}()

	header := make([]byte, 3)
	if _, err := io.ReadFull(front, header); err != nil {
		firstWire <- nil
		return
	}
	n := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
	body := make([]byte, n)
	if _, err := io.ReadFull(front, body); err != nil {
		firstWire <- nil
		return
	}
	firstWire <- append([]byte(nil), body...)
	if _, err := back.Write(append(header, body...)); err != nil {
		return
	}
	_, _ = io.Copy(back, front)
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

func TestEWPBrowserTLSSelection(t *testing.T) {
	options := option.EWPOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "example.com", ServerPort: 443},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: &option.OutboundTLSOptions{
				Enabled: true,
				Reality: &option.OutboundRealityOptions{Enabled: true, PublicKey: "invalid"},
				ECH:     &option.OutboundECHOptions{Enabled: true},
			},
		},
		Transport: &option.V2RayTransportOptions{
			Type: "xhttp",
			XHTTPOptions: option.V2RayXHTTPOptions{
				V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: "packet-up"},
				Browser:               true,
			},
		},
	}

	_, err := newOutboundTLS(context.Background(), logger.NOP(), options)
	if err == nil || !strings.Contains(err.Error(), "browser REALITY is incompatible with ECH") {
		t.Fatalf("NewOutbound error = %v, want browser REALITY/ECH validation", err)
	}
}
