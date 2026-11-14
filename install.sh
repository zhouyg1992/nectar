#!/bin/sh
set -eu

REPO="zhouyg1992/nectar"
BIN_NAME="nectar"
VERSION="${NECTAR_VERSION:-}"
TMP_DIR=""
SERVICE=1
LINGER=0
START=1
SKILLS=1
SKILL_NAME="nectar-schedule"
ENGINE=""
PLATFORM=""
FEISHU_APP_ID="${FEISHU_APP_ID:-}"
FEISHU_APP_SECRET="${FEISHU_APP_SECRET:-}"
DINGTALK_CLIENT_ID="${DINGTALK_CLIENT_ID:-}"
DINGTALK_CLIENT_SECRET="${DINGTALK_CLIENT_SECRET:-}"
TELEGRAM_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
CONFIG_FILE=""
RECONFIGURE=0
SKIP_CONFIG=0
HAVE_TTY=0
SERVICE_TOUCHED=0

ENGINE_DEFS='codex|codex|Codex CLI
opencode|opencode|OpenCode
cursor|agent,cursor-agent|Cursor Agent CLI
claude|claude|Claude Code
pi|pi|Pi'

PLATFORM_DEFS='feishu|Feishu / Lark
dingtalk|DingTalk
telegram|Telegram'

usage() {
	cat <<'EOF'
Install Nectar from GitHub Releases.

Usage:
  install.sh [options]

Options:
  --version vX.Y.Z        install a specific release (default: latest)
  --dir DIR               install directory (default: ~/.local/bin)
  --config PATH           config path (default: ~/.nectar/config.toml)
  --engine NAME           engine: codex, opencode, cursor, claude, pi
  --platform NAME         platform: feishu, dingtalk, telegram (default: interactive)
  --feishu-app-id ID      Feishu app_id
  --feishu-app-secret S   Feishu app_secret
  --dingtalk-client-id ID DingTalk client_id
  --dingtalk-client-secret S
                          DingTalk client_secret
  --telegram-bot-token T  Telegram bot token
  --reconfigure           back up and replace an existing config
  --no-service            install the binary only
  --service               accepted for compatibility; service is default
  --no-skills             skip installing the Nectar schedule skill
  --linger                enable systemd user linger (Linux)
  --no-start              install the service without starting it now
  -h, --help              show this help

Environment:
  NECTAR_VERSION          same as --version
  NECTAR_BIN_DIR          same as --dir
  NECTAR_DOWNLOAD_BASE    override the release download base URL
  FEISHU_APP_ID           same as --feishu-app-id
  FEISHU_APP_SECRET       same as --feishu-app-secret
  DINGTALK_CLIENT_ID      same as --dingtalk-client-id
  DINGTALK_CLIENT_SECRET  same as --dingtalk-client-secret
  TELEGRAM_BOT_TOKEN      same as --telegram-bot-token

When run interactively, the installer detects installed engine CLIs, asks for
the engine, platform, and credentials, writes a minimal config, and installs
the background service.
EOF
}

log() {
	printf '%s\n' "$*"
}

step() {
	printf '\n🐝 [%s/5] %s\n' "$1" "$2"
}

err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

cleanup() {
	if [ "$HAVE_TTY" -eq 1 ]; then
		stty echo <&3 2>/dev/null || true
	fi
	if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
	if [ -n "${TMP_CONFIG:-}" ] && [ -f "$TMP_CONFIG" ]; then
		rm -f "$TMP_CONFIG"
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
	--config)
		[ $# -ge 2 ] || err "--config requires a value"
		CONFIG_FILE="$2"
		shift 2
		;;
	--engine)
		[ $# -ge 2 ] || err "--engine requires a value"
		ENGINE="$2"
		shift 2
		;;
	--platform)
		[ $# -ge 2 ] || err "--platform requires a value"
		PLATFORM="$2"
		shift 2
		;;
	--feishu-app-id)
		[ $# -ge 2 ] || err "--feishu-app-id requires a value"
		FEISHU_APP_ID="$2"
		shift 2
		;;
	--feishu-app-secret)
		[ $# -ge 2 ] || err "--feishu-app-secret requires a value"
		FEISHU_APP_SECRET="$2"
		shift 2
		;;
	--dingtalk-client-id)
		[ $# -ge 2 ] || err "--dingtalk-client-id requires a value"
		DINGTALK_CLIENT_ID="$2"
		shift 2
		;;
	--dingtalk-client-secret)
		[ $# -ge 2 ] || err "--dingtalk-client-secret requires a value"
		DINGTALK_CLIENT_SECRET="$2"
		shift 2
		;;
	--telegram-bot-token)
		[ $# -ge 2 ] || err "--telegram-bot-token requires a value"
		TELEGRAM_BOT_TOKEN="$2"
		shift 2
		;;
	--reconfigure)
		RECONFIGURE=1
		shift
		;;
	--no-service)
		SERVICE=0
		shift
		;;
	--service)
		SERVICE=1
		shift
		;;
	--no-skills)
		SKILLS=0
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

if [ "$LINGER" -eq 1 ] || [ "$START" -eq 0 ]; then
	SERVICE=1
fi

[ -n "${HOME:-}" ] || err "HOME is not set"
BIN_DIR="${BIN_DIR:-${NECTAR_BIN_DIR:-$HOME/.local/bin}}"
CONFIG_FILE="${CONFIG_FILE:-$HOME/.nectar/config.toml}"

case "$CONFIG_FILE" in
"~/"*) CONFIG_FILE="$HOME/${CONFIG_FILE#~/}" ;;
esac
case "$CONFIG_FILE" in
/*) ;;
*) CONFIG_FILE="$(pwd)/$CONFIG_FILE" ;;
esac

if { exec 3</dev/tty; } 2>/dev/null; then
	HAVE_TTY=1
fi

need uname
need tar
need mktemp

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

need_tty() {
	[ "$HAVE_TTY" -eq 1 ] || err "interactive input required but no TTY is available; pass $1"
}

read_line() {
	printf '%s' "$1" >&2
	if IFS= read -r REPLY <&3; then :; else REPLY=""; fi
}

read_secret() {
	printf '%s' "$1" >&2
	if command -v stty >/dev/null 2>&1 && stty -echo <&3 2>/dev/null; then
		if IFS= read -r REPLY <&3; then :; else REPLY=""; fi
		stty echo <&3 2>/dev/null || true
		printf '\n' >&2
	else
		if IFS= read -r REPLY <&3; then :; else REPLY=""; fi
	fi
}

choose_index() {
	CHOICE_PROMPT=$1
	CHOICE_MAX=$2
	CHOICE_DEFAULT=$3
	while :; do
		printf '%s [%s]: ' "$CHOICE_PROMPT" "$CHOICE_DEFAULT" >&2
		if IFS= read -r CHOICE <&3; then :; else CHOICE=""; fi
		[ -n "$CHOICE" ] || CHOICE=$CHOICE_DEFAULT
		case "$CHOICE" in
		*[!0-9]*) ;;
		*)
			if [ "$CHOICE" -ge 1 ] && [ "$CHOICE" -le "$CHOICE_MAX" ]; then
				return 0
			fi
			;;
		esac
		printf 'invalid selection; enter 1-%s\n' "$CHOICE_MAX" >&2
	done
}

confirm_yes() {
	printf '%s [Y/n]: ' "$1" >&2
	if IFS= read -r CONFIRM <&3; then :; else CONFIRM=""; fi
	case "$CONFIRM" in
	[nN]*) return 1 ;;
	*) return 0 ;;
	esac
}

trim_value() {
	printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

toml_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

is_placeholder_value() {
	PV=$1
	if [ -z "$PV" ]; then
		return 0
	fi
	if [ "$PV" = "xxx" ]; then
		return 0
	fi
	case "$PV" in
	cli_x* | *xxxxxxxx*) return 0 ;;
	esac
	case "$PV" in
	x*)
		if [ "${#PV}" -ge 8 ] && [ -z "$(printf '%s' "$PV" | tr -d 'x')" ]; then
			return 0
		fi
		;;
	esac
	return 1
}

prompt_value() {
	PROMPT_LABEL=$1
	while :; do
		read_line "$PROMPT_LABEL: "
		VALUE=$(trim_value "$REPLY")
		if [ -z "$VALUE" ]; then
			printf 'value required\n' >&2
			continue
		fi
		if is_placeholder_value "$VALUE"; then
			printf 'placeholder value detected; enter the real value\n' >&2
			continue
		fi
		printf '%s' "$VALUE"
		return 0
	done
}

prompt_secret_value() {
	PROMPT_LABEL=$1
	while :; do
		read_secret "$PROMPT_LABEL: "
		VALUE=$(trim_value "$REPLY")
		if [ -z "$VALUE" ]; then
			printf 'value required\n' >&2
			continue
		fi
		if is_placeholder_value "$VALUE"; then
			printf 'placeholder value detected; enter the real value\n' >&2
			continue
		fi
		printf '%s' "$VALUE"
		return 0
	done
}

engine_command() {
	case "$1" in
	codex) printf 'codex' ;;
	opencode) printf 'opencode' ;;
	cursor) printf 'agent,cursor-agent' ;;
	claude) printf 'claude' ;;
	pi) printf 'pi' ;;
	*) return 1 ;;
	esac
}

engine_command_path() {
	ECP_CMDS=$1
	ECP_OLD_IFS=$IFS
	IFS=','
	for ECP_CMD in $ECP_CMDS; do
		if command -v "$ECP_CMD" >/dev/null 2>&1; then
			IFS=$ECP_OLD_IFS
			command -v "$ECP_CMD"
			return 0
		fi
	done
	IFS=$ECP_OLD_IFS
	return 1
}

normalize_engine() {
	case "$1" in
	agent | cursor-agent) printf 'cursor' ;;
	claude-code) printf 'claude' ;;
	*) printf '%s' "$1" ;;
	esac
}

engine_label() {
	case "$1" in
	codex) printf 'Codex CLI' ;;
	opencode) printf 'OpenCode' ;;
	cursor) printf 'Cursor Agent CLI' ;;
	claude) printf 'Claude Code' ;;
	pi) printf 'Pi' ;;
	*) printf '%s' "$1" ;;
	esac
}

detect_engines() {
	DETECTED_COUNT=0
	DETECTED_DEFS=""
	printf '\nDetected engines:\n' >&2
	OLD_IFS=$IFS
	IFS='
'
	for DEF in $ENGINE_DEFS; do
		ENGINE_KEY=${DEF%%|*}
		REST=${DEF#*|}
		ENGINE_CMD=${REST%%|*}
		ENGINE_LABEL=${REST#*|}
		ENGINE_PATH=
		if ENGINE_PATH=$(engine_command_path "$ENGINE_CMD"); then
			DETECTED_COUNT=$((DETECTED_COUNT + 1))
			if [ -z "$DETECTED_DEFS" ]; then
				DETECTED_DEFS=$DEF
			else
				DETECTED_DEFS="$DETECTED_DEFS
$DEF"
			fi
			printf '  %d) %-18s %s\n' "$DETECTED_COUNT" "$ENGINE_LABEL" "$ENGINE_PATH" >&2
		fi
	done
	IFS=$OLD_IFS
}

select_engine() {
	if [ -n "$ENGINE" ]; then
		ENGINE=$(normalize_engine "$(printf '%s' "$(trim_value "$ENGINE")" | tr '[:upper:]' '[:lower:]')")
		ENGINE_CMD=$(engine_command "$ENGINE") || err "unsupported engine: $ENGINE"
		ENGINE_PATH=$(engine_command_path "$ENGINE_CMD") ||
			err "engine CLI not found in PATH: $(printf '%s' "$ENGINE_CMD" | tr ',' '/')"
		return 0
	fi
	need_tty "--engine codex|opencode|cursor|claude|pi"
	choose_index "Select engine" "$DETECTED_COUNT" 1
	DEF=$(printf '%s\n' "$DETECTED_DEFS" | sed -n "${CHOICE}p")
	ENGINE=${DEF%%|*}
	REST=${DEF#*|}
	ENGINE_CMD=${REST%%|*}
	ENGINE_PATH=$(engine_command_path "$ENGINE_CMD") || err "engine CLI not found in PATH: $ENGINE_CMD"
}

select_platform() {
	if [ -n "$PLATFORM" ]; then
		PLATFORM=$(printf '%s' "$(trim_value "$PLATFORM")" | tr '[:upper:]' '[:lower:]')
		case "$PLATFORM" in
		lark) PLATFORM="feishu" ;;
		esac
		case "$PLATFORM" in
		feishu | dingtalk | telegram) ;;
		*) err "unsupported platform: $PLATFORM (supported: feishu, dingtalk, telegram)" ;;
		esac
		return 0
	fi
	need_tty "--platform feishu|dingtalk|telegram"
	PLATFORM_COUNT=0
	printf '\nSelect platform:\n' >&2
	OLD_IFS=$IFS
	IFS='
'
	for DEF in $PLATFORM_DEFS; do
		PLATFORM_COUNT=$((PLATFORM_COUNT + 1))
		PLATFORM_LABEL=${DEF#*|}
		if [ "$PLATFORM_COUNT" -eq 1 ]; then
			printf '  %d) %s (default)\n' "$PLATFORM_COUNT" "$PLATFORM_LABEL" >&2
		else
			printf '  %d) %s\n' "$PLATFORM_COUNT" "$PLATFORM_LABEL" >&2
		fi
	done
	IFS=$OLD_IFS
	choose_index "Platform" "$PLATFORM_COUNT" 1
	DEF=$(printf '%s\n' "$PLATFORM_DEFS" | sed -n "${CHOICE}p")
	PLATFORM=${DEF%%|*}
}

configure_feishu() {
	FEISHU_APP_ID=$(trim_value "$FEISHU_APP_ID")
	FEISHU_APP_SECRET=$(trim_value "$FEISHU_APP_SECRET")
	if [ -z "$FEISHU_APP_ID" ]; then
		need_tty "--feishu-app-id or FEISHU_APP_ID"
		FEISHU_APP_ID=$(prompt_value "Feishu app_id")
	fi
	if is_placeholder_value "$FEISHU_APP_ID"; then
		err "invalid Feishu app_id (placeholder value)"
	fi
	if [ -z "$FEISHU_APP_SECRET" ]; then
		need_tty "--feishu-app-secret or FEISHU_APP_SECRET"
		FEISHU_APP_SECRET=$(prompt_secret_value "Feishu app_secret")
	fi
	if is_placeholder_value "$FEISHU_APP_SECRET"; then
		err "invalid Feishu app_secret (placeholder value)"
	fi
}

configure_dingtalk() {
	DINGTALK_CLIENT_ID=$(trim_value "$DINGTALK_CLIENT_ID")
	DINGTALK_CLIENT_SECRET=$(trim_value "$DINGTALK_CLIENT_SECRET")
	if [ -z "$DINGTALK_CLIENT_ID" ]; then
		need_tty "--dingtalk-client-id or DINGTALK_CLIENT_ID"
		DINGTALK_CLIENT_ID=$(prompt_value "DingTalk client_id")
	fi
	if is_placeholder_value "$DINGTALK_CLIENT_ID"; then
		err "invalid DingTalk client_id (placeholder value)"
	fi
	if [ -z "$DINGTALK_CLIENT_SECRET" ]; then
		need_tty "--dingtalk-client-secret or DINGTALK_CLIENT_SECRET"
		DINGTALK_CLIENT_SECRET=$(prompt_secret_value "DingTalk client_secret")
	fi
	if is_placeholder_value "$DINGTALK_CLIENT_SECRET"; then
		err "invalid DingTalk client_secret (placeholder value)"
	fi
}

configure_telegram() {
	TELEGRAM_BOT_TOKEN=$(trim_value "$TELEGRAM_BOT_TOKEN")
	if [ -z "$TELEGRAM_BOT_TOKEN" ]; then
		need_tty "--telegram-bot-token or TELEGRAM_BOT_TOKEN"
		TELEGRAM_BOT_TOKEN=$(prompt_secret_value "Telegram bot_token")
	fi
	if is_placeholder_value "$TELEGRAM_BOT_TOKEN"; then
		err "invalid Telegram bot_token (placeholder value)"
	fi
}

configure_platform() {
	case "$PLATFORM" in
	feishu) configure_feishu ;;
	dingtalk) configure_dingtalk ;;
	telegram) configure_telegram ;;
	*) err "unsupported platform: $PLATFORM" ;;
	esac
}

backup_config() {
	if [ -f "$CONFIG_FILE" ]; then
		BACKUP="$CONFIG_FILE.bak.$(date +%Y%m%d-%H%M%S).$$"
		cp -p "$CONFIG_FILE" "$BACKUP" || err "could not back up existing config to $BACKUP"
		log "backed up existing config to $BACKUP"
	fi
}

write_config() {
	CONFIG_DIR=$(dirname "$CONFIG_FILE")
	OLD_UMASK=$(umask)
	umask 077
	mkdir -p "$CONFIG_DIR" || {
		umask "$OLD_UMASK"
		err "cannot create $CONFIG_DIR"
	}
	TMP_CONFIG="$CONFIG_FILE.tmp.$$"
	{
		printf '# Generated by the Nectar installer.\n'
		printf '\n[bot]\n'
		printf 'engine = "%s"\n' "$ENGINE"
		printf 'platform = "%s"\n' "$PLATFORM"
		printf '\n[%s]\n' "$PLATFORM"
		case "$PLATFORM" in
		feishu)
			printf 'app_id = "%s"\n' "$(toml_escape "$FEISHU_APP_ID")"
			printf 'app_secret = "%s"\n' "$(toml_escape "$FEISHU_APP_SECRET")"
			;;
		dingtalk)
			printf 'client_id = "%s"\n' "$(toml_escape "$DINGTALK_CLIENT_ID")"
			printf 'client_secret = "%s"\n' "$(toml_escape "$DINGTALK_CLIENT_SECRET")"
			;;
		telegram)
			printf 'bot_token = "%s"\n' "$(toml_escape "$TELEGRAM_BOT_TOKEN")"
			;;
		esac
	} >"$TMP_CONFIG" || {
		umask "$OLD_UMASK"
		err "cannot write $TMP_CONFIG"
	}
	chmod 600 "$TMP_CONFIG" || {
		umask "$OLD_UMASK"
		err "cannot set permissions on $TMP_CONFIG"
	}
	mv "$TMP_CONFIG" "$CONFIG_FILE" || {
		umask "$OLD_UMASK"
		err "cannot move config into place: $CONFIG_FILE"
	}
	umask "$OLD_UMASK"
}

log ""
log "🍯 Nectar installer"
step 1 "Detect installed engine CLIs"
detect_engines
if [ "$DETECTED_COUNT" -eq 0 ]; then
	err "no supported engine CLI found in PATH (codex, opencode, agent/cursor-agent, claude, pi); install and log in to one engine, then rerun the installer"
fi

step 2 "Configure Nectar"
if [ -f "$CONFIG_FILE" ] && [ "$RECONFIGURE" -eq 0 ]; then
	if [ "$HAVE_TTY" -eq 1 ]; then
		printf '\nExisting config found: %s\n' "$CONFIG_FILE" >&2
		printf '  1) Keep existing config and install the service (default)\n' >&2
		printf '  2) Back up and reconfigure\n' >&2
		printf '  3) Exit\n' >&2
		choose_index "Choose" 3 1
		case "$CHOICE" in
		1) SKIP_CONFIG=1 ;;
		2) RECONFIGURE=1 ;;
		3)
			log "aborted"
			exit 0
			;;
		esac
	else
		log "using existing config: $CONFIG_FILE"
		SKIP_CONFIG=1
	fi
fi

if [ "$SKIP_CONFIG" -eq 0 ]; then
	select_engine
	select_platform
	configure_platform
	if [ "$HAVE_TTY" -eq 1 ]; then
		printf '\nSummary:\n' >&2
		printf '  Engine:   %s (%s)\n' "$(engine_label "$ENGINE")" "$ENGINE_PATH" >&2
		printf '  Platform: %s\n' "$PLATFORM" >&2
		printf '  Config:   %s\n' "$CONFIG_FILE" >&2
		if [ "$SERVICE" -eq 1 ]; then
			printf '  Service:  install and start\n' >&2
		else
			printf '  Service:  skip\n' >&2
		fi
		if ! confirm_yes "Proceed?"; then
			log "aborted"
			exit 0
		fi
	fi
fi

step 3 "Download Nectar release"
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
	show_progress="${3:-0}"
	if command -v curl >/dev/null 2>&1; then
		if [ "$show_progress" -eq 1 ] && [ "$HAVE_TTY" -eq 1 ]; then
			curl -fL -# "$url" -o "$out"
		else
			curl -fsSL "$url" -o "$out"
		fi
	elif command -v wget >/dev/null 2>&1; then
		if [ "$show_progress" -eq 1 ] && [ "$HAVE_TTY" -eq 1 ]; then
			wget -O "$out" "$url"
		else
			wget -qO "$out" "$url"
		fi
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

VERSION_NO_V=${VERSION#v}
ASSET="${BIN_NAME}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
BASE_URL="${NECTAR_DOWNLOAD_BASE:-https://github.com/${REPO}/releases/download/${VERSION}}"

TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t nectar 2>/dev/null || true)
[ -n "$TMP_DIR" ] || err "could not create a temporary directory"

log "  🌼 Nectar ${VERSION} (${OS}/${ARCH})"
download "$BASE_URL/$ASSET" "$TMP_DIR/$ASSET" 1 || err "download failed: $BASE_URL/$ASSET"
verify_checksum "$BASE_URL/checksums.txt" "$TMP_DIR/$ASSET" "$ASSET" "$TMP_DIR"

step 4 "Install binary, config, and agent skill"
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

if [ "$SKIP_CONFIG" -eq 0 ]; then
	if [ "$RECONFIGURE" -eq 1 ]; then
		backup_config
	fi
	write_config
	log "created $CONFIG_FILE"
else
	log "kept existing $CONFIG_FILE"
fi

if [ "$SKILLS" -eq 1 ]; then
	"$BIN_DIR/$BIN_NAME" skills install --config "$CONFIG_FILE" ||
		err "failed to install the agent skill; rerun with --no-skills to skip it"
else
	log "skipped agent skill installation"
fi

if [ "$SERVICE" -eq 1 ]; then
	SERVICE_TOUCHED=1
	step 5 "Install background service"
	set -- service install --config "$CONFIG_FILE" --binary "$BIN_DIR/$BIN_NAME"
	if [ "$LINGER" -eq 1 ]; then
		set -- "$@" --linger
	fi
	if [ "$START" -eq 0 ]; then
		set -- "$@" --no-start
	fi
	if ! "$BIN_DIR/$BIN_NAME" "$@"; then
		if [ "$HAVE_TTY" -eq 1 ] && confirm_yes "Service install failed; continue without the background service?"; then
			SERVICE=0
		else
			err "service install failed; check $CONFIG_FILE, then run: $BIN_DIR/$BIN_NAME service install --config $CONFIG_FILE --binary $BIN_DIR/$BIN_NAME"
		fi
	fi
else
	step 5 "Skip background service"
fi

log ""
log "🍯 Nectar installed successfully"
log "installed $BIN_DIR/$BIN_NAME"
case ":${PATH:-}:" in
*":$BIN_DIR:"*) ;;
*) log "add to PATH: export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

log ""
log "Next:"
if [ "$SERVICE" -eq 1 ]; then
	log "  $BIN_NAME service status"
	log "  $BIN_NAME service logs -f"
else
	log "  run: $BIN_NAME"
fi
log "  $BIN_NAME update"

log ""
log "Uninstall:"
log "  \"$BIN_DIR/$BIN_NAME\" uninstall --config \"$CONFIG_FILE\" --binary \"$BIN_DIR/$BIN_NAME\""
log "  \"$BIN_DIR/$BIN_NAME\" uninstall --purge-data  # optional: also remove config, data, and backups"
log "  \"$BIN_DIR/$BIN_NAME\" skills uninstall       # only remove the Agent skill"
log "docs: https://github.com/${REPO}#readme"
