#!/usr/bin/env bash
# Reproducible release binaries and SHA256SUMS for one tag.
set -euo pipefail
version="${1:?usage: scripts/release.sh <version>}"
out="dist/$version"; mkdir -p "$out"
for target in linux/amd64 windows/amd64 darwin/arm64; do
  os="${target%/*}"; arch="${target#*/}"; ext=""; [ "$os" = windows ] && ext=".exe"
  for pair in "firelight:./cmd/firelight" "firelight-apply:./cmd/firelight-apply" "nql:./cmd/firelight" "nql-apply:./cmd/firelight-apply"; do
    name="${pair%%:*}"; pkg="${pair#*:}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
      -ldflags "-s -w -buildid=" -o "$out/$name-$os-$arch$ext" "$pkg"
  done
done
go version > "$out/GO_VERSION"
(cd "$out" && sha256sum firelight* nql* GO_VERSION > SHA256SUMS)
