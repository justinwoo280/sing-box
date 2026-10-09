//go:build with_cronet && with_cronet_test

package xhttp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	stdTLS "crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justinwoo280/sing-xhttp/xhttp"
	mDNS "github.com/miekg/dns"
	"github.com/sagernet/cronet-go"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/certificate"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type browserEchoHandler struct{}

func (browserEchoHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	_, _ = io.Copy(conn, conn)
	_ = conn.Close()
	if onClose != nil {
		onClose(nil)
	}
}

// Test against sing-xhttp's actual decoder and session reorder queue. cronet-go
// itself remains independent of that module.
func TestBrowserInterop(t *testing.T) {
	for _, stream := range []struct {
		mode   string
		framed bool
		proxy  bool
	}{
		{mode: "packet-up"},
		{mode: "stream-up"},
		{mode: "stream-one"},
		{mode: "stream-up", framed: true},
		{mode: "stream-one", framed: true},
		{mode: "stream-up", framed: true, proxy: true},
		{mode: "stream-one", framed: true, proxy: true},
	} {
		mode := stream.mode
		name := mode
		if stream.framed {
			name += "/grpc"
		}
		if stream.proxy {
			name += "/proxy"
		}
		for _, echMode := range []string{"off", "static", "dns", "retry", "large"} {
			if stream.proxy && echMode != "off" {
				continue
			}
			t.Run(name+"/"+echMode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				store, err := certificate.NewStore(ctx, logger.NOP(), option.CertificateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				ctx = service.ContextWith[adapter.CertificateStore](ctx, store)
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"browser.test", "public.test", "front.test"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
				der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
				if err != nil {
					t.Fatal(err)
				}
				server, err := xhttp.NewServer(ctx, logger.NOP(), xhttp.Options{Mode: mode, Path: "/browser", Host: "front.test", GRPCFraming: stream.framed, ScStreamUpServerSecs: &xhttp.Range{From: 1, To: 1}}, nil, browserEchoHandler{})
				if err != nil {
					t.Fatal(err)
				}
				defer server.Close()
				httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Host != "front.test" {
						t.Errorf("authority = %q", r.Host)
					}
					serverName := "browser.test"
					if stream.proxy {
						serverName = "front.test"
					}
					if r.TLS.ServerName != serverName {
						t.Errorf("SNI = %q", r.TLS.ServerName)
					}
					if r.TLS.ECHAccepted != (echMode != "off") {
						t.Errorf("ECHAccepted = %v", r.TLS.ECHAccepted)
					}
					if r.ProtoMajor != 2 {
						t.Errorf("protocol = %s", r.Proto)
					}
					server.ServeHTTP(w, r)
				}))
				httpServer.EnableHTTP2 = true
				httpServer.TLS = &stdTLS.Config{Certificates: []stdTLS.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
				tlsOptions := option.OutboundTLSOptions{Enabled: true, Certificate: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}}
				var queries atomic.Int32
				if echMode != "off" {
					configPEM, keyPEM, err := tls.ECHKeygenDefault("public.test")
					if err != nil {
						t.Fatal(err)
					}
					keyBlock, _ := pem.Decode([]byte(keyPEM))
					echKeys, err := tls.UnmarshalECHKeys(keyBlock.Bytes)
					if err != nil {
						t.Fatal(err)
					}
					for i := range echKeys {
						echKeys[i].SendAsRetry = true
					}
					httpServer.TLS.EncryptedClientHelloKeys = echKeys
					if echMode == "retry" {
						// A stale key must provoke Chromium's authenticated ECH retry.
						configPEM, _, err = tls.ECHKeygenDefault("public.test")
						if err != nil {
							t.Fatal(err)
						}
					}
					if echMode == "large" {
						// Force the internal DNS bridge's UDP truncation/TCP retry.
						block, _ := pem.Decode([]byte(configPEM))
						configs := bytes.Repeat(block.Bytes[2:], 8)
						list := binary.BigEndian.AppendUint16(nil, uint16(len(configs)))
						list = append(list, configs...)
						configPEM = string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: list}))
					}
					tlsOptions.ECH = &option.OutboundECHOptions{Enabled: true}
					if echMode == "dns" {
						configBlock, _ := pem.Decode([]byte(configPEM))
						tlsOptions.ECH.QueryServerName = "ech-query.test"
						ctx = service.ContextWith[adapter.DNSRouter](ctx, &browserDNSRouter{exchange: func(_ context.Context, request *mDNS.Msg, _ adapter.DNSQueryOptions) (*mDNS.Msg, error) {
							queries.Add(1)
							if len(request.Question) != 1 || request.Question[0].Name != "ech-query.test." || request.Question[0].Qtype != mDNS.TypeHTTPS {
								t.Errorf("unexpected DNS query: %v", request.Question)
							}
							response := new(mDNS.Msg)
							response.SetReply(request)
							response.Answer = []mDNS.RR{&mDNS.HTTPS{SVCB: mDNS.SVCB{
								Hdr:      mDNS.RR_Header{Name: request.Question[0].Name, Rrtype: mDNS.TypeHTTPS, Class: mDNS.ClassINET, Ttl: 300},
								Priority: 1, Target: ".", Value: []mDNS.SVCBKeyValue{&mDNS.SVCBECHConfig{ECH: configBlock.Bytes}},
							}}}
							return response, nil
						}})
					} else {
						tlsOptions.ECH.Config = []string{configPEM}
					}
				}
				httpServer.StartTLS()
				defer httpServer.Close()
				endpoint := httpServer
				var proxyUploads, proxyDownloads *atomic.Int32
				if stream.proxy {
					endpoint, proxyUploads, proxyDownloads = browserGRPCProxy(t, httpServer)
				}
				tlsConfig, err := tls.NewClient(ctx, logger.NOP(), "browser.test", tlsOptions)
				if err != nil {
					t.Fatal(err)
				}
				client, err := NewClient(ctx, N.SystemDialer, M.ParseSocksaddr(endpoint.Listener.Addr().String()), option.V2RayXHTTPOptions{Browser: true, V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/browser", Host: "front.test", GRPCFraming: stream.framed}}, tlsConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				conn, err := client.DialContext(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				payload := bytes.Repeat([]byte("cronet-to-sing-xhttp\n"), 100000)
				if stream.proxy {
					payload = bytes.Repeat([]byte("cronet-to-grpc-proxy\n"), 300000)
				}
				written := make(chan error, 1)
				go func() { _, err := conn.Write(payload); written <- err }()
				got := make([]byte, len(payload))
				if _, err := io.ReadFull(conn, got); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, payload) {
					t.Fatal("echo mismatch")
				}
				if err := <-written; err != nil {
					t.Fatal(err)
				}
				if echMode == "dns" && queries.Load() != 1 {
					t.Errorf("DNS queries = %d", queries.Load())
				}
				if stream.proxy {
					if proxyUploads.Load() < 2 {
						t.Fatal("proxy did not decode multiple upload messages")
					}
					// Stream-up responses carry protobuf heartbeats on the RPC hop.
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for proxyDownloads.Load() == 0 {
						select {
						case <-ticker.C:
						case <-ctx.Done():
							t.Fatal("proxy did not decode a response or heartbeat")
						}
					}
				}
			})
		}
	}
}

func TestBrowserSystemStoreRejectsUntrustedCertificate(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up", "stream-one"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, err := certificate.NewStore(ctx, logger.NOP(), option.CertificateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx = service.ContextWith[adapter.CertificateStore](ctx, store)
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			tlsConfig, err := tls.NewClient(ctx, logger.NOP(), "example.com", option.OutboundTLSOptions{Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewClient(ctx, N.SystemDialer, M.ParseSocksaddr(server.Listener.Addr().String()), option.V2RayXHTTPOptions{
				Browser: true, V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/browser"},
			}, tlsConfig)
			if err != nil {
				t.Fatalf("default system store prevented client creation: %v", err)
			}
			defer client.Close()
			conn, err := client.DialContext(ctx)
			if err == nil {
				defer conn.Close()
				_, err = conn.Read(make([]byte, 1))
			}
			if err == nil || !strings.Contains(err.Error(), "ERR_CERT_AUTHORITY_INVALID") && !errors.Is(err, cronet.NetErrorCertAuthorityInvalid) {
				t.Fatalf("untrusted certificate was not rejected by Cronet: %v", err)
			}
			if requests.Load() != 0 {
				t.Fatal("HTTP request reached an untrusted server")
			}
		})
	}
}

// Terminate each RPC hop with grpc-go and decode/re-encode every protobuf
// message. The independent download GET goes through an HTTP streaming proxy.
func browserGRPCProxy(t *testing.T, origin *httptest.Server) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	backendTLS := origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	backendTLS.ServerName = "front.test"
	backend, err := grpc.NewClient(origin.Listener.Addr().String(), grpc.WithAuthority("front.test"), grpc.WithTransportCredentials(credentials.NewTLS(backendTLS)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	uploads, downloads := new(atomic.Int32), new(atomic.Int32)
	proxy := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, incoming grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(incoming)
		md, _ := metadata.FromIncomingContext(incoming.Context())
		outgoing := metadata.MD{}
		for _, key := range []string{"referer", "x-xhttp-session", "x-padding", "cookie"} {
			if values := md.Get(key); len(values) != 0 {
				outgoing.Set(key, values...)
			}
		}
		ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(incoming.Context(), outgoing))
		defer cancel()
		stream, err := backend.NewStream(ctx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, method)
		if err != nil {
			return err
		}
		go func() {
			for {
				message := new(wrapperspb.BytesValue)
				err := incoming.RecvMsg(message)
				if err == io.EOF {
					_ = stream.CloseSend()
					return
				}
				if err == nil {
					uploads.Add(1)
					err = stream.SendMsg(message)
				}
				if err != nil {
					cancel()
					return
				}
			}
		}()
		for {
			message := new(wrapperspb.BytesValue)
			if err := stream.RecvMsg(message); err != nil {
				incoming.SetTrailer(stream.Trailer())
				if err == io.EOF {
					return nil
				}
				return err
			}
			downloads.Add(1)
			if err := incoming.SendMsg(message); err != nil {
				return err
			}
		}
	}))
	t.Cleanup(proxy.Stop)
	originURL, _ := url.Parse(origin.URL)
	httpProxy := httputil.NewSingleHostReverseProxy(originURL)
	transport := &http2.Transport{TLSClientConfig: backendTLS}
	httpProxy.Transport, httpProxy.FlushInterval = transport, -1
	t.Cleanup(transport.CloseIdleConnections)
	front := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "front.test" || r.TLS.ServerName != "browser.test" || r.ProtoMajor != 2 {
			t.Errorf("proxy request: host=%s SNI=%s proto=%s", r.Host, r.TLS.ServerName, r.Proto)
		}
		if r.Method == http.MethodGet {
			httpProxy.ServeHTTP(w, r)
		} else {
			proxy.ServeHTTP(w, r)
		}
	}))
	front.EnableHTTP2 = true
	front.TLS = origin.TLS.Clone()
	front.StartTLS()
	t.Cleanup(front.Close)
	return front, uploads, downloads
}

type browserDNSRouter struct {
	adapter.DNSRouter
	exchange func(context.Context, *mDNS.Msg, adapter.DNSQueryOptions) (*mDNS.Msg, error)
}

func (r *browserDNSRouter) Exchange(ctx context.Context, request *mDNS.Msg, options adapter.DNSQueryOptions) (*mDNS.Msg, error) {
	return r.exchange(ctx, request, options)
}

func TestBrowserMetadataInterop(t *testing.T) {
	for _, placement := range []string{"header", "cookie"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server, err := xhttp.NewServer(ctx, logger.NOP(), xhttp.Options{
				Mode: "packet-up", Path: "/meta", SessionPlacement: "query", SeqPlacement: "header",
				UplinkDataPlacement: placement, UplinkDataKey: "x_data", XPaddingObfsMode: true,
				XPaddingPlacement: "header", XPaddingMethod: "tokenish", XPaddingBytes: &xhttp.Range{From: 100, To: 100},
			}, nil, browserEchoHandler{})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			client, err := cronet.NewBrowserXHTTPClient(ctx, httpServer.URL, cronet.BrowserXHTTPOptions{
				Path: "/meta", SessionPlace: "query", SeqPlace: "header", UplinkPlace: placement, UplinkKey: "x_data",
				XPaddingObfs: true, XPaddingPlace: "header", XPaddingMethod: "tokenish", XPaddingBytes: cronet.BrowserXHTTPRange{From: 100, To: 100},
				ScMaxEachPostBytes: cronet.BrowserXHTTPRange{From: 512, To: 512},
				SessionIDTable:     "hex", SessionIDLength: cronet.BrowserXHTTPRange{From: 16, To: 16},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			conn, err := client.DialContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			payload := bytes.Repeat([]byte("metadata\n"), 400)
			written := make(chan error, 1)
			go func() { _, err := conn.Write(payload); written <- err }()
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("metadata echo mismatch")
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
		})
	}
}
