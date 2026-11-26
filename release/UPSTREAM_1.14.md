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

Cronet uses the published Browser-capable fork wrapper and native modules at
`v0.0.0-20261126220904-c67c5dc12708`, paired with
`all@v0.0.0-20261126220912-ffdf7fe53517`. They were generated from wrapper
`4d330abc1184ced5a21b20cdc011795acf615fb8` and native
`e214d4134b35a0f0e362a68d151cbcb39f289ef2` after full publication run
[45896719250](https://github.com/justinwoo280/cronet-go/actions/runs/45896719250)
passed the native, desktop integration and Android runtime gates.

Selective upstream backports cover asynchronous custom-socket scheduling,
dialer-owned Naive address resolution and reset cancellation, zero-initialized
Go C-header arrays, BSD UDP EOF handling and Windows TCP metrics synchronization.
Browser REALITY/Strict ECH, explicit ALPN/H3 and empty-Host/SNI behavior remain
part of the fork. Chrome Root Store v45 and the eMudhra SPKI blocklist were
already present. The actual native source remains
`naiveproxy-REALITY/naiveproxy:cronet-go-chromium-154`, with SagerNet fixes
integrated individually.

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
