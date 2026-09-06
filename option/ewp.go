package option

// EWPUser describes a single EWP user. Name is informational
// (used for logs / metrics); UUID is the authentication credential.
type EWPUser struct {
	Name string `json:"name,omitempty"`
	UUID string `json:"uuid"`
}

// EWPInboundOptions configures an EWP/v2.3 server-side listener.
//
// EWP runs on top of an arbitrary byte-stream transport (TLS, plus
// optionally a v2ray transport such as ws/grpc/httpupgrade) — the
// crypto/protocol layer itself does not pin a transport.
//
// SigningPrivateKey is the server's Ed25519 signing identity, base64-encoded
// (64-byte private key). Generate with `sing-box generate ewp-keypair`.
// ServerID names this listener in the v2.3 transcript and in every signed
// short-term outer key; clients must configure the identical value.
type EWPInboundOptions struct {
	ListenOptions
	Users                  []EWPUser `json:"users,omitempty"`
	ServerID               string    `json:"server_id"`
	RouteEpoch             uint64    `json:"route_epoch,omitempty"`
	SigningPrivateKey      string    `json:"signing_private_key"`
	InboundTLSOptionsContainer
	Multiplex *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions   `json:"transport,omitempty"`
}

// EWPOutboundOptions configures an EWP/v2.3 client outbound. UUID is the
// pre-shared user credential. ServerPublicKey pins the server's Ed25519
// signing identity (base64-encoded 32-byte public key, the public
// counterpart of the server's signing_private_key). ServerID must match
// the server's server_id exactly. RouteEpoch is 0 unless the operator
// rotates route tags.
type EWPOutboundOptions struct {
	DialerOptions
	ServerOptions
	UUID            string      `json:"uuid"`
	ServerPublicKey string      `json:"server_public_key"`
	ServerID        string      `json:"server_id"`
	RouteEpoch      uint64      `json:"route_epoch,omitempty"`
	Network         NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions    `json:"transport,omitempty"`
}
