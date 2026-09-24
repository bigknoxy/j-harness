#!/usr/bin/env sh
# j-harness installer.
#
#   curl -fsSL https://raw.githubusercontent.com/bigknoxy/j-harness/main/install.sh | sh
#
# Installs the `harness` binary and a starter agent registry. Overridable:
#   VERSION=v1.2.3   pin a release (default: latest)
#   PREFIX=$HOME/.local   install prefix (default: ~/.local)
#   BINDIR=$HOME/bin      where the binary goes (default: $PREFIX/bin)
#   REGISTRY_DIR=...      where the starter registry goes (default: ~/.config/j-harness/registry)
set -eu

REPO="bigknoxy/j-harness"
VERSION="${VERSION:-latest}"
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
REGISTRY_DIR="${REGISTRY_DIR:-$HOME/.config/j-harness/registry}"

say() { printf '%s\n' "$*"; }
err() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}
need uname
need curl
need tar

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) err "unsupported OS: $os (use the release archive manually)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac

asset="j-harness_${os}_${arch}.tar.gz"
if [ "$os" = windows ]; then asset="j-harness_${os}_${arch}.zip"; fi

if [ "$VERSION" = latest ]; then
  base="https://github.com/${REPO}/releases/latest/download"
else
  base="https://github.com/${REPO}/releases/download/${VERSION}"
fi
url="${base}/${asset}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "j-harness installer"
say "  os/arch : ${os}/${arch}"
say "  version : ${VERSION}"
say "  binary  : ${BINDIR}/harness"
say ""

say "downloading ${asset}..."
curl -fsSL "$url" -o "$tmp/$asset" || err "download failed: $url"

if curl -fsSL "${base}/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  ( cd "$tmp"
    want=$(grep " ${asset}\$" checksums.txt | awk '{print $1}')
    if [ -n "$want" ]; then
      if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$asset" | awk '{print $1}')
      else got=$(shasum -a 256 "$asset" | awk '{print $1}'); fi
      [ "$want" = "$got" ] || err "checksum mismatch for ${asset}"
      say "checksum ok"
    fi
  )
fi

mkdir -p "$tmp/extract"
case "$asset" in
  *.zip) need unzip; unzip -q "$tmp/$asset" -d "$tmp/extract" ;;
  *) tar -xzf "$tmp/$asset" -C "$tmp/extract" ;;
esac

bin=$(find "$tmp/extract" -type f -name harness | head -n 1)
[ -n "$bin" ] || err "harness binary not found in archive"

mkdir -p "$BINDIR"
install -m 0755 "$bin" "$BINDIR/harness"

say "fetching starter registry..."
mkdir -p "$REGISTRY_DIR"
ref="${VERSION}"
[ "$ref" = latest ] && ref=main
curl -fsSL "https://codeload.github.com/${REPO}/tar.gz/refs/heads/${ref}" -o "$tmp/src.tgz" 2>/dev/null \
  || curl -fsSL "https://codeload.github.com/${REPO}/tar.gz/refs/tags/${ref}" -o "$tmp/src.tgz" \
  || err "could not fetch registry source"
mkdir -p "$tmp/src"
tar -xzf "$tmp/src.tgz" -C "$tmp/src" --strip-components=1
cp -R "$tmp/src/agent-registry/." "$REGISTRY_DIR/"
[ -d "$tmp/src/schemas" ] && cp -R "$tmp/src/schemas" "$REGISTRY_DIR/../schemas" 2>/dev/null || true

say ""
say "installed:"
say "  harness  -> ${BINDIR}/harness"
say "  registry -> ${REGISTRY_DIR}"
say ""
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) say "note: add ${BINDIR} to PATH:"
     say "  export PATH=\"${BINDIR}:\$PATH\"" ;;
esac
say "run it:"
say "  HARNESS_REGISTRY=\"${REGISTRY_DIR}\" harness --addr 127.0.0.1:8080"
say ""
say "uninstall:"
say "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/uninstall.sh | sh"
