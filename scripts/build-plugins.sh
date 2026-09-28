#!/usr/bin/env bash
# Catalog source files; plugins are compiled into the host at installation.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd -P)
version=${1:?Usage: build-plugins.sh TAG [OUTPUT]}
out=${2:-$root/dist}; mkdir -p "$out"; out=$(cd "$out" && pwd -P)
cd "$root"
go run ./cmd/plugin-catalog --tag "$version" --output "$out/plugins-catalog.json"
