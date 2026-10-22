//go:build with_cronet

package xhttp

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

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
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Download != nil {
		return nil, E.New("xhttp: browser does not support separate download settings")
	}
	u := url.URL{Scheme: "http", Host: serverAddr.String()}
	opts := cronet.BrowserXHTTPOptions{
		Mode: options.Mode, Host: options.Host, Path: options.Path,
		Headers: make(http.Header), NoGRPCHeader: options.NoGRPCHeader, NoSSEHeader: options.NoSSEHeader,
		GRPCFraming:          options.GRPCFraming,
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
		opts.UseTrustedRootCertificates = browserTLS.UseTrustedRootCertificates
		opts.ECHConfigList = browserTLS.ECHConfigList
		opts.GetECHConfigList = browserTLS.GetECHConfigList
		if browserTLS.Reality != nil {
			opts.Reality = &cronet.BrowserRealityOptions{
				PublicKey: browserTLS.Reality.PublicKey,
				ShortID:   browserTLS.Reality.ShortID,
			}
		}
	}
	create := func() (*cronet.BrowserXHTTPClient, error) {
		return cronet.NewBrowserXHTTPClient(ctx, u.String(), opts)
	}
	client, err := create()
	if err != nil {
		return nil, err
	}
	return &browserClient{ctx: ctx, create: create, client: client}, nil
}

// A network update discards the old native pool. Close permanently releases the
// transport; Reset leaves it available for a fresh engine on the next dial.
type browserClient struct {
	ctx    context.Context
	create func() (*cronet.BrowserXHTTPClient, error)
	mu     sync.Mutex
	client *cronet.BrowserXHTTPClient
	closed bool
}

func (c *browserClient) DialContext(ctx context.Context) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, net.ErrClosed
	}
	if err := c.ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if c.client == nil {
		var err error
		c.client, err = c.create()
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	client := c.client
	c.mu.Unlock()
	return client.DialContext(ctx)
}

func (c *browserClient) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeClient()
}

func (c *browserClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.closeClient()
}

func (c *browserClient) closeClient() error {
	if c.client == nil {
		return nil
	}
	client := c.client
	c.client = nil
	return client.Close()
}

func browserRange(r option.V2RayXHTTPRange) cronet.BrowserXHTTPRange {
	return cronet.BrowserXHTTPRange{From: r.From, To: r.To}
}
