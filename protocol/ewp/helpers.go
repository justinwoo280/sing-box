package ewp

import (
	"context"
	"net"
	"time"

	M "github.com/sagernet/sing/common/metadata"

	sewp "github.com/justinwoo280/sing-ewp"
)

const ewpHandshakeTimeout = 10 * time.Second

type ewpClient interface {
	DialConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.Conn, error)
	DialPacketConn(ctx context.Context, conn net.Conn, dst sewp.Address) (net.PacketConn, error)
	SetTicketStore(store sewp.V23TicketStore)
}

func socksaddrToEWP(s M.Socksaddr) sewp.Address {
	if s.IsFqdn() {
		return sewp.Address{Domain: s.Fqdn, Port: s.Port}
	}
	return sewp.Address{Addr: s.AddrPort()}
}

func ewpToSocksaddr(a sewp.Address) M.Socksaddr {
	if a.Domain != "" {
		return M.Socksaddr{Fqdn: a.Domain, Port: a.Port}
	}
	if a.Addr.IsValid() {
		return M.SocksaddrFromNetIP(a.Addr)
	}
	return M.Socksaddr{}
}
