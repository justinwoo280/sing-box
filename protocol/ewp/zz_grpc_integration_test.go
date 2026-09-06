package ewp

import (
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raygrpclite"
	"github.com/sagernet/sing/common/bufio/deadline"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	sewp "github.com/justinwoo280/sing-ewp"
)

type loopbackDialer struct{}

func (loopbackDialer) DialContext(ctx context.Context, network string, dest M.Socksaddr) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, dest.String())
}

func (loopbackDialer) DialPacketContext(ctx context.Context, dest M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func (loopbackDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

type echoEWPHandler struct{}

func (echoEWPHandler) NewConnection(ctx context.Context, conn net.Conn, md sewp.Metadata) error {
	_, _ = io.Copy(conn, conn)
	return conn.Close()
}

func (echoEWPHandler) NewPacketConnection(ctx context.Context, pc net.PacketConn, md sewp.Metadata) error {
	return pc.Close()
}

type grpcTestServerHandler struct {
	service *sewp.ServiceV23
}

func (h *grpcTestServerHandler) NewConnectionEx(ctx context.Context, conn net.Conn,
	source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc,
) {
	if deadline.NeedAdditionalReadDeadline(conn) {
		conn = deadline.NewConn(conn)
	}
	hsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = h.service.HandleConn(hsCtx, conn)
	if onClose != nil {
		onClose(nil)
	}
}

// TestEWP_Over_GRPCLite runs the real EWP client+server handshake over a
// real grpclite (gun, HTTP/2) transport in cleartext-h2c loopback, then
// echoes a payload. Confirms EWP-over-gRPC completes instead of hanging.
func TestEWP_Over_GRPCLite(t *testing.T) {
	priv, pub, err := sewp.GenerateSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	const serverID = "grpc-test"
	service, err := sewp.NewServiceV23(echoEWPHandler{}, priv, serverID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AddUser(v23TestUUID); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := M.SocksaddrFromNet(ln.Addr())

	srv, err := v2raygrpclite.NewServer(context.Background(), nopLogger{},
		option.V2RayGRPCOptions{ServiceName: "GunService"}, nil,
		&grpcTestServerHandler{service: service})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	client := v2raygrpclite.NewClient(context.Background(), loopbackDialer{}, addr,
		option.V2RayGRPCOptions{ServiceName: "GunService"}, nil)
	defer client.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	gunConn, err := client.DialContext(dialCtx)
	if err != nil {
		t.Fatalf("grpc DialContext: %v", err)
	}
	if deadline.NeedAdditionalReadDeadline(gunConn) {
		gunConn = deadline.NewConn(gunConn)
	}

	ewpClient, err := sewp.NewClientV23(v23TestUUID, serverID, pub, 0)
	if err != nil {
		t.Fatalf("NewClientV23: %v", err)
	}
	dst := sewp.Address{Addr: netip.MustParseAddrPort("8.8.8.8:443")}
	hsCtx, hsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hsCancel()
	conn, err := ewpClient.DialConn(hsCtx, gunConn, dst)
	if err != nil {
		t.Fatalf("EWP DialConn over gRPC: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello over gRPC")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	t.Logf("EWP over gRPC echo ok: %q", buf)
}
