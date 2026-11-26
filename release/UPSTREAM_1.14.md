# 1.14 maintenance integration

The `1.14.x` branch integrates upstream
`stable@8dbc70becf76363ebc25cce47a58165f0466ea3a` (2026-11-26), including
`v1.14.4@c005f35300ce76427dc285481ec5695f5b179cd2` and subsequent maintenance
fixes. This is a post-v1.14.4 source snapshot, not an upstream release tag.

## Retained extensions

- Ordinary and Browser XHTTP, independent download transports and
  presence-aware JSON import/format/merge semantics.
- Browser REALITY, Strict ECH and explicit ALPN; an empty HTTP Host still
  defaults to the configured endpoint rather than the TLS SNI.
- EWP, conntrack, selector callbacks and the SekaiMod Android platform adapter.
- Default Go TUN and selectable system/gVisor/mixed/lwIP stacks, including
  callback-based auto-redirect route snapshots and Android VPNService support.

EWP and private callers use the upstream scoped lifecycle. Constructor contexts
belong to the Box scope, so closing an unstarted instance or failing construction
also cancels native transports allocated during configuration validation.

Dynamic ECH uses upstream resolver selection and shared, per-query-name cache
state. Browser exports use the same resolver/cache; independent download TLS
inherits the outbound resolver unless its download options override it.

## Dependency selection

The root, integration-test and SekaiMod libcore module graphs use immutable
pseudo-versions. The custom TUN replacement is
`github.com/justinwoo280/sing-tun v0.9.7-0.20261126133200-ff0e24508fc9`, paired
with `github.com/sagernet/sing v0.9.7-0.20261125122036-9b11dbe28a7d`. This sing
dev revision includes the maintenance main line and the packet-offload and
memory-limit APIs needed by the Go stack. Published lwIP archives remain at
`c447d5af480c` with bindings `0dca048bf681`.

Cronet retains the published Browser-capable fork wrapper/native pair
`9586d9103f19`, with `all@49cf5150931d`. Its Chromium 154 Browser extensions
are required by this fork. Upstream's `ccefd17767e7` wrapper and
`2767273ce7ab` archives cannot be substituted directly: they lack these custom
APIs. Upstream's newer custom-socket worker scheduling, native certificate
blocklist and Go C-header zero-initialization changes still require a separate
Cronet reconciliation and matching publication. Updating the sing-box baseline
does not claim those Cronet native fixes are included.

## Validation

Build and test in the prepared SekaiMod Box after sourcing
`/workspace/home/sekaimod-env.sh`. Record both parent commits, the pending source
hash and dependency graph. Use published Cronet/lwIP archives.

Acceptance coverage includes root tests; ordinary/Browser XHTTP, EWP, TLS/ECH,
REALITY, reset/cancellation/half-close regressions under the race detector;
the integration-test module; libcore configuration and protocol fixtures;
and the four Android ABI build. `TestUnshareNamespace` requires permission to
create a network namespace, which the Box does not provide; this environment
limitation must be reported separately from the passing suites.
