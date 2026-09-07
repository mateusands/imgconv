#!/usr/bin/env bash
# Build the release binaries for every supported platform.
#
# This script is the ONE place that knows the target list. The release workflow
# calls it rather than repeating the matrix in YAML: two copies of a build matrix
# drift, and the one that drifts is the one nobody runs by hand.
#
# CGO is off on purpose. It is what makes every target build from one machine with
# no cross-compiler, and what makes each output a single file that runs on a
# system with nothing installed. A dependency needing cgo would end that, which is
# why the GUI is served to a browser instead of using a native toolkit.
set -euo pipefail

VERSION="${1:-dev}"
OUT="${OUT:-dist}"

TARGETS=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/arm64
)

rm -rf "$OUT"
mkdir -p "$OUT"

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  name="imgconv-${VERSION}-${os}-${arch}${ext}"

  echo "building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" -o "$OUT/$name" ./cmd/imgconv
done

# Checksums, so whoever downloads one can tell it is the file this built.
( cd "$OUT" && sha256sum ./* > SHA256SUMS )

echo
ls -lh "$OUT"
