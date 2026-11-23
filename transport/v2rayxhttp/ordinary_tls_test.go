//go:build with_utls

package xhttp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	stdTLS "crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func TestOrdinaryXHTTPAdvancedJSONSafari(t *testing.T) {
	testOrdinaryXHTTPAdvancedJSONProfile(t, &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "safari"})
}

func TestOrdinaryXHTTPUTLSProtocol(t *testing.T) {
	for _, fingerprint := range []string{"", "safari", "chrome"} {
		for _, protocol := range []string{"http/1.1", "h2"} {
			t.Run(fingerprint+"/"+protocol, func(t *testing.T) {
				seen := make(chan string, 1)
				posted := make(chan string, 1)
				upload := make(chan []byte, 1)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.TLS.NegotiatedProtocol != protocol {
						t.Errorf("negotiated %s, want %s", r.TLS.NegotiatedProtocol, protocol)
					}
					if r.Method == http.MethodGet {
						seen <- r.RequestURI
						select {
						case body := <-upload:
							_, _ = w.Write(body)
						case <-r.Context().Done():
						}
						return
					}
					if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/0") {
						t.Errorf("ordinary TLS auto mode changed wire shape: %s %s", r.Method, r.RequestURI)
						http.NotFound(w, r)
						return
					}
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("read upload: %v", err)
						return
					}
					posted <- r.RequestURI
					upload <- body
					w.WriteHeader(http.StatusOK)
				}))
				server.EnableHTTP2 = protocol == "h2"
				server.StartTLS()
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				opts := option.OutboundTLSOptions{Enabled: true, ServerName: "example.com", ALPN: []string{protocol}, Certificate: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}}
				if fingerprint != "" {
					opts.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: fingerprint}
				}
				tlsConfig, err := tls.NewClient(ctx, logger.NOP(), "127.0.0.1", opts)
				if err != nil {
					t.Fatal(err)
				}
				client, err := NewClient(ctx, N.SystemDialer, M.SocksaddrFromNet(server.Listener.Addr()), option.V2RayXHTTPOptions{V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: "auto", Host: "front.test", Path: "/xhttp"}}, tlsConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				conn, err := client.DialContext(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if _, err = conn.Write([]byte("packet payload")); err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(conn)
				if err != nil || string(body) != "packet payload" {
					t.Fatalf("body=%q error=%v", body, err)
				}
				select {
				case request := <-seen:
					if !strings.HasPrefix(request, "/xhttp/") || request == "/xhttp/" {
						t.Fatalf("ordinary TLS auto mode changed wire shape: %s", request)
					}
					select {
					case post := <-posted:
						if post != request+"/0" {
							t.Fatalf("upload %s does not match download %s", post, request)
						}
					case <-ctx.Done():
						t.Fatal("no upload request")
					}
				case <-ctx.Done():
					t.Fatal("no HTTP request")
				}
			})
		}
	}
}

type ordinaryEchoHandler struct{}

func (ordinaryEchoHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, done N.CloseHandlerFunc) {
	go func() {
		_, err := io.Copy(conn, conn)
		_ = conn.Close()
		if done != nil {
			done(err)
		}
	}()
}

// Authenticating to a local REALITY server proves the explicit capability
// keeps auto=stream-one, rather than merely fixing ordinary uTLS by removing
// REALITY detection entirely.
func TestOrdinaryXHTTPRealityAuto(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("authenticated XHTTP reached the REALITY fallback target")
		w.WriteHeader(http.StatusNotFound)
	}))
	target.EnableHTTP2 = true
	target.TLS = &stdTLS.Config{MinVersion: stdTLS.VersionTLS13}
	target.StartTLS()
	defer target.Close()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	targetAddress := M.SocksaddrFromNet(target.Listener.Addr())
	serverTLS, err := tls.NewRealityServer(ctx, logger.NOP(), option.InboundTLSOptions{
		Enabled: true, ServerName: "example.com", ALPN: []string{"h2"},
		Reality: &option.InboundRealityOptions{
			Enabled: true, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()),
			ShortID: []string{"0102030405060708"}, MaxTimeDifference: badoption.Duration(time.Minute),
			Handshake: option.InboundRealityHandshakeOptions{ServerOptions: option.ServerOptions{Server: targetAddress.AddrString(), ServerPort: targetAddress.Port}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(ctx, logger.NOP(), option.V2RayXHTTPOptions{V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: "stream-one", Host: "front.test", Path: "/reality"}}, serverTLS, ordinaryEchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	clientTLS, err := tls.NewClient(ctx, logger.NOP(), "127.0.0.1", option.OutboundTLSOptions{
		Enabled: true, ServerName: "example.com", ALPN: []string{"h2"},
		UTLS:    &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"},
		Reality: &option.OutboundRealityOptions{Enabled: true, PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "0102030405060708"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// kTLS must forward the marker even though it embeds only the base Config
	// interface. This check does not require enabling kernel TLS on the host.
	for _, config := range []tls.Config{clientTLS, clientTLS.Clone(), &tls.KTLSClientConfig{Config: clientTLS}} {
		marker, ok := config.(interface{ IsReality() bool })
		if !ok || !marker.IsReality() {
			t.Fatalf("lost REALITY capability: %T", config)
		}
	}
	client, err := NewClient(ctx, N.SystemDialer, M.SocksaddrFromNet(listener.Addr()), option.V2RayXHTTPOptions{V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: "auto", Host: "front.test", Path: "/reality"}}, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := client.DialContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload := []byte("authenticated REALITY auto transport")
	if _, err = conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("echo=%q error=%v", got, err)
	}
}
