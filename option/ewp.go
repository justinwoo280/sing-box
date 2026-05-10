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
//
// ServerStaticPrivateKey, when non-empty, enables EWP/v2.1 by binding
// the handshake KDF to a long-term server X25519 identity (closes
// audit findings S1, S2, H2). Format: base64-encoded 32-byte X25519
// scalar. Generate with `sing-box generate ewp-keypair`.
type EWPInboundOptions struct {
	ListenOptions
	Users                  []EWPUser `json:"users,omitempty"`
	ServerStaticPrivateKey string    `json:"server_static_private_key,omitempty"`
	InboundTLSOptionsContainer
	Multiplex *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions   `json:"transport,omitempty"`
}

// EWPOutboundOptions configures an EWP v2 client outbound. UUID is the
// pre-shared user credential. TLS / Transport mirror the VLESS shape.
//
// ServerStaticPublicKey, when non-empty, enables EWP/v2.1 by pinning
// the long-term server X25519 identity. The value is the base64
// encoding of the genuine server's 32-byte X25519 public key (the
// public counterpart to ServerStaticPrivateKey on the server side).
// REQUIRED when talking to an EWP/v2.1 server; v2.1 servers REJECT
// clients that did not bind to their identity.
type EWPOutboundOptions struct {
	DialerOptions
	ServerOptions
	UUID                  string      `json:"uuid"`
	ServerStaticPublicKey string      `json:"server_static_public_key,omitempty"`
	Network               NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions    `json:"transport,omitempty"`
}
