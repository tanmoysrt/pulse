#!/bin/sh
# Install (or update) pulse from the latest GitHub release.
#   curl -fsSL https://raw.githubusercontent.com/tanmoysrt/pulse/master/install.sh | sh
set -eu

REPO="tanmoysrt/pulse"
BIN="pulse"
INSTALL_DIR="/usr/bin"

fail() { echo "pulse: $1" >&2; exit 1; }

os=$(uname -s)
case "$os" in
	Linux)  os="linux" ;;
	Darwin) os="darwin" ;;
	*)      fail "unsupported OS: $os (linux and macOS only)" ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64|amd64)  arch="amd64" ;;
	arm64|aarch64) arch="arm64" ;;
	*)             fail "unsupported architecture: $arch (amd64 and arm64 only)" ;;
esac

asset="${BIN}-${os}-${arch}"
url="https://github.com/${REPO}/releases/latest/download/${asset}"
target="${INSTALL_DIR}/${BIN}"

# Elevate only if we can't write to the install dir ourselves.
sudo=""
if [ ! -w "$INSTALL_DIR" ]; then
	command -v sudo >/dev/null 2>&1 || fail "need write access to $INSTALL_DIR (run as root or install sudo)"
	sudo="sudo"
fi

# Already installed? Ask before replacing it. Read from the terminal, since
# stdin is the piped script when run via `curl | sh`.
if command -v "$BIN" >/dev/null 2>&1; then
	current=$("$BIN" --version 2>/dev/null || echo "unknown")
	printf "pulse %s is already installed. Update to the latest release? [y/N] " "$current"
	if [ -r /dev/tty ]; then read -r ans </dev/tty; else read -r ans; fi
	case "$ans" in
		y|Y|yes|YES) ;;
		*) echo "pulse: leaving the existing install untouched."; exit 0 ;;
	esac
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
echo "pulse: downloading ${asset}…"
curl -fSL# "$url" -o "$tmp" || fail "download failed (no release asset for ${os}/${arch}?)"
chmod +x "$tmp"
$sudo mv "$tmp" "$target"
trap - EXIT

echo "pulse: installed to ${target} ($("$target" --version 2>/dev/null || echo "?"))"
echo "pulse: run 'pulse' to get started."
