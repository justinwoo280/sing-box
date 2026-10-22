package tls

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/certificate"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"
)

// Android always registers the system store, even with no certificate overrides.
func TestBrowserSystemCertificateStore(t *testing.T) {
	store, err := certificate.NewStore(context.Background(), logger.NOP(), option.CertificateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := service.ContextWith[adapter.CertificateStore](context.Background(), store)
	config, err := NewSTDClient(ctx, logger.NOP(), "browser.test", option.OutboundTLSOptions{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []Config{config, config.Clone()} {
		client := config.(*STDClientConfig)
		if client.config.RootCAs == nil {
			t.Fatal("test must exercise the non-nil Android system certificate pool")
		}
		exported, err := client.BrowserTLSConfig()
		if err != nil {
			t.Fatal(err)
		}
		if exported.ServerName != "browser.test" {
			t.Fatalf("unexpected default certificate configuration: %+v", exported)
		}
		if runtime.GOOS == "android" {
			if !exported.UseTrustedRootCertificates || exported.CertificatePEM == "" {
				t.Fatalf("Android system roots were not exported: %+v", exported)
			}
		} else if exported.UseTrustedRootCertificates || exported.CertificatePEM != "" {
			t.Fatalf("unexpected explicit default roots: %+v", exported)
		}
		// Replacing the pool must not silently inherit system-store semantics.
		client.config.RootCAs = x509.NewCertPool()
		if _, err := client.BrowserTLSConfig(); err == nil {
			t.Fatal("custom certificate pool was ignored")
		}
	}
}

func TestBrowserCustomCertificateStore(t *testing.T) {
	_, rootPEM, err := GenerateCertificate(nil, nil, time.Now, "browser.test", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		options option.CertificateOptions
		wantPEM bool
	}{
		{name: "none", options: option.CertificateOptions{Store: "none"}},
		{name: "none-custom", options: option.CertificateOptions{Store: "none", Certificate: badoption.Listable[string]{string(rootPEM)}}, wantPEM: true},
		{name: "mozilla", options: option.CertificateOptions{Store: "mozilla"}, wantPEM: true},
		{name: "chrome", options: option.CertificateOptions{Store: "chrome"}, wantPEM: true},
		{name: "system", options: option.CertificateOptions{Store: "system", CertificateDirectoryPath: []string{t.TempDir()}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := certificate.NewStore(context.Background(), logger.NOP(), test.options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			ctx := service.ContextWith[adapter.CertificateStore](context.Background(), store)
			config, err := NewSTDClient(ctx, logger.NOP(), "browser.test", option.OutboundTLSOptions{Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			exported, err := config.(*STDClientConfig).BrowserTLSConfig()
			if test.name == "system" && runtime.GOOS != "android" {
				if err == nil || !strings.Contains(err.Error(), "custom certificate store") {
					t.Fatalf("custom trust policy was ignored: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !exported.UseTrustedRootCertificates {
				t.Fatalf("trust policy was not exported: %+v", exported)
			}
			wantPEM := test.wantPEM || (test.name == "system" && runtime.GOOS == "android")
			if (exported.CertificatePEM != "") != wantPEM {
				t.Fatalf("certificate PEM = %q", exported.CertificatePEM)
			}
		})
	}
}

func TestBrowserOutboundCertificate(t *testing.T) {
	_, certificatePEM, err := GenerateCertificate(nil, nil, time.Now, "browser.test", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewSTDClient(context.Background(), logger.NOP(), "browser.test", option.OutboundTLSOptions{
		Enabled:     true,
		Certificate: badoption.Listable[string]{string(certificatePEM)},
	})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := config.(*STDClientConfig).BrowserTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !exported.UseTrustedRootCertificates || exported.CertificatePEM != string(certificatePEM) {
		t.Fatalf("outbound roots = %+v", exported)
	}
}

func TestBrowserTLSOptions(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   option.OutboundTLSOptions
		wantError string
	}{
		{"default", option.OutboundTLSOptions{}, ""},
		{"insecure", option.OutboundTLSOptions{Insecure: true}, "insecure"},
		{"disable-sni", option.OutboundTLSOptions{DisableSNI: true}, "disable_sni"},
		{"fragment", option.OutboundTLSOptions{Fragment: true}, "fragmentation"},
		{"version", option.OutboundTLSOptions{MinVersion: "1.3"}, "version"},
		{"alpn-h3", option.OutboundTLSOptions{ALPN: []string{"h3"}}, "ALPN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.options.Enabled = true
			config, err := NewClient(context.Background(), logger.NOP(), "browser.test", test.options)
			if err != nil {
				t.Fatal(err)
			}
			exporter := config.(*STDClientConfig)
			options, err := exporter.BrowserTLSConfig()
			if test.wantError == "" {
				if err != nil || options.ServerName != "browser.test" {
					t.Fatalf("export = %q, %v", options.ServerName, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type browserECHDNS struct {
	adapter.DNSRouter
	exchange func(context.Context, *mDNS.Msg) (*mDNS.Msg, error)
}

func (d *browserECHDNS) Exchange(ctx context.Context, request *mDNS.Msg, _ adapter.DNSQueryOptions) (*mDNS.Msg, error) {
	return d.exchange(ctx, request)
}

func TestBrowserECHRefresh(t *testing.T) {
	var queries int
	wire := []byte{0, 1, 2}
	var lookupError error
	ctx := service.ContextWith[adapter.DNSRouter](context.Background(), &browserECHDNS{exchange: func(_ context.Context, request *mDNS.Msg) (*mDNS.Msg, error) {
		queries++
		if request.Question[0].Name != "ech-query.test." || request.Question[0].Qtype != mDNS.TypeHTTPS {
			t.Fatalf("question = %v", request.Question)
		}
		if lookupError != nil {
			return nil, lookupError
		}
		response := new(mDNS.Msg)
		response.SetReply(request)
		if wire != nil {
			response.Answer = []mDNS.RR{&mDNS.HTTPS{SVCB: mDNS.SVCB{
				Hdr:      mDNS.RR_Header{Name: request.Question[0].Name, Rrtype: mDNS.TypeHTTPS, Class: mDNS.ClassINET, Ttl: 300},
				Priority: 1, Target: ".", Value: []mDNS.SVCBKeyValue{&mDNS.SVCBECHConfig{ECH: wire}},
			}}}
		}
		return response, nil
	}})
	config, err := NewClient(ctx, logger.NOP(), "inner.test", option.OutboundTLSOptions{Enabled: true, ECH: &option.OutboundECHOptions{Enabled: true, QueryServerName: "ech-query.test"}})
	if err != nil {
		t.Fatal(err)
	}
	ech := config.(*ECHClientConfig)
	options, err := ech.BrowserTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if queries != 0 {
		t.Fatal("lookup during construction")
	}
	for i := 0; i < 2; i++ {
		got, err := options.GetECHConfigList(ctx)
		if err != nil || !bytes.Equal(got, wire) {
			t.Fatalf("config = %v, %v", got, err)
		}
		got[0] = 99 // The exported slice must not mutate the cached config.
	}
	if queries != 1 {
		t.Fatalf("cached queries = %d", queries)
	}
	ech.lastUpdate = time.Now().Add(-time.Hour)
	wire = []byte{0, 3, 4}
	got, err := options.GetECHConfigList(ctx)
	if err != nil || !bytes.Equal(got, wire) || queries != 2 {
		t.Fatalf("refresh = %v, %v, queries=%d", got, err, queries)
	}
	ech.lastUpdate = time.Now().Add(-time.Hour)
	lookupError = errors.New("DNS unavailable")
	if _, err := options.GetECHConfigList(ctx); !errors.Is(err, lookupError) {
		t.Fatalf("lookup error = %v", err)
	}
	lookupError = nil
	wire = nil
	if _, err := options.GetECHConfigList(ctx); err == nil {
		t.Fatal("stale ECH config reused after record removal")
	}
}
