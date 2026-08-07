package ewp

import (
	"context"
	"net"

	M "github.com/sagernet/sing/common/metadata"

	sewp "github.com/justinwoo280/sing-ewp"
)

// ewpClient is the surface common to *sewp.Client (v2.0) and
// *sewp.ClientV21 (v2.1), so the outbound code can switch between
// them based on whether ServerStaticPublicKey is configured without
// duplicating the dial pipeline.
type ewpClient interface {
	DialConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.Conn, error)
	DialPacketConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.PacketConn, error)
}

// ewpService is the surface common to *sewp.Service (v2.0) and
// *sewp.ServiceV21 (v2.1), letting the inbound code accept either
// without branching on the concrete type at every call site.
type ewpService interface {
	AddUser(uuidStr string) error
	HandleConn(ctx context.Context, conn net.Conn) error
	Close() error
}

// socksaddrToEWP converts sing-box's metadata Socksaddr into the
// EWP-native Address (which preserves FQDN destinations).
func socksaddrToEWP(s M.Socksaddr) sewp.Address {
	if s.IsFqdn() {
		return sewp.Address{Domain: s.Fqdn, Port: s.Port}
	}
	return sewp.Address{Addr: s.AddrPort()}
}

// ewpToSocksaddr converts an EWP Address back to a sing-box Socksaddr
// suitable for InboundContext.Destination.
func ewpToSocksaddr(a sewp.Address) M.Socksaddr {
	if a.Domain != "" {
		return M.Socksaddr{Fqdn: a.Domain, Port: a.Port}
	}
	if a.Addr.IsValid() {
		return M.SocksaddrFromNetIP(a.Addr)
	}
	return M.Socksaddr{}
}
