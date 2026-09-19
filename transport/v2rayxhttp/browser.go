//go:build with_cronet

package xhttp

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/sagernet/cronet-go"
	_ "github.com/sagernet/cronet-go/all"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func newBrowserClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	if options.Download != nil {
		return nil, E.New("xhttp: browser does not support separate download settings")
	}
	u := url.URL{Scheme: "http", Host: serverAddr.String()}
	opts := cronet.BrowserXHTTPOptions{
		Mode: options.Mode, Host: options.Host, Path: options.Path,
		Headers: make(http.Header), NoGRPCHeader: options.NoGRPCHeader, NoSSEHeader: options.NoSSEHeader,
		XPaddingBytes:        browserRange(options.XPaddingBytes),
		ScMaxEachPostBytes:   browserRange(options.ScMaxEachPostBytes),
		ScMinPostsIntervalMs: browserRange(options.ScMinPostsIntervalMs),
		ScMaxBufferedPosts:   options.GetNormalizedScMaxBufferedPosts(),
		DisableQUIC:          true,
		DialContext: func(ctx context.Context) (net.Conn, error) {
			return dialer.DialContext(ctx, N.NetworkTCP, serverAddr)
		},
	}
	for key, value := range options.Headers {
		opts.Headers.Set(key, value)
	}
	if opts.Host == "" {
		opts.Host = serverAddr.String()
	}
	if tlsConfig != nil {
		// The concrete TLS config must export an explicitly supported Cronet
		// configuration. A Go TLS connection is never put under Cronet.
		exporter, ok := tlsConfig.(interface {
			BrowserTLSConfig() (tls.BrowserTLSOptions, error)
		})
		if !ok {
			return nil, E.New("xhttp: browser requires standard TLS options; this TLS engine is unsupported")
		}
		browserTLS, err := exporter.BrowserTLSConfig()
		if err != nil {
			return nil, E.Cause(err, "xhttp browser TLS")
		}
		serverName := browserTLS.ServerName
		if serverName == "" {
			serverName = serverAddr.AddrString()
		}
		u.Scheme = "https"
		u.Host = net.JoinHostPort(serverName, strconv.Itoa(int(serverAddr.Port)))
		opts.TrustedRootCertificates = browserTLS.CertificatePEM
		opts.ECHConfigList = browserTLS.ECHConfigList
		opts.GetECHConfigList = browserTLS.GetECHConfigList
		if browserTLS.Reality != nil {
			opts.Reality = &cronet.BrowserRealityOptions{
				PublicKey: browserTLS.Reality.PublicKey,
				ShortID:   browserTLS.Reality.ShortID,
			}
		}
	}
	return cronet.NewBrowserXHTTPClient(ctx, u.String(), opts)
}

func browserRange(r option.V2RayXHTTPRange) cronet.BrowserXHTTPRange {
	return cronet.BrowserXHTTPRange{From: r.From, To: r.To}
}
