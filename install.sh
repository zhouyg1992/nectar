#!/bin/sh
set -eu

REPO="zhouyg1992/nectar"
BIN_NAME="nectar"
VERSION="${NECTAR_VERSION:-}"
TMP_DIR=""
SERVICE=0
LINGER=0
START=1

usage() {
	cat <<'EOF'
Install Nectar from GitHub Releases.

Usage:
  install.sh [--version vX.Y.Z] [--dir DIR] [--service] [--linger] [--no-start]

Options:
  --version vX.Y.Z   install a specific release (default: latest)
  --dir DIR          install directory (default: ~/.local/bin)
  --service          install and enable a background service
  --linger           enable systemd user linger (Linux; implies --service)
  --no-start         install the service but do not start it now
  -h, --help         show this help

Environment:
  NECTAR_VERSION        same as --version
  NECTAR_BIN_DIR        same as --dir
  NECTAR_DOWNLOAD_BASE  override the release download base URL
EOF
}

log() {
	printf '%s\n' "$*"
}

err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

cleanup() {
	if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || err "--version requires a value"
		VERSION="$2"
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || err "--dir requires a value"
		BIN_DIR="$2"
		shift 2
		;;
	--service)
		SERVICE=1
		shift
		;;
	--linger)
		SERVICE=1
		LINGER=1
		shift
		;;
	--no-start)
		SERVICE=1
		START=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		err "unknown option: $1"
		;;
	esac
done

[ -n "${HOME:-}" ] || err "HOME is not set"
BIN_DIR="${BIN_DIR:-${NECTAR_BIN_DIR:-$HOME/.local/bin}}"

need uname
need tar
need mktemp

latest_version() {
	if command -v curl >/dev/null 2>&1; then
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") || return 1
		case "$url" in
		*/releases/tag/*)
			printf '%s\n' "${url##*/}"
			return 0
			;;
		esac
		return 1
	fi
	if command -v wget >/dev/null 2>&1; then
		tag=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
			head -n 1)
		[ -n "$tag" ] || return 1
		printf '%s\n' "$tag"
		return 0
	fi
	return 1
}

download() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$out"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$out" "$url"
	else
		err "curl or wget is required"
	fi
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		err "sha256sum or shasum is required"
	fi
}

verify_checksum() {
	url="$1"
	file="$2"
	name="$3"
	dir="$4"

	if ! download "$url" "$dir/checksums.txt"; then
		log "warning: checksums.txt unavailable; skipping verification"
		return 0
	fi
	expected=$(awk -v name="$name" '{f=$2; sub(/^\*/, "", f); if (f==name) {print $1; exit}}' "$dir/checksums.txt")
	if [ -z "$expected" ]; then
		log "warning: checksum entry not found; skipping verification"
		return 0
	fi
	actual=$(sha256_file "$file")
	[ "$actual" = "$expected" ] || err "checksum mismatch for $name"
	log "checksum verified"
}

if [ -z "$VERSION" ]; then
	VERSION=$(latest_version) || err "could not determine the latest release; pass --version vX.Y.Z"
fi
case "$VERSION" in
v*) ;;
*) VERSION="v$VERSION" ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
darwin | linux) ;;
*) err "unsupported OS: $OS (use Linux/macOS, or WSL on Windows)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
x86_64 | amd64) ARCH="amd64" ;;
arm64 | aarch64) ARCH="arm64" ;;
*) err "unsupported architecture: $ARCH" ;;
esac

VERSION_NO_V=${VERSION#v}
ASSET="${BIN_NAME}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
BASE_URL="${NECTAR_DOWNLOAD_BASE:-https://github.com/${REPO}/releases/download/${VERSION}}"

TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t nectar 2>/dev/null || true)
[ -n "$TMP_DIR" ] || err "could not create a temporary directory"

log "Nectar ${VERSION} (${OS}/${ARCH})"
download "$BASE_URL/$ASSET" "$TMP_DIR/$ASSET" || err "download failed: $BASE_URL/$ASSET"
verify_checksum "$BASE_URL/checksums.txt" "$TMP_DIR/$ASSET" "$ASSET" "$TMP_DIR"

mkdir -p "$TMP_DIR/pkg"
tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR/pkg" || err "failed to extract $ASSET"

BIN_PATH="$TMP_DIR/pkg/$BIN_NAME"
if [ ! -f "$BIN_PATH" ]; then
	BIN_PATH=$(find "$TMP_DIR/pkg" -type f -name "$BIN_NAME" | head -n 1 || true)
fi
[ -n "${BIN_PATH:-}" ] && [ -f "$BIN_PATH" ] || err "binary not found in archive"

mkdir -p "$BIN_DIR" 2>/dev/null || err "cannot create $BIN_DIR (try --dir with a writable path)"
if command -v install >/dev/null 2>&1; then
	install -m 0755 "$BIN_PATH" "$BIN_DIR/$BIN_NAME" || err "failed to install to $BIN_DIR"
else
	cp "$BIN_PATH" "$BIN_DIR/$BIN_NAME" || err "failed to install to $BIN_DIR"
	chmod 0755 "$BIN_DIR/$BIN_NAME"
fi

CONFIG_DIR="$HOME/.nectar"
CONFIG_FILE="$CONFIG_DIR/config.toml"
if [ ! -f "$CONFIG_FILE" ] && [ -f "$TMP_DIR/pkg/config.example.toml" ]; then
	mkdir -p "$CONFIG_DIR"
	cp "$TMP_DIR/pkg/config.example.toml" "$CONFIG_FILE"
	chmod 600 "$CONFIG_FILE"
	log "created $CONFIG_FILE"
fi

if [ "$SERVICE" -eq 1 ]; then
	set -- service install --config "$CONFIG_FILE" --binary "$BIN_DIR/$BIN_NAME"
	if [ "$LINGER" -eq 1 ]; then
		set -- "$@" --linger
	fi
	if [ "$START" -eq 0 ]; then
		set -- "$@" --no-start
	fi
	if ! "$BIN_DIR/$BIN_NAME" "$@"; then
		err "service install failed; edit $CONFIG_FILE and run: $BIN_DIR/$BIN_NAME service install"
	fi
fi

log "installed $BIN_DIR/$BIN_NAME"
case ":${PATH:-}:" in
*":$BIN_DIR:"*) ;;
*) log "add to PATH: export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

if ! command -v codex >/dev/null 2>&1 && ! command -v opencode >/dev/null 2>&1; then
	log "warning: neither codex nor opencode CLI found; install and log in to your engine before running nectar"
fi

log ""
log "Next:"
log "  1. edit $CONFIG_FILE with your Feishu app_id/app_secret"
log "  2. make sure your engine CLI (codex or opencode) is installed and logged in"
log "  3. run: $BIN_NAME"
log "  4. optional background service: $BIN_NAME service install"
log "docs: https://github.com/${REPO}#readme"
