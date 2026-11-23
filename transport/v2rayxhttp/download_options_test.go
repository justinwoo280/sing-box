package xhttp

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"

	"github.com/stretchr/testify/require"
)

type loopbackDNS struct {
	adapter.DNSRouter
	lookups chan string
}

func (d loopbackDNS) Lookup(_ context.Context, name string, options adapter.DNSQueryOptions) ([]netip.Addr, error) {
	if name != "download.test" || options.Transport == nil || options.Transport.Tag() != "loopback" {
		return nil, fmt.Errorf("unexpected lookup: %s", name)
	}
	d.lookups <- name
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

type loopbackDNSTransport struct{ adapter.DNSTransport }

func (loopbackDNSTransport) Tag() string { return "loopback" }

type loopbackDNSManager struct{ adapter.DNSTransportManager }

func (loopbackDNSManager) Transport(tag string) (adapter.DNSTransport, bool) {
	return loopbackDNSTransport{}, tag == "loopback"
}

func TestOrdinaryXHTTPIndependentDownloadJSON(t *testing.T) {
	for _, overrideResolver := range []bool{false, true} {
		name := "inherit outbound resolver"
		if overrideResolver {
			name = "download resolver override"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			lookups := make(chan string, 2)
			ctx = service.ContextWith[adapter.DNSRouter](ctx, loopbackDNS{lookups: lookups})
			ctx = service.ContextWith[adapter.DNSTransportManager](ctx, loopbackDNSManager{})

			type packet struct {
				session string
				payload []byte
			}
			posted := make(chan packet, 1)
			upload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS != nil || r.ProtoMajor != 1 || r.Method != http.MethodPut || r.Host != "up.test" ||
					r.URL.Path != "/upload/" || r.URL.Query().Get("route") != "up" || r.URL.Query().Get("seq") != "0" {
					t.Errorf("bad upload: %s %s %s %s", r.Proto, r.Method, r.Host, r.RequestURI)
				}
				if r.Header.Get("Authorization") != "Bearer loopback-upload" || r.Header.Get("Up-Pad") != strings.Repeat("X", 23) || r.Header.Get("Down-Pad") != "" {
					t.Error("upload got the wrong headers or padding")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					t.Errorf("expected empty upload body, got %d bytes: %v", len(body), err)
				}
				first, last := r.Header.Get("Data-0"), r.Header.Get("Data-1")
				if len(first) != 64 || len(last) == 0 || r.Header.Get("Data-2") != "" {
					t.Errorf("expected uplink chunks of 64 and remainder, got %d and %d", len(first), len(last))
				}
				decoded, err := base64.RawURLEncoding.DecodeString(first + last)
				if err != nil {
					t.Errorf("decode header uplink: %v", err)
				}
				posted <- packet{session: r.Header.Get("Up-Session"), payload: decoded}
			}))
			defer upload.Close()

			download := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || r.TLS.ServerName != "example.com" || r.ProtoMajor != 2 || r.Method != http.MethodGet || r.Host != "down.test" ||
					r.URL.Path != "/download/" || r.URL.Query().Get("route") != "down" || r.URL.Query().Has("seq") {
					t.Errorf("bad download: %s %s %s %s", r.Proto, r.Method, r.Host, r.RequestURI)
				}
				padURL, err := url.Parse(r.Header.Get("Down-Pad"))
				if err != nil || padURL.Query().Get("pad") != strings.Repeat("X", 17) || padURL.Query().Get("route") != "down" {
					t.Errorf("incorrect download padding: %q, %v", r.Header.Get("Down-Pad"), err)
				}
				if r.Header.Get("Authorization") != "Bearer loopback-download" || r.Header.Get("Up-Session") != "" || r.Header.Get("Up-Pad") != "" || r.Referer() != "" {
					t.Error("download got the wrong headers or padding")
				}
				select {
				case up := <-posted:
					cookie, err := r.Cookie("down_sid")
					if err != nil || cookie.Value != up.session || len(up.session) != 12 || strings.Trim(up.session, "0123456789") != "" {
						t.Errorf("session ID mismatch: upload %q, download %v (%v)", up.session, cookie, err)
					}
					_, _ = w.Write(up.payload)
				case <-r.Context().Done():
				}
			}))
			download.EnableHTTP2 = true
			download.StartTLS()
			defer download.Close()

			downAddr := M.SocksaddrFromNet(download.Listener.Addr())
			cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: download.Certificate().Raw})
			var dialOptions option.DialerOptions
			var resolver string
			if overrideResolver {
				resolver = `"domain_resolver":"loopback",`
			} else {
				dialOptions.DomainResolver = &option.DomainResolveOptions{Server: "loopback"}
			}
			options := decodeXHTTPOptions(t, fmt.Sprintf(`{
				"type":"xhttp", "mode":"stream-down", "host":"up.test", "path":"/upload?route=up",
				"headers":{"Authorization":"Bearer loopback-upload"}, "uplink_http_method":"PUT",
				"x_padding_obfs_mode":true, "x_padding_header":"Up-Pad", "x_padding_bytes":"23",
				"session_placement":"header", "session_key":"Up-Session", "session_id_table":"number", "session_id_length":12,
				"seq_placement":"query", "seq_key":"seq", "uplink_data_placement":"header", "uplink_data_key":"Data",
				"uplink_chunk_size":{"from":64,"to":64}, "xmux":{"max_concurrency":"2-9"},
				"download":{
					"server":"download.test", "server_port":%d, %s
					"tls":{"enabled":true,"server_name":"example.com","alpn":["h2"],"certificate":%q},
					"host":"down.test", "path":"download?route=down", "headers":{"Authorization":"Bearer loopback-download"},
					"x_padding_placement":"queryInHeader", "x_padding_header":"Down-Pad", "x_padding_key":"pad", "x_padding_bytes":17,
					"session_placement":"cookie", "session_key":"down_sid", "xmux":{"max_concurrency":1,"h_keep_alive_period":-1}
				}
			}`, downAddr.Port, resolver, cert))
			upAddr := M.SocksaddrFromNet(upload.Listener.Addr())
			upDialer, err := dialer.New(ctx, dialOptions, false)
			require.NoError(t, err)
			client, err := NewClientWithDialerOptions(ctx, logger.NOP(), upDialer, &dialOptions, upAddr, options, nil)
			require.NoError(t, err)
			defer client.Close()
			conn, err := client.DialContext(ctx)
			require.NoError(t, err)
			defer conn.Close()
			payload := strings.Repeat("XHTTP-wire-", 6)
			_, err = conn.Write([]byte(payload))
			require.NoError(t, err)
			body := make([]byte, len(payload))
			_, err = io.ReadFull(conn, body)
			require.NoError(t, err)
			require.Equal(t, payload, string(body))
			require.Len(t, lookups, 1)
			require.Equal(t, "download.test", <-lookups)
		})
	}
}

func TestOrdinaryXHTTPSameEndpointDownloadOverrides(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	upload := make(chan []byte, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Host != "example.com" || r.TLS == nil {
			t.Errorf("download URL override changed shared TLS/host: %s %s", r.Proto, r.Host)
		}
		switch r.Method {
		case http.MethodGet:
			ref, err := url.Parse(r.Referer())
			if err != nil || len(ref.Query().Get("x_padding")) < 100 || r.Header.Get("Auth-Token") != "" || r.Header.Get("X-Only-Up") != "" || r.URL.Query().Get("route") != "down" {
				t.Error("explicit false, zero and empty headers did not reset the download")
			}
			select {
			case body := <-upload:
				_, _ = w.Write(body)
			case <-r.Context().Done():
			}
		case http.MethodPost:
			if r.Header.Get("Auth-Token") != strings.Repeat("X", 20) || r.Header.Get("X-Only-Up") != "yes" || r.Referer() != "" || r.URL.Query().Get("route") != "up" {
				t.Error("download reset leaked to the upload")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			upload <- body
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	address := M.SocksaddrFromNet(server.Listener.Addr())
	config, err := tls.NewClient(ctx, logger.NOP(), address.AddrString(), option.OutboundTLSOptions{
		Enabled: true, ServerName: "example.com", ALPN: []string{"h2"},
		Certificate: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))},
	})
	require.NoError(t, err)
	options := decodeXHTTPOptions(t, `{
		"type":"xhttp", "mode":"packet-up", "path":"/upload?route=up",
		"headers":{"X-Only-Up":"yes"}, "x_padding_obfs_mode":true, "x_padding_header":"Auth-Token", "x_padding_bytes":20,
		"download":{"path":"/download?route=down", "headers":{}, "x_padding_obfs_mode":false, "x_padding_bytes":0, "xmux":{}}
	}`)
	client, err := NewClient(ctx, N.SystemDialer, address, options, config)
	require.NoError(t, err)
	defer client.Close()
	conn, err := client.DialContext(ctx)
	require.NoError(t, err)
	defer conn.Close()
	const payload = "same address and TLS, different wire settings"
	_, err = conn.Write([]byte(payload))
	require.NoError(t, err)
	got := make([]byte, len(payload))
	_, err = io.ReadFull(conn, got)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
}

type testDownloadOutbound struct {
	adapter.Outbound
	address M.Socksaddr
}

func (o testDownloadOutbound) DialContext(ctx context.Context, network string, address M.Socksaddr) (net.Conn, error) {
	if address != o.address {
		return nil, fmt.Errorf("unexpected download detour destination: %v", address)
	}
	return N.SystemDialer.DialContext(ctx, network, address)
}

type testDownloadOutboundManager struct {
	adapter.OutboundManager
	detour adapter.Outbound
}

func (m *testDownloadOutboundManager) Outbound(tag string) (adapter.Outbound, bool) {
	return m.detour, tag == "registered-later" && m.detour != nil
}

func TestOrdinaryXHTTPDownloadDetourIsLazy(t *testing.T) {
	manager := new(testDownloadOutboundManager)
	ctx, cancel := context.WithTimeout(service.ContextWith[adapter.OutboundManager](context.Background(), manager), 5*time.Second)
	defer cancel()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.TLS != nil || !strings.HasPrefix(r.URL.Path, "/download/") {
			t.Errorf("unexpected download request: %s %s", r.Method, r.RequestURI)
		}
		_, _ = io.WriteString(w, "download through a later outbound")
	}))
	defer down.Close()
	dlAddr := M.SocksaddrFromNet(down.Listener.Addr())
	options := decodeXHTTPOptions(t, fmt.Sprintf(`{"type":"xhttp","download":{"server":"127.0.0.1","server_port":%d,"detour":"registered-later","path":"/download"}}`, dlAddr.Port))
	// A separately specified server without download.tls uses cleartext even
	// if the upload transport is TLS. No upload is needed in this test.
	upTLS, err := tls.NewClient(ctx, logger.NOP(), "127.0.0.1", option.OutboundTLSOptions{Enabled: true, ServerName: "upload.test"})
	require.NoError(t, err)
	client, err := NewClient(ctx, N.SystemDialer, M.ParseSocksaddr("127.0.0.1:1"), options, upTLS)
	require.NoError(t, err)
	defer client.Close()
	manager.detour = testDownloadOutbound{address: dlAddr}
	conn, err := client.DialContext(ctx)
	require.NoError(t, err)
	defer conn.Close()
	body, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, "download through a later outbound", string(body))
}
