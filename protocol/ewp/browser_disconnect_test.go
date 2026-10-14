//go:build with_cronet && with_cronet_test && with_utls

package ewp

import (
	"bytes"
	"context"
	stdTLS "crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// Exercise a separate TLS/H2 proxy which ends only the download after an EWP
// handshake and successful data transfer. Upload remains independently routed.
func TestEWPBrowserProxyDisconnect(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up"} {
		for _, ending := range []string{"eof", "reset"} {
			t.Run(mode+"/"+ending, func(t *testing.T) {
				const flows = 24
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				certificate, certPEM, _ := browserXHTTPCertificate(t)
				privateKey, publicKey, err := sewp.GenerateSigningIdentity()
				if err != nil {
					t.Fatal(err)
				}
				const uuid = "11111111-2222-3333-4444-555555555555"
				transport := option.V2RayTransportOptions{Type: "xhttp", XHTTPOptions: option.V2RayXHTTPOptions{
					V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Mode: mode, Path: "/ewp"},
				}}
				router := &browserXHTTPEchoRouter{metadata: make(chan adapter.InboundContext, flows)}
				inbound, err := NewInbound(ctx, router, logger.NOP(), "ewp-in", option.EWPInboundOptions{
					ServerID: "proxy-test", SigningPrivateKey: privateKey, Users: []option.EWPUser{{UUID: uuid}}, Transport: &transport,
				})
				if err != nil {
					t.Fatal(err)
				}
				defer inbound.Close()
				backend := httptest.NewUnstartedServer(inbound.(*Inbound).transport.(http.Handler))
				backend.EnableHTTP2 = true
				backend.TLS = &stdTLS.Config{Certificates: []stdTLS.Certificate{certificate}}
				backend.StartTLS()
				defer backend.Close()
				backend.Client().Transport.(*http.Transport).TLSClientConfig.ServerName = "ewp.test"
				target, _ := url.Parse(backend.URL)
				proxy := httputil.NewSingleHostReverseProxy(target)
				proxy.Transport = backend.Client().Transport
				proxy.FlushInterval = -1
				proxy.ErrorLog = log.New(io.Discard, "", 0)
				cutDownloads := make(chan *browserProxyCutBody, 1)
				var downloads atomic.Int32
				proxy.ModifyResponse = func(response *http.Response) error {
					if response.Request.Method != http.MethodGet || downloads.Add(1)%2 == 0 {
						return nil
					}
					body := &browserProxyCutBody{ReadCloser: response.Body, clean: ending == "eof"}
					response.Body = body
					cutDownloads <- body
					return nil
				}
				front := httptest.NewUnstartedServer(proxy)
				front.EnableHTTP2 = true
				front.TLS = &stdTLS.Config{Certificates: []stdTLS.Certificate{certificate}}
				front.Config.ErrorLog = log.New(io.Discard, "", 0)
				front.StartTLS()
				defer front.Close()
				transport.XHTTPOptions.Browser = true
				address := M.ParseSocksaddr(front.Listener.Addr().String())
				outbound, err := NewOutbound(ctx, nil, logger.NOP(), "ewp-out", option.EWPOutboundOptions{
					UUID: uuid, ServerID: "proxy-test", ServerPublicKey: publicKey, Transport: &transport,
					ServerOptions: option.ServerOptions{Server: address.AddrString(), ServerPort: address.Port},
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
						Enabled: true, ServerName: "ewp.test", Certificate: []string{certPEM},
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer outbound.(*Outbound).Close()
				for flow := 0; flow < flows; flow++ {
					conn, err := outbound.DialContext(ctx, "tcp", M.ParseSocksaddr("destination.test:443"))
					if err != nil {
						t.Fatalf("flow %d handshake: %v", flow, err)
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					payload := bytes.Repeat([]byte("proxy echo"), 256)
					if _, err := conn.Write(payload); err != nil {
						t.Fatal(err)
					}
					got := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
						t.Fatalf("flow %d echo: %v", flow, err)
					}
					if flow%2 == 0 {
						select {
						case body := <-cutDownloads:
							body.cut.Store(true)
							_ = body.ReadCloser.Close()
						case <-ctx.Done():
							t.Fatal("proxy did not expose the active download")
						}
						_, err := conn.Read(make([]byte, 1))
						var timeout net.Error
						if err == nil || errors.Is(err, context.Canceled) || errors.As(err, &timeout) && timeout.Timeout() {
							t.Fatalf("download failure was lost or detected only by the deadline: %v", err)
						}
						if flow == 0 {
							t.Logf("proxy %s surfaced as %v", ending, err)
						}
					}
					_ = conn.Close()
				}
			})
		}
	}
}

type browserProxyCutBody struct {
	io.ReadCloser
	cut   atomic.Bool
	clean bool
}

func (b *browserProxyCutBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.cut.Load() {
		if b.clean {
			return n, io.EOF
		}
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
