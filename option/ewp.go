package option

// EWPUser describes a single EWP v2 user. Name is informational
// (used for logs / metrics); UUID is the authentication credential.
type EWPUser struct {
	Name string `json:"name,omitempty"`
	UUID string `json:"uuid"`
}

// EWPInboundOptions configures an EWP v2 server-side listener.
//
// EWP runs on top of an arbitrary byte-stream transport (TLS, plus
// optionally a v2ray transport such as ws/grpc/httpupgrade) — the
// crypto/protocol layer itself does not pin a transport.
type EWPInboundOptions struct {
	ListenOptions
	Users []EWPUser `json:"users,omitempty"`
	InboundTLSOptionsContainer
	Multiplex *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions   `json:"transport,omitempty"`
}

// EWPOutboundOptions configures an EWP v2 client outbound. UUID is the
// pre-shared user credential. TLS / Transport mirror the VLESS shape.
type EWPOutboundOptions struct {
	DialerOptions
	ServerOptions
	UUID    string      `json:"uuid"`
	Network NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions    `json:"transport,omitempty"`
}
