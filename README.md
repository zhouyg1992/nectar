<h1 align="center">🍯 Nectar</h1>

<p align="center">
  <strong>Your coding agent, one message away.</strong><br>
  Use Codex, Claude Code, Cursor Agent, OpenCode, or Pi from Feishu / Lark, DingTalk, or Telegram.
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/zhouyg1992/nectar/releases"><img src="https://img.shields.io/github/v/release/zhouyg1992/nectar?display_name=tag&sort=semver" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zhouyg1992/nectar" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="macOS | Linux">
  <a href="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml"><img src="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

Nectar sends chat messages to a coding agent on your machine and streams progress and results back as live messages or cards. Sessions stay continuous, progress is visible in real time, and tasks can run on a schedule. The installer detects installed engines, writes the config, and starts the background service.

<p align="center">
  <img src="docs/assets/nectar-flow.svg?v=20261114" alt="Feishu, DingTalk, and Telegram messages flow through Nectar to Codex, OpenCode, Cursor Agent, Claude Code, or Pi" width="860">
</p>

## Screenshots

<table>
  <tr>
    <td align="center"><a href="docs/assets/screenshots/feishu.png"><img src="docs/assets/screenshots/feishu.png" alt="Nectar answering a question in Feishu" width="260"></a><br><sub><b>Feishu / Lark</b></sub></td>
    <td align="center"><a href="docs/assets/screenshots/dingtalk.png"><img src="docs/assets/screenshots/dingtalk.png" alt="Nectar managing scheduled tasks in DingTalk" width="260"></a><br><sub><b>DingTalk</b></sub></td>
    <td align="center"><a href="docs/assets/screenshots/telegram.png"><img src="docs/assets/screenshots/telegram.png" alt="Nectar running commands in Telegram" width="260"></a><br><sub><b>Telegram</b></sub></td>
  </tr>
</table>

## Why Nectar

- **Chat is the interface**: talk to your agent from Feishu / Lark, DingTalk, or Telegram, in direct messages or group chats.
- **Sessions stay alive**: session mappings persist in `~/.nectar/sessions.json`, so the service resumes the same agent session when it is still valid after a restart.
- **Progress is visible**: 🌼 Thinking → 🐝 Working → 🍯 Complete / ⚠️ Failed, updated while the agent runs.
- **Tasks can be scheduled**: cron, fixed intervals, and one-shot runs, managed from chat or the CLI.
- **Bring your own engine**: choose Codex, Claude Code, Cursor Agent, OpenCode, or Pi.

## Quick start

Requirements: macOS or Linux (Windows via WSL), `curl` or `wget`, `tar`, and one installed and logged-in engine.

### 1. Prepare one coding agent

Install and log in to **one** of: **Codex, Claude Code, Cursor Agent, OpenCode, or Pi**. The installer detects installed engines.

→ [Engine setup and login checks](docs/en/engines.md)

### 2. Install

Choose one of two paths.

**Option A — install manually**

Before running it, create the platform app and get its credentials: [Feishu](docs/en/feishu.md) · [DingTalk](docs/en/dingtalk.md) · [Telegram](docs/en/telegram.md).

macOS / Linux (amd64 / arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

The installer is interactive. Choose an engine and platform (Feishu / Lark by default), then enter the credentials. It writes `~/.nectar/config.toml`, installs the background service, and starts it.

> The installer starts the background service by default. You do not need to run `nectar` manually.

**Option B — let your coding agent install it**

Paste this into Codex, Claude Code, Cursor Agent, OpenCode, or Pi:

> Install Nectar from <https://github.com/zhouyg1992/nectar>. Read AGENT_INSTALL.md in that repository and follow it. Ask me for credentials, and confirm before installing the background service.

Your agent reads the install contract, asks for the required credentials, runs the installer, and verifies the platform connection. See [AGENT_INSTALL.md](AGENT_INSTALL.md) for the full contract.

### 3. Verify

```bash
nectar service status
nectar service logs -f
```

If `nectar` is not found, run `~/.local/bin/nectar ...` or add `~/.local/bin` to `PATH`.

Send `/help` in chat first. Direct messages work without a mention; group messages require @Bot.

### Optional: harden access

The installer writes a minimal working config, but it does not restrict who can use the bot. Edit `~/.nectar/config.toml`:

```toml
[bot]
admin_users = ["ou_xxx"]
allowed_chats = ["oc_xxx"]
```

Then restart the service:

```bash
nectar service restart
```

The IDs above are Feishu examples: DingTalk uses `staffId` / `openConversationId`, and Telegram uses numeric user and chat IDs (quote negative group IDs). An empty `admin_users` leaves scheduled-task management open to anyone; an empty `allowed_chats` leaves group chats unrestricted. See the [configuration reference](docs/en/config.md) and [security boundaries](docs/en/security.md).

## Support matrix

**Platforms**

| Platform | Status | Setup |
|---|---|---|
| Feishu / Lark | ✅ Supported | [Feishu setup](docs/en/feishu.md) |
| DingTalk | ✅ Supported | [DingTalk setup](docs/en/dingtalk.md) |
| Telegram | ✅ Supported | [Telegram setup](docs/en/telegram.md) |
| WeChat, Slack | 🚧 Planned | [Roadmap](docs/en/roadmap.md) |

**Engines**

| Engine | Status | Invocation |
|---|---|---|
| Codex CLI | ✅ Supported | `exec --json` / `resume` |
| OpenCode | ✅ Supported | `run --format json` / `--session` |
| Cursor Agent CLI | ✅ Supported | `-p --output-format stream-json` / `--resume` |
| Claude Code | ✅ Supported | `-p --output-format stream-json` / `--resume` |
| Pi | ✅ Supported | `--mode json` / `--session` |
| Kimi | 🚧 Planned | See [Roadmap](docs/en/roadmap.md) |

## Commands

### CLI

```text
nectar [-config PATH]                 Run in the foreground
nectar -version                       Print the version
nectar help                           Show CLI help

nectar service install                Install and start the background service
nectar service start|stop|restart     Start / stop / restart
nectar service status                 Show status
nectar service logs                   Show logs
nectar service uninstall              Uninstall the service

nectar update                         Update to the latest release
nectar schedule list|add|preview|pause|resume|run|cancel|delete|update
nectar skills install|uninstall       Manage agent skills
nectar uninstall                      Uninstall Nectar
```

Aliases: `schedule ls` = `list`, `schedule stop` = `cancel`, `schedule del` / `rm` = `delete`, `skills remove` = `uninstall`. See the [command reference](docs/en/commands.md) for all flags.

### Chat commands

| Command | Purpose |
|---|---|
| `/new`, `/reset` | Start a new session |
| `/status` | Show the current engine, model, and agent thread ID |
| `/stop` | Cancel the current run |
| `/cron`, `/schedule` | Scheduled task entry point |
| `/help`, `/commands` | Show the command card without invoking the engine |

`/cron` subcommands:

```text
/cron list [all]
/cron add <min> <hour> <day> <month> <weekday> <prompt>
/cron every <duration> <prompt>
/cron in <duration> <prompt>
/cron at <RFC3339-time> <prompt>
/cron pause|resume|run|cancel|delete|update <id>
```

See [scheduled tasks](docs/en/schedule.md) for details, or send `/cron help` in chat. Scheduled tasks are enabled by default; no `[schedule]` section is required. Only users in `bot.admin_users` can manage them; an empty list removes that restriction. See [hardening access](#optional-harden-access).

## Maintenance

```bash
nectar update              # keep config and data, replace the binary, restart the service
nectar uninstall           # remove the service, binary, skills, and managed AGENTS.md blocks
nectar uninstall --purge-data
```

See [Install and upgrade](docs/en/install.md) for all update and uninstall flags.

## Docs

- [Engine setup and login checks](docs/en/engines.md)
- [Configuration reference](docs/en/config.md)
- [Install and upgrade](docs/en/install.md)
- [Command reference](docs/en/commands.md)
- [Feishu setup and troubleshooting](docs/en/feishu.md)
- [DingTalk setup and troubleshooting](docs/en/dingtalk.md)
- [Telegram setup and troubleshooting](docs/en/telegram.md)
- [Background service](docs/en/service.md)
- [Scheduled tasks](docs/en/schedule.md)
- [Security boundaries](docs/en/security.md)
- [Roadmap](docs/en/roadmap.md)
- [Changelog](docs/en/changelog.md)
- [中文文档](docs/zh-CN/README.md)

## License

MIT © 2026 zhou.yongguo. See [LICENSE](LICENSE).
