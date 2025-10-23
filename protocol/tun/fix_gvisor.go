//go:build with_gvisor

package tun

import (
	"context"
	"net/netip"
	"time"
	"unsafe"

	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/logger"
)

type gVisor struct {
	ctx                  context.Context
	tun                  any
	inet4LoopbackAddress []netip.Addr
	inet6LoopbackAddress []netip.Addr
	udpTimeout           time.Duration
	broadcastAddr        netip.Addr
	handler              any
	logger               logger.Logger
	stack                *stack.Stack
	endpoint             stack.LinkEndpoint
}

func (t *gVisor) Close() error {
	if t.stack == nil {
		return nil
	}
	t.endpoint.(*tun.LinkEndpointFilter).LinkEndpoint.Attach(nil)
	t.stack.Close()
	for _, endpoint := range t.stack.CleanupEndpoints() {
		endpoint.Abort()
	}
	return nil
}

func (t *Inbound) fixGvisorClose() {
	// 正确的修复方式：修改 sing-tun 里面的 func (w *LinkEndpointFilter) Attach
	if gvs, ok := t.tunStack.(*tun.GVisor); ok {
		p := (*gVisor)(unsafe.Pointer(gvs))
		p.Close()
		p.stack = nil // prevent next Close
	}
}
