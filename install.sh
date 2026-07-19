#!/bin/sh
# Install (or update) pulse from the latest GitHub release.
#   curl -fsSL https://raw.githubusercontent.com/tanmoysrt/pulse/master/install.sh | sh
set -eu

REPO="tanmoysrt/pulse"
BIN="pulse"
INSTALL_DIR="/usr/bin"

fail() { echo "pulse: $1" >&2; exit 1; }

as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
		return
	fi
	command -v sudo >/dev/null 2>&1 || fail "need sudo to install required tools"
	sudo "$@"
}

install_linux_tools() {
	missing_tmux=0
	missing_sqlite=0
	command -v tmux >/dev/null 2>&1 || missing_tmux=1
	command -v sqlite3 >/dev/null 2>&1 || missing_sqlite=1
	[ "$missing_tmux" -eq 0 ] && [ "$missing_sqlite" -eq 0 ] && return

	if command -v apt-get >/dev/null 2>&1; then
		packages=""
		[ "$missing_tmux" -eq 0 ] || packages="$packages tmux"
		[ "$missing_sqlite" -eq 0 ] || packages="$packages sqlite3"
		as_root apt-get update
		as_root apt-get install -y $packages
	elif command -v apk >/dev/null 2>&1; then
		packages=""
		[ "$missing_tmux" -eq 0 ] || packages="$packages tmux"
		[ "$missing_sqlite" -eq 0 ] || packages="$packages sqlite"
		as_root apk add $packages
	elif command -v dnf >/dev/null 2>&1; then
		packages=""
		[ "$missing_tmux" -eq 0 ] || packages="$packages tmux"
		[ "$missing_sqlite" -eq 0 ] || packages="$packages sqlite"
		as_root dnf install -y $packages
	elif command -v yum >/dev/null 2>&1; then
		packages=""
		[ "$missing_tmux" -eq 0 ] || packages="$packages tmux"
		[ "$missing_sqlite" -eq 0 ] || packages="$packages sqlite"
		as_root yum install -y $packages
	elif command -v pacman >/dev/null 2>&1; then
		packages=""
		[ "$missing_tmux" -eq 0 ] || packages="$packages tmux"
		[ "$missing_sqlite" -eq 0 ] || packages="$packages sqlite"
		as_root pacman -Sy --noconfirm $packages
	else
		fail "unsupported Linux package manager; install tmux and sqlite3, then run this again"
	fi
}

install_macos_tools() {
	packages=""
	command -v tmux >/dev/null 2>&1 || packages="$packages tmux"
	command -v sqlite3 >/dev/null 2>&1 || packages="$packages sqlite"
	[ -n "$packages" ] || return

	command -v brew >/dev/null 2>&1 || fail "Homebrew is required to install tmux and sqlite3 (https://brew.sh)"
	brew install $packages
	if command -v sqlite3 >/dev/null 2>&1; then
		return
	fi
	brew link --overwrite --force sqlite
}

install_required_tools() {
	case "$os" in
		linux) install_linux_tools ;;
		darwin) install_macos_tools ;;
	esac
	command -v tmux >/dev/null 2>&1 || fail "tmux installation failed"
	command -v sqlite3 >/dev/null 2>&1 || fail "sqlite3 installation failed"
}

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

install_required_tools

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
echo "pulse: downloading ${asset}…"
curl -fSL# "$url" -o "$tmp" || fail "download failed (no release asset for ${os}/${arch}?)"
chmod +x "$tmp"
$sudo mv "$tmp" "$target"
trap - EXIT

echo "pulse: installed to ${target} ($("$target" --version 2>/dev/null || echo "?"))"
echo "pulse: run 'pulse' to get started."
