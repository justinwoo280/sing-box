package outbound

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

type Adapter struct {
	outboundType string
	outboundTag  string
	network      []string
	dependencies []string
}

func NewAdapter(outboundType string, outboundTag string, network []string, dependencies []string) Adapter {
	return Adapter{
		outboundType: outboundType,
		outboundTag:  outboundTag,
		network:      network,
		dependencies: dependencies,
	}
}

func NewAdapterWithDialerOptions(outboundType string, outboundTag string, network []string, dialOptions option.DialerOptions) Adapter {
	var dependencies []string
	if dialOptions.Detour != "" {
		dependencies = []string{dialOptions.Detour}
	}
	return NewAdapter(outboundType, outboundTag, network, dependencies)
}

func NewAdapterWithTransportOptions(outboundType string, outboundTag string, network []string, dialOptions option.DialerOptions, transport *option.V2RayTransportOptions) Adapter {
	adapter := NewAdapterWithDialerOptions(outboundType, outboundTag, network, dialOptions)
	if transport != nil && transport.Type == C.V2RayTransportTypeXHTTP {
		if download := transport.XHTTPOptions.Download; download != nil && download.Detour != "" && download.Detour != dialOptions.Detour {
			adapter.dependencies = append(adapter.dependencies, download.Detour)
		}
	}
	return adapter
}

func (a *Adapter) Type() string {
	return a.outboundType
}

func (a *Adapter) Tag() string {
	return a.outboundTag
}

func (a *Adapter) Network() []string {
	return a.network
}

func (a *Adapter) Dependencies() []string {
	return a.dependencies
}
