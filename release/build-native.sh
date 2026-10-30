#!/usr/bin/env bash
# Build a release executable and stage its matching native runtime and notices.
# Run from the repository root. All libraries come from the locked module graph.
set -euo pipefail
export GOWORK=off

: "${GOOS:?set GOOS}"
: "${GOARCH:?set GOARCH}"
mode=${LWIP_MODE:-dynamic}
libc=${LWIP_LIBC:-gnu}
out=${OUTPUT_DIR:-dist}
tags=${BUILD_TAGS:-with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,with_clash_api,with_tailscale}
ldflags="$(cat release/LDFLAGS) -s -w -buildid= -X github.com/sagernet/sing-box/constant.Version=${VERSION:-unknown}"
modflags=(-mod=readonly)
module=github.com/justinwoo280/sing-lwip
host_go() { env -u GOOS -u GOARCH -u GOARM -u GO386 -u GOMIPS -u GOMIPS64 -u CC CGO_ENABLED=0 go "$@"; }
mkdir -p "$out/licenses/sing-lwip"
work=''
trap 'if [[ -n "$work" ]]; then rm -rf "$work"; fi' EXIT

case "$mode" in
  dynamic|static)
    if [[ "$GOARCH" == arm && "${GOARM:-7}" != 7 ]]; then
      echo 'Published lwIP ARM libraries require GOARM=7; use source mode for older CPUs.' >&2
      exit 1
    fi
    if [[ "$GOARCH" == mips* && "${GOMIPS:-${GOMIPS64:-hardfloat}}" != hardfloat ]]; then
      echo 'Published lwIP MIPS libraries require hardfloat; use source mode for softfloat.' >&2
      exit 1
    fi
    target="${GOOS}_${GOARCH}"
    if [[ "$GOOS" == linux ]]; then target="${target}_${libc}"; fi
    library_module="$module/lib/$target"
    host_go mod download "$library_module"
    directory=$(host_go list -m -f '{{.Dir}}' "$library_module")
    cp "$directory/LICENSE" "$directory/LWIP-LICENSE" "$out/licenses/sing-lwip/"
    cp "$directory/manifest.json" "$out/licenses/sing-lwip/native-build.json"
    if [[ "$mode" == dynamic ]]; then
      export CGO_ENABLED=0
      tags="$tags,with_lwip,with_purego"
      case "$GOOS" in
        windows) library=sing-lwip.dll ;;
        darwin) library=libsing-lwip.dylib ;;
        linux) library=libsing-lwip.so ;;
        *) echo "Unsupported dynamic target: $GOOS" >&2; exit 1 ;;
      esac
      cp "$directory/$library" "$out/"
      if [[ "$GOOS/$GOARCH" == linux/arm ]]; then
        ldflags="$ldflags -I /lib/ld-linux-armhf.so.3"
      elif [[ "$GOOS/$GOARCH" == linux/riscv64 ]]; then
        ldflags="$ldflags -I /lib/ld-linux-riscv64-lp64d.so.1"
      fi
    else
      export CGO_ENABLED=1
      tags="$tags,with_lwip,with_lwip_prebuilt"
    fi
    ;;
  source)
    export CGO_ENABLED=1
    tags="$tags,with_lwip"
    work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/sing-box-lwip.XXXXXX")
    host_go run ./cmd/internal/prepare_lwip -dir "$work"
    modflags=(-mod=readonly "-modfile=$work/source.mod")
    cp "$work/source/LICENSE" "$out/licenses/sing-lwip/"
    cp "$work/source/third_party/lwip/LICENSE" "$out/licenses/sing-lwip/LWIP-LICENSE"
    cp "$work/source/third_party/lwip.lock.json" "$out/licenses/sing-lwip/native-source.json"
    ;;
  disabled)
    export CGO_ENABLED=0
    rmdir "$out/licenses/sing-lwip" "$out/licenses"
    ;;
  *) echo "Unknown LWIP_MODE: $mode" >&2; exit 1 ;;
esac

if [[ "$mode" == static || "$mode" == source ]]; then
  if [[ ",$tags," == *,with_purego,* ]]; then
    echo 'Static lwIP must not use the global with_purego build tag.' >&2
    exit 1
  fi
  : "${CC:?set a target C compiler for CGO}"
  if [[ "$GOOS/$GOARCH/${GOARM:-}" == linux/arm/5 && "$CC" == *zig* ]]; then
    # ARMv5 has no LDREX/STREX. GCC's Linux runtime supplies the kernel-helper
    # atomics required by runtime/cgo; Zig 0.15.2 does not provide these symbols.
    libgcc=${ARMV5_LIBGCC:-$(arm-linux-gnueabi-gcc -print-libgcc-file-name)}
    test -f "$libgcc"
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} $libgcc -Wl,--gc-sections"
  fi
  if [[ "$GOOS" == linux && "$libc" == musl ]]; then
    tags="$tags,with_musl,netgo,osusergo"
    ldflags="$ldflags -linkmode external -extldflags -static"
  fi
fi

if [[ "${WITH_CRONET:-false}" == true ]]; then
  tags="$tags,with_cronet"
  if [[ "$CGO_ENABLED" == 0 ]]; then
    tags="$tags,with_purego"
    cronet_module="github.com/sagernet/cronet-go/lib/${GOOS}_${GOARCH}"
    host_go mod download "$cronet_module"
    cronet_dir=$(host_go list -m -f '{{.Dir}}' "$cronet_module")
    case "$GOOS" in
      windows) cp "$cronet_dir/libcronet.dll" "$out/" ;;
      linux) cp "$cronet_dir/libcronet.so" "$out/" ;;
    esac
  fi
fi

go build "${modflags[@]}" -trimpath -tags "$tags" -ldflags "$ldflags" -o "$out/sing-box" ./cmd/sing-box
if [[ -n "${LWIP_TEST_BINARY:-}" ]]; then
  go test "${modflags[@]}" -c -tags "$tags" -ldflags "$ldflags" -o "$LWIP_TEST_BINARY" github.com/sagernet/sing-tun
fi
if [[ "$mode" != disabled ]]; then
  {
    echo "binding=$mode"
    echo "target=$GOOS/$GOARCH"
    echo "libc=$libc"
    echo "GOARM=${GOARM:-} GO386=${GO386:-} GOMIPS=${GOMIPS:-} GOMIPS64=${GOMIPS64:-}"
    host_go list -m -f '{{.Path}} {{.Version}}' "$module"
    if [[ -n "${library_module:-}" ]]; then host_go list -m -f '{{.Path}} {{.Version}}' "$library_module"; fi
  } > "$out/licenses/sing-lwip/build.txt"
fi
