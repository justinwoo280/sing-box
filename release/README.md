# Native release profiles

All builds use the immutable sing-tun, sing and sing-lwip versions in `go.mod`.
The sing-lwip binding pins its platform archives and the native-source lock pins
the lwIP Git commit and tree hash. Shared libraries are copied from that module
graph, never from a moving release URL. `go` is the default TUN stack; users opt
into lwIP with `"stack": "lwip"`.

| Profile | Platforms | lwIP binding |
|---|---|---|
| Desktop | Windows 386/amd64/arm64; macOS amd64/arm64 | PureGo + packaged shared library |
| Linux glibc | 386 SSE2, amd64, ARMv7, arm64, ppc64le, riscv64, loong64 | PureGo + packaged shared library |
| Linux musl / OpenWrt | Matrix in `.github/workflows/build.yml` | CGO, fully static executable |
| Android CLI | 386, amd64, ARMv7, arm64; API 21 | Source-CGO with the target NDK |
| Android AAR | Standard API 24 / legacy API 21 | Prebuilt-CGO / source-CGO, linked into libbox |
| Legacy desktop | Windows 7; older ARM, 386 softfloat, MIPS, s390x | Existing Go-only build, without lwIP |

The musl archives have a `-musl` suffix and are the inputs to OpenWrt packages.
They are built separately from glibc desktop binaries. ARMv5/v6, 386 softfloat,
big-endian MIPS and soft-float MIPS compile native sources for their CPU/ABI;
they do not reuse the ARMv7/hard-float archives. Cronet is enabled on the modern
targets explicitly marked in the matrix; static profiles use its musl archives.

## Build

Run `bash release/build-native.sh` from the repository root with `GOOS`, `GOARCH`
and one of these modes:

- `LWIP_MODE=dynamic`: `CGO_ENABLED=0`, `with_lwip,with_purego`.
- `LWIP_MODE=static`: `CGO_ENABLED=1`, `with_lwip,with_lwip_prebuilt`, target `CC`.
- `LWIP_MODE=source`: `CGO_ENABLED=1`, `with_lwip`, target `CC`; prepares the
  locked source module using `cmd/internal/prepare_lwip` and a temporary modfile.
- `LWIP_MODE=disabled`: the legacy Go-only profile.

Set `LWIP_LIBC=musl` for Linux musl; this also enables `with_musl,netgo,osusergo`
and external static linking. Set `WITH_CRONET=true` only on a matching Cronet
target. `with_purego` is global: a static profile cannot enable that tag.
`BUILD_TAGS`, `VERSION` and `OUTPUT_DIR` override the base tags, version and output
directory. Use a fresh output directory for each profile.

`release/zig-cc.sh` adapts Go's MIPS compiler arguments to Zig. Keep all target/CPU
flags in `CC` so Go's build cache distinguishes ABI variants. CI uses Zig 0.15.2.
ARMv5 Zig builds also need `gcc-arm-linux-gnueabi` (or `ARMV5_LIBGCC` pointing to
its `libgcc.a`) for Linux kernel-helper atomics used by Go's CGO runtime.
Source preparation requires Git and network access on its first fetch.

## Packaging

- Windows: `sing-lwip.dll` beside `sing-box.exe`.
- macOS: `libsing-lwip.dylib` beside `sing-box`.
- Linux glibc: `libsing-lwip.so` beside `sing-box`; DEB/RPM/Pacman install both
  in `/usr/bin`, matching the loader's executable-relative path.
- Static profiles: no separate lwIP runtime library.

Every native-enabled profile includes `licenses/sing-lwip/` with licenses and
build provenance. Package managers install it under `/usr/share/licenses/sing-box`.
The release upload job generates `SHA256SUMS` for the final archives and packages.
Update the binding and native libraries together. `SING_LWIP_LIBRARY` can override
the dynamic library path; the loader checks the native ABI when lwIP is started.

## Verify

The native release workflow builds desktop and static profiles and exercises the
binding on supported host runners. Linux smoke tests run the actual packaged CLI
in a fresh network namespace, checking IPv4/IPv6 TCP half-close, UDP fragmentation
and TCP/UDP DNS interception. For example:

```sh
sudo python3 release/smoke-tun.py ./dist/sing-box --stack lwip
sudo env SING_LWIP_LIBRARY=/nonexistent/unused.so \
  python3 release/smoke-tun.py ./dist/sing-box --stack default
```

OpenWrt packaging rejects executables with an ELF interpreter or `DT_NEEDED`
dependencies. Cross-builds establish compile/link compatibility; they do not
replace runtime validation on the target device.
