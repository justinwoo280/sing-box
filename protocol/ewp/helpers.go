package ewp

import (
	M "github.com/sagernet/sing/common/metadata"

	sewp "github.com/justinwoo280/sing-ewp"
)

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
