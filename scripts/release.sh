#!/bin/bash
# Build official installer binaries, source plugin catalog, and checksums.
# Source deployments update by rebuilding tag sources with their local plugins.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
version=${1:-${LAOWANGBOT_VERSION:-${MIBOT_VERSION:-}}}
[[ -n "$version" ]] || { echo "Usage: bash scripts/release.sh <version>" >&2; exit 2; }
out="$root/dist"
rm -rf "$out"
mkdir -p "$out"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  suffix=""; [[ "$os" != windows ]] || suffix=.exe
  GOOS=$os GOARCH=$arch LAOWANGBOT_VERSION=$version bash "$root/scripts/build.sh" "$out/laowangbot-$os-$arch$suffix"
done

bash "$root/scripts/build-plugins.sh" "$version" "$out"

cd "$out"
if command -v sha256sum > /dev/null; then
  sha256sum laowangbot-* plugins-catalog.json > checksums.txt
else
  shasum -a 256 laowangbot-* plugins-catalog.json > checksums.txt
fi
cat checksums.txt
printf 'Upload every file in %s as assets of release %s\n' "$out" "$version"
