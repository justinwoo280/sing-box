package ewp

import (
	"context"
	stdTLS "crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2/hpack"
)

func TestEWPOrdinaryXHTTPIndependentDownload(t *testing.T) {
	testEWPXHTTPIndependentDownload(t, "")
}

func testEWPXHTTPIndependentDownload(t *testing.T, fingerprint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cert, key := ewpXHTTPTestCertificate(t)
	serverTLS := fmt.Sprintf(`,"tls":{"enabled":true,"alpn":["h2"],"certificate":%q,"key":%q}`, cert, key)
	const originTransport = `{
		"type":"xhttp","mode":"stream-up","host":"ewp.test","path":"/ewp",
		"session_placement":"header","session_key":"X-Origin-Session"
	}`
	origin, publicKey, router := startEWPXHTTPTestInbound(t, ctx, originTransport, serverTLS, true)
	originURL := &url.URL{Scheme: "https", Host: origin.String()}
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(cert)))
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &stdTLS.Config{RootCAs: roots, ServerName: ewpXHTTPHost},
	}
	t.Cleanup(transport.CloseIdleConnections)
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		ErrorLog:  log.New(io.Discard, "", 0),
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(originURL)
			r.Out.URL.Path = "/ewp/"
			r.Out.URL.RawQuery = ""
			r.Out.Host = ewpXHTTPHost
			// Both frontends map to a single real EWP/XHTTP origin. Their
			// incoming metadata differs, but they must forward the same ID.
			r.Out.Header.Set("Referer", originURL.String()+"/ewp/?x_padding="+strings.Repeat("X", 128))
		},
	}
	var getCount, postCount atomic.Int32
	upload := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := r.Header.Get("X-Up-Session")
		token := r.Header.Get("Auth-Token")
		padSize := hpack.HuffmanEncodeLength(token)
		if r.Method != http.MethodPost || r.ProtoMajor != 2 || r.TLS == nil || r.TLS.ServerName != "example.com" ||
			r.Host != "upload.test" || r.URL.Path != "/up/" || r.URL.Query().Get("route") != "upload" || sid == "" ||
			r.UserAgent() != "grpc-node-js/1.14.3" || r.Header.Get("Authorization") != "Digest loopback-test" ||
			padSize < 46 || padSize > 50 || strings.Trim(token, "X") == "" || r.Header.Get("_key") != "" || r.Referer() != "" {
			t.Errorf("unexpected EWP XHTTP uplink: %s %s %s %s (padding bytes %d)", r.Proto, r.Method, r.Host, r.RequestURI, padSize)
			http.Error(w, "unexpected upload", http.StatusBadRequest)
			return
		}
		postCount.Add(1)
		r.Header.Set("X-Origin-Session", sid)
		proxy.ServeHTTP(w, r)
	}))
	upload.EnableHTTP2 = true
	upload.StartTLS()
	t.Cleanup(upload.Close)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("dl_sid")
		referer, err := url.Parse(r.Referer())
		if r.Method != http.MethodGet || r.ProtoMajor != 1 || r.TLS != nil || r.Host != "download.test" ||
			r.URL.Path != "/down/" || r.URL.Query().Get("route") != "download" || sid == "" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Auth-Token") != "" || r.Header.Get("X-Up-Session") != "" ||
			r.UserAgent() == "grpc-node-js/1.14.3" || err != nil || referer.Query().Get("x_padding") != strings.Repeat("X", 24) {
			t.Errorf("unexpected EWP XHTTP downlink: %s %s %s %s", r.Proto, r.Method, r.Host, r.RequestURI)
			http.Error(w, "unexpected download", http.StatusBadRequest)
			return
		}
		getCount.Add(1)
		r.Header.Set("X-Origin-Session", sid)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(down.Close)
	downAddr := M.SocksaddrFromNet(down.Listener.Addr())
	upAddr := M.SocksaddrFromNet(upload.Listener.Addr())
	clientTransport := fmt.Sprintf(`{
		"type":"xhttp", "mode":"stream-up", "host":"upload.test", "path":"/up?route=upload",
		"headers":{"User-Agent":"grpc-node-js/1.14.3","Authorization":"Digest loopback-test"},
		"x_padding_obfs_mode":true,"x_padding_placement":"header","x_padding_method":"tokenish",
		"x_padding_header":"Auth-Token","x_padding_key":"_key","x_padding_bytes":48,
		"session_placement":"header","session_key":"X-Up-Session","xmux":{"max_concurrency":"2-9"},
		"download":{
			"server":%q,"server_port":%d,"tls":{"enabled":false},"host":"download.test","path":"/down?route=download",
			"headers":{},"session_placement":"query","session_key":"dl_sid",
			"x_padding_obfs_mode":false,"x_padding_bytes":"24","xmux":{}
		}
	}`, downAddr.AddrString(), downAddr.Port)
	upCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upload.Certificate().Raw})
	var utls string
	if fingerprint != "" {
		utls = fmt.Sprintf(`,"utls":{"enabled":true,"fingerprint":%q}`, fingerprint)
	}
	clientTLS := fmt.Sprintf(`,"tls":{"enabled":true,"alpn":["h2","http/1.1"],"server_name":"example.com","certificate":%q%s}`, upCert, utls)
	client := newEWPXHTTPTestOutbound(t, ctx, upAddr, publicKey, clientTransport, clientTLS, true)
	checkEWPXHTTPFlows(t, ctx, client, router)
	require.GreaterOrEqual(t, postCount.Load(), int32(3))
	require.GreaterOrEqual(t, getCount.Load(), int32(3))
}
