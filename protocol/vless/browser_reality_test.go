//go:build with_cronet && with_cronet_test && with_utls

package vless

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	stdTLS "crypto/tls"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type browserRealityRouter struct {
	adapter.Router
	metadata chan adapter.InboundContext
}

func (r *browserRealityRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.metadata <- metadata
	go func() {
		_, err := io.Copy(conn, conn)
		_ = conn.Close()
		if onClose != nil {
			onClose(err)
		}
	}()
}

// Exercise the real outbound constructor: browser=true must select native
// REALITY even when tls.utls is absent, then traverse the existing VLESS inbound.
func TestBrowserRealityVLESS(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("proxy request reached camouflage target") }))
			target.EnableHTTP2 = true
			target.Config.ErrorLog = log.New(io.Discard, "", 0)
			target.TLS = &stdTLS.Config{MinVersion: stdTLS.VersionTLS13}
			target.StartTLS()
			defer target.Close()
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			dest := M.ParseSocksaddr(target.Listener.Addr().String())
			const uuid = "11111111-2222-3333-4444-555555555555"
			transport := option.V2RayTransportOptions{Type: C.V2RayTransportTypeXHTTP, XHTTPOptions: option.V2RayXHTTPOptions{
				V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/reality", Host: "front.test"},
			}}
			router := &browserRealityRouter{metadata: make(chan adapter.InboundContext, 1)}
			inbound, err := NewInbound(ctx, router, logger.NOP(), "reality-in", option.VLESSInboundOptions{
				Users: []option.VLESSUser{{Name: "browser", UUID: uuid}}, Transport: &transport,
				InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: &option.InboundTLSOptions{
					Enabled: true, ServerName: "reality.test", Reality: &option.InboundRealityOptions{
						Enabled: true, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), ShortID: []string{"0102"},
						Handshake: option.InboundRealityHandshakeOptions{ServerOptions: option.ServerOptions{Server: dest.AddrString(), ServerPort: dest.Port}},
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer inbound.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() { _ = inbound.(*Inbound).transport.Serve(listener) }()
			clientTransport := transport
			clientTransport.XHTTPOptions.Browser = true
			serverAddr := M.ParseSocksaddr(listener.Addr().String())
			outbound, err := NewOutbound(ctx, nil, logger.NOP(), "browser-out", option.VLESSOutboundOptions{
				UUID: uuid, Transport: &clientTransport,
				ServerOptions: option.ServerOptions{Server: serverAddr.AddrString(), ServerPort: serverAddr.Port},
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
					Enabled: true, ServerName: "reality.test", Reality: &option.OutboundRealityOptions{
						Enabled: true, PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "0102",
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer outbound.(*Outbound).Close()
			destination := M.ParseSocksaddr("destination.test:443")
			conn, err := outbound.DialContext(ctx, N.NetworkTCP, destination)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
			payload := bytes.Repeat([]byte("VLESS over Browser XHTTP REALITY\n"), 16384)
			written := make(chan error, 1)
			go func() { _, err := conn.Write(payload); written <- err }()
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatal(err)
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatal("VLESS echo mismatch")
			}
			select {
			case metadata := <-router.metadata:
				if metadata.User != "browser" || metadata.Destination != destination {
					t.Fatalf("wrong routed metadata: %+v", metadata)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
