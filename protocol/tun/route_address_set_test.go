package tun

import (
	"net/netip"
	"slices"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/logger"

	"go4.org/netipx"
)

type testRouteRuleSet struct {
	adapter.RuleSet
	sets []*netipx.IPSet
}

func (s *testRouteRuleSet) ExtractIPSet() []*netipx.IPSet { return s.sets }

type testRouteRedirect struct {
	tun.AutoRedirect
	onUpdate func()
}

func (r *testRouteRedirect) UpdateRouteAddressSet() error { r.onUpdate(); return nil }

func TestAutoRedirectRuleSetSnapshot(t *testing.T) {
	makeSet := func(prefixes ...string) *netipx.IPSet {
		t.Helper()
		var builder netipx.IPSetBuilder
		for _, prefix := range prefixes {
			builder.AddPrefix(netip.MustParsePrefix(prefix))
		}
		set, err := builder.IPSet()
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	include := &testRouteRuleSet{sets: []*netipx.IPSet{makeSet("198.18.0.0/24", "fd00::/64")}}
	exclude := &testRouteRuleSet{sets: []*netipx.IPSet{makeSet("198.18.0.53/32", "fd00::53/128")}}
	inbound := &Inbound{
		logger: logger.NOP(), routeRuleSet: []adapter.RuleSet{include}, routeExcludeRuleSet: []adapter.RuleSet{exclude},
		// DNS interception provides a recognizable verdict for included destinations.
		dnsHijackAddress: []netip.Addr{netip.MustParseAddr("198.18.0.1"), netip.MustParseAddr("fd00::1")},
	}
	inbound.refreshRouteAddressSets()
	handler := (*autoRedirectHandler)(inbound)
	for _, address := range []string{"198.18.0.1", "fd00::1", "198.18.0.53", "fd00::53", "203.0.113.1", "fd01::1"} {
		addr := netip.MustParseAddr(address)
		want := tun.ActionBypass
		if slices.Contains(inbound.dnsHijackAddress, addr) {
			want = tun.ActionHijackDNS
		}
		if got := handler.JudgeFlow(17, netip.AddrPort{}, netip.AddrPortFrom(addr, 53), nil).Action; got != want {
			t.Fatalf("%s: action %v, want %v", address, got, want)
		}
	}
	old := inbound.routeSets.Load()
	exclude.sets = []*netipx.IPSet{makeSet("198.18.0.1/32", "fd00::1/128")}
	inbound.autoRedirect = &testRouteRedirect{onUpdate: func() {
		_, excluded, err := inbound.routeAddressSetPrefixes()
		if err != nil || !slices.Contains(excluded, netip.MustParsePrefix("fd00::1/128")) {
			t.Fatalf("route callback did not see the new snapshot: %v, %v", excluded, err)
		}
	}}
	var readers sync.WaitGroup
	readers.Go(func() {
		for range 100 {
			inbound.routeAddressSetPrefixes()
			handler.JudgeFlow(17, netip.AddrPort{}, netip.MustParseAddrPort("198.18.0.1:53"), nil)
		}
	})
	inbound.updateRouteAddressSet(exclude)
	readers.Wait()
	if old.bypass(netip.MustParseAddr("198.18.0.1")) || !inbound.routeSets.Load().bypass(netip.MustParseAddr("198.18.0.1")) {
		t.Fatal("rule-set update mutated the old snapshot or lost the new bypass policy")
	}
	inbound.routeSetClosed = true
	exclude.sets = nil
	inbound.updateRouteAddressSet(exclude)
	if !inbound.routeSets.Load().bypass(netip.MustParseAddr("198.18.0.1")) {
		t.Fatal("late callback changed a closed inbound")
	}
}
