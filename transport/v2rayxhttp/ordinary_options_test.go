package xhttp

import (
	"context"
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
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2/hpack"
)

func decodeXHTTPOptions(t *testing.T, raw string) option.V2RayXHTTPOptions {
	t.Helper()
	var transport option.V2RayTransportOptions
	require.NoError(t, json.UnmarshalDisallowUnknownFields([]byte(raw), &transport))
	return transport.XHTTPOptions
}

func TestOrdinaryXHTTPAdvancedJSONProfile(t *testing.T) {
	testOrdinaryXHTTPAdvancedJSONProfile(t, nil)
}

func testOrdinaryXHTTPAdvancedJSONProfile(t *testing.T, utls *option.OutboundUTLSOptions) {
	t.Helper()
	options := decodeXHTTPOptions(t, `{
		"type":"xhttp", "mode":"stream-up", "host":"profile.test", "path":"/profile?route=one",
		"headers":{"User-Agent":"grpc-node-js/1.14.3","Authorization":"Digest loopback-test","X-Multi":["one","two"]},
		"x_padding_bytes":64, "x_padding_obfs_mode":true, "x_padding_placement":"header",
		"x_padding_header":"Auth-Token", "x_padding_key":"_key", "x_padding_method":"tokenish",
		"xmux":{"max_concurrency":"2-9"}
	}`)
	optionsForLibrary := buildOptions(&options.V2RayXHTTPBaseOptions)
	require.EqualValues(t, 2, optionsForLibrary.Xmux.MaxConcurrency.From)
	require.EqualValues(t, 9, optionsForLibrary.Xmux.MaxConcurrency.To)
	require.False(t, optionsForLibrary.GRPCFraming)

	const payload = "stream-up payload without gRPC framing"
	getPath := make(chan string, 1)
	postPath := make(chan string, 1)
	upload := make(chan []byte, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Host != "profile.test" || r.URL.Query().Get("route") != "one" {
			t.Errorf("incorrect request route: %s %s %s", r.Proto, r.Host, r.RequestURI)
		}
		if r.UserAgent() != "grpc-node-js/1.14.3" || r.Header.Get("Authorization") != "Digest loopback-test" {
			t.Error("configured User-Agent or Authorization was not sent")
		}
		if values := r.Header.Values("X-Multi"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
			t.Errorf("header values = %q", values)
		}
		padding := r.Header.Get("Auth-Token")
		if size := hpack.HuffmanEncodeLength(padding); size < 62 || size > 66 || strings.HasPrefix(padding, "_key") ||
			strings.Trim(padding, "X") == "" || r.Header.Get("_key") != "" || r.Referer() != "" {
			t.Errorf("wrong tokenish padding (Huffman length %d)", size)
		}
		switch r.Method {
		case http.MethodGet:
			getPath <- r.URL.Path
			select {
			case body := <-upload:
				_, _ = w.Write(body)
			case <-r.Context().Done():
			}
		case http.MethodPost:
			body := make([]byte, len(payload))
			if _, err := io.ReadFull(r.Body, body); err != nil {
				t.Errorf("read upload: %v", err)
				return
			}
			postPath <- r.URL.Path
			upload <- body
		default:
			t.Errorf("unexpected request: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	address := M.SocksaddrFromNet(server.Listener.Addr())
	config, err := tls.NewClient(ctx, logger.NOP(), address.AddrString(), option.OutboundTLSOptions{
		Enabled: true, ServerName: "example.com", ALPN: []string{"h2", "http/1.1"},
		Certificate: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))},
		UTLS:        utls,
	})
	require.NoError(t, err)
	client, err := NewClient(ctx, N.SystemDialer, address, options, config)
	require.NoError(t, err)
	defer client.Close()
	conn, err := client.DialContext(ctx)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(payload))
	require.NoError(t, err)
	received := make([]byte, len(payload))
	_, err = io.ReadFull(conn, received)
	require.NoError(t, err)
	require.Equal(t, payload, string(received))
	get, post := <-getPath, <-postPath
	require.Equal(t, get, post)
	require.True(t, strings.HasPrefix(get, "/profile/") && len(get) > len("/profile/"), "session path: %s", get)
}

func TestXHTTPInvalidOptions(t *testing.T) {
	address := M.ParseSocksaddr("127.0.0.1:1")
	for _, test := range []struct{ json, error string }{
		{`"browser":true,"download":{}`, "browser does not support separate download"},
		{`"browser":true,"x_padding_obfs_mode":true`, "browser does not support x_padding_obfs_mode"},
		{`"browser":true,"session_placement":"cookie"`, "browser does not support session_placement"},
		{`"mode":"stream-one","download":{}`, "stream-one does not support separate download"},
		{`"download":{"server":"down.test"}`, "download.server and download.server_port"},
		{`"download":{"server_port":443}`, "download.server and download.server_port"},
		{`"download":{"x_padding_obfs_mode":true,"x_padding_method":"unknown"}`, "invalid x_padding_method"},
		{`"uplink_data_placement":"header"`, "uplink_data_key is required"},
		{`"uplink_http_method":"bad method"`, "invalid uplink HTTP method"},
		{`"headers":{"Authorization":"value\r\ninvalid"}`, "invalid value for header Authorization"},
		{`"session_placement":"header","seq_placement":"header","session_key":"X-Id","seq_key":"x-id"`, "share placement"},
	} {
		t.Run(test.error, func(t *testing.T) {
			options := decodeXHTTPOptions(t, `{"type":"xhttp",`+test.json+`}`)
			_, err := NewClient(context.Background(), N.SystemDialer, address, options, nil)
			require.ErrorContains(t, err, test.error)
		})
	}
	for _, field := range []string{`"sc_max_buffered_posts":4294967297`, `"xmux":{"h_keep_alive_period":4294967297}`, `"server_max_header_bytes":2147483648`} {
		var transport option.V2RayTransportOptions
		require.Error(t, json.Unmarshal([]byte(`{"type":"xhttp",`+field+`}`), &transport), field)
	}

	_, err := NewServer(context.Background(), logger.NOP(), decodeXHTTPOptions(t, `{"type":"xhttp","download":{}}`), nil, nil)
	require.ErrorContains(t, err, "download is a client-only option")
}

func TestOrdinaryXHTTPInboundHeadersJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	options := decodeXHTTPOptions(t, `{
		"type":"xhttp", "path":"/inbound", "headers":{"X-Multi":["one","two"]}, "server_max_header_bytes":512
	}`)
	server, err := NewServer(ctx, logger.NOP(), options, nil, nil)
	require.NoError(t, err)
	defer server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()
	// Use a fresh connection for both probes. The HTTP/1 keep-alive reader
	// can buffer the start of a later request before the header limit resets.
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodOptions, "http://"+listener.Addr().String()+"/inbound/", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, []string{"one", "two"}, response.Header.Values("X-Multi"))

	request, err = http.NewRequestWithContext(ctx, http.MethodOptions, request.URL.String(), nil)
	require.NoError(t, err)
	request.Header.Set("X-Large", strings.Repeat("x", 7*1024))
	response, err = client.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusRequestHeaderFieldsTooLarge, response.StatusCode)
}
