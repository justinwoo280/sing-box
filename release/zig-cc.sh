#!/usr/bin/env bash
set -euo pipefail

# Keep the target flags in CC (and therefore Go's cache key).
directories=()
next_directory=false
mips64=false
for arg in "$@"; do
  if $next_directory; then directories+=("$arg"); next_directory=false; fi
  case "$arg" in
    -L) next_directory=true ;;
    -L*) directories+=("${arg#-L}") ;;
    mips64*-linux-*) mips64=true ;;
  esac
done
args=()
for arg in "$@"; do
  # The target ABI already selects hard-float. N64 requires PIC with abicalls.
  if [[ "$arg" == -mhard-float ]]; then continue; fi
  if $mips64 && [[ "$arg" == -fno-PIC || "$arg" == -fno-pic ]]; then continue; fi
  # Zig 0.15.2 classifies -l:libcronet.a as a shared library even with -static.
  # Resolve exact archive names against the same -L search paths instead.
  if [[ "$arg" == -l:*.a ]]; then
    name=${arg#-l:}
    found=false
    for directory in "${directories[@]}"; do
      if [[ -f "$directory/$name" ]]; then
        arg="$directory/$name"
        found=true
        break
      fi
    done
    if ! $found; then echo "Cannot resolve static archive: $name" >&2; exit 1; fi
  fi
  args+=("$arg")
done
if $mips64; then
  # Zig defaults static musl to non-PIC, including CGO's preprocessing probes.
  # N64 still needs PIC code even when the final executable is fully static.
  args+=(-fPIC)
fi
exec zig cc "${args[@]}"
