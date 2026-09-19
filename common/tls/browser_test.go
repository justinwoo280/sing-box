package tls

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"
)

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
