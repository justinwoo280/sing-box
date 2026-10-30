#!/usr/bin/env bash
set -euo pipefail

# Keep the target flags in CC (and therefore Go's cache key). Go's MIPS CGO
# driver adds -mhard-float, which Zig rejects; its target ABI already selects it.
args=()
for arg in "$@"; do
  if [[ "$arg" != -mhard-float ]]; then args+=("$arg"); fi
done
exec zig cc "${args[@]}"
