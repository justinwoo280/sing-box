package xhttp

import (
	"context"
	stdTLS "crypto/tls"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type downloadECHDNS struct {
	adapter.DNSRouter
	config  []byte
	queries chan adapter.DNSQueryOptions
}

func (d *downloadECHDNS) Exchange(_ context.Context, request *mDNS.Msg, options adapter.DNSQueryOptions) (*mDNS.Msg, error) {
	if len(request.Question) != 1 || request.Question[0].Name != "download-ech.test." || request.Question[0].Qtype != mDNS.TypeHTTPS ||
		options.Transport == nil || options.Transport.Tag() != "loopback" || !options.DisableReverseMapping {
		return nil, fmt.Errorf("incorrect download ECH resolver: %v %+v", request.Question, options)
	}
	d.queries <- options
	response := new(mDNS.Msg)
	response.SetReply(request)
	response.Answer = []mDNS.RR{&mDNS.HTTPS{SVCB: mDNS.SVCB{
		Hdr:      mDNS.RR_Header{Name: request.Question[0].Name, Rrtype: mDNS.TypeHTTPS, Class: mDNS.ClassINET, Ttl: 300},
		Priority: 1, Target: ".", Value: []mDNS.SVCBKeyValue{&mDNS.SVCBECHConfig{ECH: d.config}},
	}}}
	return response, nil
}

func TestOrdinaryXHTTPDownloadECHResolver(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprintf("override=%v", override), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			configPEM, keyPEM, err := tls.ECHKeygenDefault("example.com")
			require.NoError(t, err)
			config, _ := pem.Decode([]byte(configPEM))
			keys, _ := pem.Decode([]byte(keyPEM))
			echKeys, err := tls.UnmarshalECHKeys(keys.Bytes)
			require.NoError(t, err)
			dns := &downloadECHDNS{config: config.Bytes, queries: make(chan adapter.DNSQueryOptions, 4)}
			ctx = service.ContextWith[adapter.DNSRouter](ctx, dns)
			ctx = service.ContextWith[adapter.DNSTransportManager](ctx, loopbackDNSManager{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || !r.TLS.ECHAccepted || r.TLS.ServerName != "example.com" || r.ProtoMajor != 2 || r.Method != http.MethodGet {
					t.Errorf("download did not use ECH over HTTP/2: %+v %s", r.TLS, r.Method)
				}
				_, _ = io.WriteString(w, "independent encrypted download")
			}))
			server.EnableHTTP2 = true
			server.TLS = &stdTLS.Config{EncryptedClientHelloKeys: echKeys}
			server.StartTLS()
			defer server.Close()
			certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			var dialOptions option.DialerOptions
			dialOptions.DomainResolver = &option.DomainResolveOptions{Server: "loopback"}
			resolver := ""
			if override {
				dialOptions.DomainResolver.Server = "upload-only"
				resolver = `"domain_resolver":"loopback",`
			}
			address := M.SocksaddrFromNet(server.Listener.Addr())
			options := decodeXHTTPOptions(t, fmt.Sprintf(`{"type":"xhttp","mode":"packet-up","download":{
				"server":"127.0.0.1","server_port":%d,%s "tls":{"enabled":true,"server_name":"example.com",
				"alpn":["h2"],"certificate":%q,"ech":{"enabled":true,"query_server_name":"download-ech.test"}}}}`, address.Port, resolver, certificate))
			client, err := NewClientWithDialerOptions(ctx, logger.NOP(), N.SystemDialer, &dialOptions, M.ParseSocksaddr("127.0.0.1:1"), options, nil)
			require.NoError(t, err)
			defer client.Close()
			conn, err := client.DialContext(ctx)
			require.NoError(t, err)
			defer conn.Close()
			body, err := io.ReadAll(conn)
			require.NoError(t, err)
			require.Equal(t, "independent encrypted download", string(body))
			require.Len(t, dns.queries, 1)
		})
	}
}
