package ewp

import (
	"context"
	"net"

	M "github.com/sagernet/sing/common/metadata"

	sewp "github.com/justinwoo280/sing-ewp"
)

// ewpClient is the client surface used by the outbound: both dial methods
// take a byte-stream carrier and add v2.3 framing internally.
type ewpClient interface {
	DialConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.Conn, error)
	DialPacketConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.PacketConn, error)
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
