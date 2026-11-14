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

Nectar sends chat messages to a coding agent on your machine and streams the process and result back as a card. Sessions stay continuous, progress is visible in real time, and tasks can run on a schedule. The installer detects installed engines, writes the config, and starts the background service.

<p align="center">
  <img src="docs/assets/nectar-flow.svg" alt="Feishu, DingTalk, and Telegram messages flow through Nectar to Codex, OpenCode, Cursor Agent, Claude Code, or Pi" width="860">
</p>

## Why Nectar

- **Chat is the interface**: talk to your agent from Feishu / Lark, DingTalk, or Telegram, in direct messages or group chats.
- **Sessions stay alive**: session mappings persist in `~/.nectar/sessions.json`, so the service resumes the same agent session after a restart.
- **Progress is visible**: 🌼 Thinking → 🐝 Working → 🍯 Complete / ⚠️ Failed, updated while the agent runs.
- **Tasks can be scheduled**: cron, fixed intervals, and one-shot runs, managed from chat or the CLI.
- **Bring your own engine**: choose Codex, Claude Code, Cursor Agent, OpenCode, or Pi.

## Quick start

### 1. Prepare one coding agent

Install and log in to **one** of: **Codex, Claude Code, Cursor Agent, OpenCode, or Pi**. The installer detects installed engines.

→ [Engine setup and login checks](docs/en/engines.md)

### 2. Install

macOS / Linux (amd64 / arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

The installer is interactive: choose an engine and platform (Feishu / Lark by default), enter the credentials, write `~/.nectar/config.toml`, then install and start the background service.

> The installer starts the background service by default. You do not need to run `nectar` manually.

### 3. Verify

```bash
nectar service status
nectar service logs -f
```

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

> An empty `admin_users` does not limit admin access, so anyone can manage `/cron`. An empty `allowed_chats` does not limit group chats. See the [configuration reference](docs/en/config.md).

## Support matrix

**Platforms**

| Platform | Status | Setup |
|---|---|---|
| Feishu / Lark | ✅ Supported | [Feishu setup](docs/en/feishu.md) |
| DingTalk | ✅ Supported | [DingTalk setup](docs/en/dingtalk.md) |
| Telegram | ✅ Supported | [Telegram setup](docs/en/telegram.md) |
| WeCom / WeChat, Slack | 🚧 Planned | [Roadmap](docs/en/roadmap.md) |

**Engines**

| Engine | Status | Invocation |
|---|---|---|
| Codex CLI | ✅ Supported | `exec --json` / `resume` |
| OpenCode | ✅ Supported | `run --format json` / `--session` |
| Cursor Agent CLI | ✅ Supported | `-p --output-format stream-json` / `--resume` |
| Claude Code | ✅ Supported | `-p --output-format stream-json` / `--resume` |
| Pi | ✅ Supported | `--mode json` / `--session` |
| Kimi Code | 🚧 Planned | See [Roadmap](docs/en/roadmap.md) |

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
/cron pause <id>
/cron resume <id>
/cron run <id>
/cron cancel <id>
/cron delete <id>
/cron update <id> ...
/cron help
```

Scheduled tasks are enabled by default; no `[schedule]` section is required. Management commands are limited to `bot.admin_users`; when that list is empty, admin access is not restricted.

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
