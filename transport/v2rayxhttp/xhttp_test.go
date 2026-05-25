package v2rayxhttp_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// Verifies that sing-box's v2ray.NewServerTransport / NewClientTransport
// correctly dispatch to the xhttp bridge and that data flows through.
// This is the integration test for the EWP-over-XHTTP path: EWP simply
// calls v2ray.NewServerTransport / NewClientTransport, so if this works
// EWP-over-XHTTP works.
func TestXHTTP_PlaintextPacketUp_EndToEnd(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	transOpts := option.V2RayTransportOptions{
		Type: "xhttp",
		XHTTPOptions: option.V2RayXHTTPOptions{
			Mode: "packet-up",
			Path: "/xhttp",
			ScMaxEachPostBytes: &option.XHTTPRange{From: 4096, To: 4096},
		},
	}

	srv, err := v2ray.NewServerTransport(context.Background(), logger.NOP(),
		transOpts, nil /* plaintext */, echoServerHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve(listener)
	time.Sleep(50 * time.Millisecond)

	cli, err := v2ray.NewClientTransport(context.Background(),
		directDialer{}, M.ParseSocksaddrHostPort("127.0.0.1", uint16(port)),
		transOpts, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	conn, err := cli.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	payload := make([]byte, 32*1024)
	_, _ = rand.Read(payload)

	var wg sync.WaitGroup
	wg.Add(2)
	recv := make([]byte, len(payload))
	var readErr error
	go func() { defer wg.Done(); _, readErr = io.ReadFull(conn, recv) }()
	go func() { defer wg.Done(); io.Copy(conn, bytes.NewReader(payload)) }()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	if !bytes.Equal(payload, recv) {
		t.Fatal("payload mismatch")
	}
}

type echoServerHandler struct{}

var _ adapter.V2RayServerTransportHandler = echoServerHandler{}

func (echoServerHandler) NewConnectionEx(ctx context.Context, conn net.Conn,
	_ M.Socksaddr, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		defer conn.Close()
		io.Copy(conn, conn)
		if onClose != nil {
			onClose(nil)
		}
	}()
}

type directDialer struct{}

func (directDialer) DialContext(ctx context.Context, network string, dest M.Socksaddr) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, dest.String())
}
func (directDialer) ListenPacket(ctx context.Context, dest M.Socksaddr) (net.PacketConn, error) {
	return net.ListenPacket("udp", "")
}
