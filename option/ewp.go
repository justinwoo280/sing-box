package option

type EWPUser struct {
	Name string `json:"name,omitempty"`
	UUID string `json:"uuid"`
}

type EWPInboundOptions struct {
	ListenOptions
	Users               []EWPUser `json:"users,omitempty"`
	ServerID            string    `json:"server_id"`
	RouteEpoch          uint64    `json:"route_epoch,omitempty"`
	SigningPrivateKey   string    `json:"signing_private_key"`
	EarlyServerFinished bool      `json:"early_server_finished,omitempty"`
	InboundTLSOptionsContainer
	Multiplex *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions   `json:"transport,omitempty"`
}

type EWPOutboundOptions struct {
	DialerOptions
	ServerOptions
	UUID                string      `json:"uuid"`
	ServerPublicKey     string      `json:"server_public_key"`
	ServerID            string      `json:"server_id"`
	RouteEpoch          uint64      `json:"route_epoch,omitempty"`
	EarlyServerFinished bool        `json:"early_server_finished,omitempty"`
	Network             NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions    `json:"transport,omitempty"`
}
