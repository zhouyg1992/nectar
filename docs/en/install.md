# Install and Upgrade

## Requirements

- macOS or Linux, amd64 / arm64
- At least one installed and logged-in engine CLI: Codex, Claude Code, Cursor Agent, OpenCode, or Pi
- Access to GitHub Releases
- `curl` or `wget`, `tar`, and either `sha256sum` or `shasum`

On Windows, use WSL or download a release manually. The repository must be public for anonymous `curl | sh`.

## Interactive install

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

The installer:

1. Detects installed engine CLIs and exits if none are found.
2. Lets you choose an engine and platform (Feishu / Lark by default, DingTalk optional).
3. Asks for the selected platform's credentials.
4. Downloads the matching release from GitHub and verifies `checksums.txt` when available.
5. Installs the binary to `~/.local/bin/nectar`.
6. Writes a minimal `~/.nectar/config.toml`.
7. Installs the `nectar-schedule` skill and adds a managed block to the workspace `AGENTS.md`.
8. Installs and starts the background service.

An existing `~/.nectar/config.toml` is kept by default. In interactive mode the installer asks whether to keep it, reconfigure, or exit.

> The installer only writes `[bot]` and the selected platform credentials. Engine sections, `[schedule]`, and `bot.admin_users` use built-in defaults and are not written to the file.

## Verify the install

```bash
nectar service status
nectar service logs -f
```

Wait for the platform connection message, then send `/help` in chat:

- Feishu: `feishu websocket connected; ready to receive messages`
- DingTalk: `dingtalk: stream connected`

Direct messages work without a mention; group messages require @Bot.

## Installer options

```text
--version vX.Y.Z        install a specific release (default: latest)
--dir DIR               install directory (default: ~/.local/bin)
--config PATH           config path (default: ~/.nectar/config.toml)
--engine NAME           codex | opencode | cursor | claude | pi
--platform NAME         feishu | dingtalk | telegram
--feishu-app-id ID      Feishu app_id
--feishu-app-secret S   Feishu app_secret
--dingtalk-client-id ID DingTalk client_id
--dingtalk-client-secret S
                        DingTalk client_secret
--telegram-bot-token T  Telegram bot token
--reconfigure           back up and replace an existing config
--no-service            install the binary only
--service               accepted for compatibility; service is the default
--no-skills             skip agent skill installation
--linger                enable systemd user linger (Linux)
--no-start              install and enable the service without starting it now
-h, --help              show installer help
```

Environment variables:

```text
NECTAR_VERSION          same as --version
NECTAR_BIN_DIR          same as --dir
NECTAR_DOWNLOAD_BASE    override the release download base URL
FEISHU_APP_ID           same as --feishu-app-id
FEISHU_APP_SECRET       same as --feishu-app-secret
DINGTALK_CLIENT_ID      same as --dingtalk-client-id
DINGTALK_CLIENT_SECRET  same as --dingtalk-client-secret
```

## Common installs

Pin a version or choose an install directory:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --version v0.7.0

curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --dir /usr/local/bin
```

Download first, then run:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh -o install.sh
sh install.sh
```

Install the binary only:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --no-service
```

Non-interactive install:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env FEISHU_APP_ID=cli_xxx FEISHU_APP_SECRET=xxx \
  sh -s -- --engine codex --platform feishu
```

DingTalk:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env DINGTALK_CLIENT_ID=dingxxx DINGTALK_CLIENT_SECRET=xxx \
  sh -s -- --engine codex --platform dingtalk
```

Telegram:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env TELEGRAM_BOT_TOKEN=your-bot-token \
  sh -s -- --engine codex --platform telegram
```

Start at boot on Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --linger
```

If `~/.local/bin` is not on `PATH`, add it as printed by the installer:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Update

```bash
nectar update
```

`nectar update` downloads the latest release, verifies `checksums.txt`, atomically replaces the binary, refreshes agent skills with the new binary, and restarts the installed background service. It leaves `~/.nectar/config.toml` and data untouched. Asset downloads show a progress bar, and transient connection, TLS, or 5xx failures are retried automatically.

```bash
nectar update --check          # check without installing
nectar update --version v0.7.0 # pin or downgrade
nectar update --force          # reinstall the current version
nectar update --no-restart     # update binary and skills without restarting
nectar update --no-skills      # skip the skill refresh
nectar update --timeout 15m    # wait up to 15 minutes per download attempt
nectar update --attempts 3     # try up to 3 times on transient failures
nectar update --no-progress    # suitable for scripts; hide the progress bar
```

Session mappings and last context usage are stored in `~/.nectar/sessions.json`. After a restart, Nectar resumes the same engine session; if that session is no longer valid, it reports the failure and starts a new one.

## Uninstall

```bash
nectar uninstall                    # service, binary, skills, and managed AGENTS.md blocks
nectar uninstall --purge-data       # also remove config, task data, workspaces, and backups
nectar skills uninstall             # remove only the agent skill
```

`nectar uninstall` keeps config and task data by default. Common flags:

```text
--keep-binary     keep the binary
--no-skills       keep agent skills and managed AGENTS.md blocks
--dry-run         preview cleanup
--config PATH     config path
--binary PATH     binary path
```

If you installed with a custom `--dir` or `--config`, pass `--binary` and `--config` with the same paths when uninstalling.

## Run from source

Requires Go 1.26 or later:

```bash
go build -o nectar .
mkdir -p ~/.nectar
[ -f ~/.nectar/config.toml ] || cp config.example.toml ~/.nectar/config.toml
./nectar
```

When running from source, edit `~/.nectar/config.toml` and fill in at least the platform credentials.

Back to: [English README](../../README.md) · [Configuration](config.md) · [Background service](service.md)
