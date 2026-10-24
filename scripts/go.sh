#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOCACHE="${GOCACHE:-$PWD/.cache/go-build}"
export GOPATH="${GOPATH:-$PWD/.cache/gopath}"
export GOTMPDIR="${GOTMPDIR:-$PWD/.cache/tmp}"
export GOTOOLCHAIN=local
mkdir -p "$GOCACHE" "$GOPATH" "$GOTMPDIR"
if command -v go >/dev/null 2>&1; then
  exec go "$@"
fi
exec "$PWD/.tools/go/bin/go" "$@"
