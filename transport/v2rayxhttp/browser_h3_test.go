//go:build with_cronet && with_cronet_test

package xhttp

import (
	"bytes"
	"context"
	stdTLS "crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justinwoo280/sing-xhttp/xhttp"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type browserH3Dialer struct {
	N.Dialer
	tcp, udp atomic.Int32
	wrapped  bool
}

func (d *browserH3Dialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if network == N.NetworkUDP {
		d.udp.Add(1)
	} else {
		d.tcp.Add(1)
	}
	conn, err := d.Dialer.DialContext(ctx, network, destination)
	if err == nil && d.wrapped {
		conn = &browserH3WrappedConn{conn}
	}
	return conn, err
}

type browserH3WrappedConn struct{ net.Conn }

func TestBrowserHTTP3Interop(t *testing.T) {
	keyPEM, certPEM, err := tls.GenerateCertificate(nil, nil, time.Now, "browser.test", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := stdTLS.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []struct {
		mode   string
		framed bool
	}{
		{"packet-up", false}, {"stream-up", false}, {"stream-one", false},
		{"stream-up", true}, {"stream-one", true},
	} {
		name := stream.mode
		if stream.framed {
			name += "/grpc"
		}
		for _, wrapped := range []bool{false, true} {
			route := "direct"
			if wrapped {
				route = "wrapped"
			}
			t.Run(name+"/"+route, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				server, err := xhttp.NewServer(ctx, logger.NOP(), xhttp.Options{
					Mode: stream.mode, GRPCFraming: stream.framed, Path: "/browser", Host: "front.test",
				}, nil, browserEchoHandler{})
				if err != nil {
					t.Fatal(err)
				}
				defer server.Close()
				packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				h3 := &http3.Server{Logger: slog.Default(),
					TLSConfig: &stdTLS.Config{Certificates: []stdTLS.Certificate{certificate}},
					Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.ProtoMajor != 3 || r.TLS.NegotiatedProtocol != "h3" {
							t.Errorf("protocol = %s / %s", r.Proto, r.TLS.NegotiatedProtocol)
						}
						if r.Host != "front.test" || r.TLS.ServerName != "browser.test" {
							t.Errorf("authority/SNI = %s / %s", r.Host, r.TLS.ServerName)
						}
						server.ServeHTTP(w, r)
					}),
				}
				done := make(chan error, 1)
				go func() { done <- h3.Serve(packet) }()
				defer func() { _ = h3.Close(); _ = packet.Close(); <-done }()
				tlsConfig, err := tls.NewClient(ctx, logger.NOP(), "browser.test", option.OutboundTLSOptions{
					Enabled: true, ALPN: []string{"h3"}, Certificate: []string{string(certPEM)},
				})
				if err != nil {
					t.Fatal(err)
				}
				dialer := &browserH3Dialer{Dialer: N.SystemDialer, wrapped: wrapped}
				client, err := NewClient(ctx, dialer, M.ParseSocksaddr(packet.LocalAddr().String()), option.V2RayXHTTPOptions{
					Browser: true, V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{
						Mode: stream.mode, GRPCFraming: stream.framed, Path: "/browser", Host: "front.test",
					},
				}, tlsConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				for i := 0; i < 3; i++ {
					if i == 2 {
						if err := client.(*browserClient).Reset(); err != nil {
							t.Fatal(err)
						}
					}
					conn, err := client.DialContext(ctx)
					if err != nil {
						t.Fatal(err)
					}
					payload := bytes.Repeat([]byte("cronet-h3-to-sing-xhttp\n"), 50000)
					written := make(chan error, 1)
					go func() { _, err := conn.Write(payload); written <- err }()
					got := make([]byte, len(payload))
					_, err = io.ReadFull(conn, got)
					if err != nil {
						_ = conn.Close()
						t.Fatal(err)
					}
					if !bytes.Equal(got, payload) {
						t.Fatal("HTTP/3 echo mismatch")
					}
					if err := <-written; err != nil {
						t.Fatal(err)
					}
					_ = conn.Close()
				}
				if dialer.tcp.Load() != 0 || dialer.udp.Load() < 2 {
					t.Fatalf("dial counts: TCP=%d UDP=%d", dialer.tcp.Load(), dialer.udp.Load())
				}
			})
		}
	}
}
