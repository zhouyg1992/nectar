<h1 align="center">🍯 Nectar</h1>

<p align="center">
  <strong>Memory that grows with you.</strong><br>
  A Go bridge between chat apps and coding agents.
</p>

<p align="center">
  <a href="README.md">简体中文</a> ·
  <a href="README.en.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/zhouyg1992/nectar/releases"><img src="https://img.shields.io/github/v/release/zhouyg1992/nectar?display_name=tag&sort=semver" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zhouyg1992/nectar" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.22+">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="macOS | Linux">
</p>

> First release: **v0.1.0**.

Nectar forwards Feishu messages to Codex CLI and streams the result back as an interactive card. v1 supports Feishu + Codex.

## Support matrix

**v1 currently supports Feishu / Lark + Codex CLI only.**

Legend: ✅ supported · 🚧 planned

### Platforms

| Platform | Status | Notes |
|---|---|---|
| Feishu / Lark | ✅ Supported | WebSocket, interactive cards, context usage |
| DingTalk | 🚧 Planned | Stream mode and interactive cards |
| Telegram | 🚧 Planned | Long polling |
| WeCom / WeChat | 🚧 Planned | Platform-dependent |
| Slack | 🚧 Planned | Socket Mode |
| More platforms | 🚧 Planned | Implement `PlatformAdapter` |

### Engines

| Engine | Status | Notes |
|---|---|---|
| Codex CLI | ✅ Supported | `exec --json` / `resume` |
| Claude Code | 🚧 Planned | CLI / Agent SDK adapter |
| OpenCode | 🚧 Planned | CLI adapter |
| Pi | 🚧 Planned | CLI adapter |
| Kimi Code | 🚧 Planned | Local server API adapter |
| More engines | 🚧 Planned | Implement `EngineAdapter` |

> Planned items indicate direction, not a schedule. See the [Roadmap](docs/zh-CN/roadmap.md) and Releases for progress.

## Install

macOS / Linux (amd64 / arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

Pin a version or choose an install directory:

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --version v0.1.0
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --dir /usr/local/bin
```

The installer verifies the SHA-256 checksum, installs to `~/.local/bin/nectar`, and creates `~/.nectar/config.toml` if it does not exist.

Windows: use WSL or download a release manually. The repository must be public for anonymous `curl | sh`.

## Prerequisites

- Codex CLI installed and logged in:

```bash
codex exec --json --skip-git-repo-check "reply with OK"
```

- A Feishu self-built app with the bot capability, message event subscription, and long-connection mode. See the [Feishu guide](docs/zh-CN/feishu.md) for now.

## Configuration

Edit `~/.nectar/config.toml`:

```toml
[feishu]
app_id = "cli_xxx"
app_secret = "xxx"
log_level = "info"

[codex]
path = "codex"
model = ""
effort = ""
sandbox = "workspace-write"
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0
per_chat_workspace = false
bypass_approvals = false

[bot]
name = "Nectar"
agent_id = "codex"
admin_users = []
allowed_chats = []

[storage]
data_dir = "~/.nectar"
```

`context_window = 0` auto-detects the model context window from the Codex model catalog. Set it explicitly if needed, e.g. `context_window = 258000`.

## Usage

```bash
nectar
```

Wait for:

```text
feishu websocket connected; ready to receive messages
```

Send `/help` first to verify the Feishu path without invoking Codex. Direct messages work without a mention; group messages require @Bot.

### Card behavior

- `🐝 Thinking...` immediately after a message
- `🐝 Working...` with the current tool command
- `🍯 Complete` or `⚠️ Failed` in place
- Footer shows context usage, model, elapsed time, and tokens

### Commands

| Command | Description |
|---|---|
| `/new`, `/reset` | Start a new Codex thread |
| `/status` | Show the current thread ID |
| `/stop` | Cancel the current run |
| `/help` | Show help |

## Docs

- [English docs](docs/en/README.md) (translation in progress)
- [Chinese docs](docs/zh-CN/README.md)
- [Architecture](docs/zh-CN/architecture.md)
- [Development](docs/zh-CN/development.md)
- [Security](docs/zh-CN/security.md)
- [Release process](docs/zh-CN/release.md)
- [Roadmap](docs/zh-CN/roadmap.md)
- [Changelog](docs/zh-CN/changelog.md)

## License

MIT © 2026 zhou.yongguo. See [LICENSE](LICENSE).
