#!/bin/bash
# Build the release artifacts .update expects: one binary per platform and a
# checksums.txt. `.update run` refuses a release with no checksum file, so
# publishing without this script produces a release nothing will install.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
version=${1:-${MIBOT_VERSION:-}}
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

cd "$out"
if command -v sha256sum > /dev/null; then
  sha256sum laowangbot-* > checksums.txt
else
  shasum -a 256 laowangbot-* > checksums.txt
fi
cat checksums.txt
printf 'Upload every file in %s as assets of release %s\n' "$out" "$version"
