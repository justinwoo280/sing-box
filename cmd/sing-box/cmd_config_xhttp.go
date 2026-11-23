package main

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// The generic format/merge helpers prune empty objects. An XHTTP download
// object is a presence-sensitive override, so keep that subtree intact. Both
// helpers preserve outbound order; merge concatenates the outbound lists.
func preserveXHTTPDownloads(outbounds []option.Outbound, original []option.Outbound) {
	if len(outbounds) != len(original) {
		return
	}
	for i, entry := range original {
		source := outboundXHTTPOptions(entry)
		if source == nil || source.Download == nil || entry.Tag != outbounds[i].Tag || entry.Type != outbounds[i].Type {
			continue
		}
		if destination := outboundXHTTPOptions(outbounds[i]); destination != nil {
			destination.Download = source.Download
		}
	}
}

func outboundXHTTPOptions(outbound option.Outbound) *option.V2RayXHTTPOptions {
	var transport *option.V2RayTransportOptions
	switch options := outbound.Options.(type) {
	case *option.VLESSOutboundOptions:
		transport = options.Transport
	case *option.VMessOutboundOptions:
		transport = options.Transport
	case *option.TrojanOutboundOptions:
		transport = options.Transport
	case *option.EWPOutboundOptions:
		transport = options.Transport
	}
	if transport == nil || transport.Type != C.V2RayTransportTypeXHTTP {
		return nil
	}
	return &transport.XHTTPOptions
}
