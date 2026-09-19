//go:build !with_cronet

package xhttp

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func newBrowserClient(context.Context, N.Dialer, M.Socksaddr, option.V2RayXHTTPOptions, tls.Config) (adapter.V2RayClientTransport, error) {
	return nil, E.New("xhttp: browser requires a build with with_cronet")
}
