#!/bin/sh
# Installs a released wayfinder-map into <this checkout>/bin, after checking
# its SHA-256 against the release's SHA256SUMS. Writes nothing outside this
# checkout. Needs gh, logged in.
#
#   ./install.sh [vX.Y.Z]    (the latest release if no version is given)
set -eu

repo=distrail-io/wayfinder-map
root=$(cd "$(dirname "$0")" && pwd -P)

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

command -v gh >/dev/null || fail "gh is required: https://cli.github.com"

case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "unsupported OS $(uname -s)" ;;
esac
case $(uname -m) in
arm64 | aarch64) arch=arm64 ;;
x86_64 | amd64) arch=amd64 ;;
*) fail "unsupported CPU $(uname -m)" ;;
esac
[ "$os/$arch" != darwin/amd64 ] ||
	fail "no darwin/amd64 build; build from source: go build -o bin/wayfinder-map ./cmd/wayfinder-map"
asset=wayfinder-map_${os}_${arch}

version=${1:-}
if [ -z "$version" ]; then
	version=$(gh release view --repo "$repo" --json tagName --jq .tagName)
fi
case $version in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) fail "bad version '$version': want vX.Y.Z" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $asset $version"
gh release download "$version" --repo "$repo" --dir "$tmp" --pattern "$asset" --pattern SHA256SUMS

echo "Checking SHA-256"
grep "  $asset\$" "$tmp/SHA256SUMS" >"$tmp/want" || fail "$asset is missing from SHA256SUMS"
if command -v sha256sum >/dev/null; then
	(cd "$tmp" && sha256sum -c want)
else
	(cd "$tmp" && shasum -a 256 -c want)
fi

if [ "$os" = darwin ]; then
	# Recorded for the release checklist: is the download quarantined?
	q=$(xattr -p com.apple.quarantine "$tmp/$asset" 2>/dev/null) || q=none
	echo "com.apple.quarantine: $q"
fi

mkdir -p "$root/bin"
chmod 755 "$tmp/$asset"
cp "$tmp/$asset" "$root/bin/.wayfinder-map.new"
mv "$root/bin/.wayfinder-map.new" "$root/bin/wayfinder-map"
echo "Installed $root/bin/wayfinder-map ($version)"
echo "To put it on your PATH: ln -s \"$root/bin/wayfinder-map\" ~/.local/bin/wayfinder-map"
