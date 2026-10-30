package tun

import (
	"net/netip"
	"slices"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common"

	"go4.org/netipx"
)

// The pre-match path and Android route callback share an immutable snapshot.
// A rule-set update must not temporarily remove the bypass policy.
type routeAddressSets struct {
	include []*netipx.IPSet
	exclude []*netipx.IPSet
}

func (t *Inbound) refreshRouteAddressSets() {
	t.routeSets.Store(&routeAddressSets{
		include: common.FlatMap(t.routeRuleSet, adapter.RuleSet.ExtractIPSet),
		exclude: common.FlatMap(t.routeExcludeRuleSet, adapter.RuleSet.ExtractIPSet),
	})
}

func (t *Inbound) routeAddressSetPrefixes() (include []netip.Prefix, exclude []netip.Prefix, err error) {
	if sets := t.routeSets.Load(); sets != nil {
		include = common.FlatMap(sets.include, (*netipx.IPSet).Prefixes)
		exclude = common.FlatMap(sets.exclude, (*netipx.IPSet).Prefixes)
	}
	return
}

func (s *routeAddressSets) bypass(destination netip.Addr) bool {
	contains := func(set *netipx.IPSet) bool { return set.Contains(destination.Unmap()) }
	return slices.ContainsFunc(s.exclude, contains) ||
		(len(s.include) > 0 && !slices.ContainsFunc(s.include, contains))
}
