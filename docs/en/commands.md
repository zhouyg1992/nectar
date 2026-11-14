# Command Reference

Nectar has two command surfaces:

- CLI commands: run `nectar ...` in a terminal.
- Chat commands: send `/new`, `/cron`, and similar messages in Feishu / DingTalk.

## CLI overview

```text
nectar [-config PATH]                 Run the bot in the foreground
nectar -version                       Print the version
nectar help                           Show CLI help
nectar service <subcommand>           Manage the background service
nectar update                         Update Nectar
nectar schedule <subcommand>          Manage scheduled tasks
nectar skills <subcommand>            Manage agent skills
nectar uninstall                      Uninstall Nectar
```

Global flags:

| Flag | Description |
|---|---|
| `-config PATH` / `--config PATH` | Config path, default `~/.nectar/config.toml` |
| `-version` / `--version` | Print the version and exit |
| `-h` / `--help` | Show help |

> The installer starts the background service by default. To run the bot in the foreground, stop the service first with `nectar service stop` to avoid two instances connecting to the platform.

## Background service

```bash
nectar service install [--config PATH] [--binary PATH] [--linger] [--no-start]
nectar service start [--config PATH] [--binary PATH]
nectar service stop [--config PATH] [--binary PATH]
nectar service restart [--config PATH] [--binary PATH]
nectar service status [--config PATH] [--binary PATH]
nectar service logs [--config PATH] [--binary PATH] [-n N] [-f]
nectar service uninstall [--config PATH] [--binary PATH]
```

| Flag | Description |
|---|---|
| `--config PATH` | Config file used by the service |
| `--binary PATH` | Nectar binary used by the service |
| `--linger` | Enable systemd user linger on Linux |
| `--no-start` | Install and enable without starting immediately |
| `-n N` | Number of log lines, default 200 |
| `-f` | Follow logs |

Examples:

```bash
nectar service status
nectar service logs -n 500
nectar service logs -f
nectar service restart
```

## Update

```bash
nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart] [--no-skills]
  [--config PATH] [--binary PATH] [--timeout 10m] [--attempts 2] [--no-progress]
```

| Flag | Description |
|---|---|
| `--check` | Check the latest release without installing |
| `--force` | Reinstall even when already on the target version |
| `--version vX.Y.Z` | Install a specific release; also used for downgrades |
| `--no-restart` | Update the binary and skills without restarting the service |
| `--no-skills` | Skip the agent skill refresh |
| `--config PATH` | Config path used to detect the installed service |
| `--binary PATH` | Binary path to update; defaults to the current executable |
| `--timeout 10m` | Timeout for each download attempt, default 10 minutes |
| `--attempts 2` | Maximum metadata and download attempts |
| `--no-progress` | Disable the download progress bar |

Examples:

```bash
nectar update
nectar update --check
nectar update --version v0.7.0
nectar update --no-restart
nectar update --timeout 15m --attempts 3
```

Asset downloads show a percentage progress bar. Transient redirect, TLS, and connection failures are retried automatically.

## Scheduled tasks

The CLI talks to the running bot over `~/.nectar/run/api.sock`, so the service must be running.

### list

```bash
nectar schedule list [--all] [--json] [--socket PATH]
```

| Flag | Description |
|---|---|
| `--all` | Include completed and failed tasks |
| `--json` | Print JSON |
| `--socket PATH` | Override the schedule API socket |

### add

```bash
nectar schedule add \
  (--cron "0 9 * * 1-5" | --every 90m | --in 30m | --at "2026-11-14T15:00:00+08:00") \
  --prompt "task prompt" \
  [--desc "description"] [--timeout 30m] [--timezone Asia/Shanghai] \
  [--channel feishu] [--chat CHAT_ID] [--thread THREAD_ID] [--agent codex] \
  [--json] [--socket PATH]
```

Choose exactly one of `--cron`, `--every`, `--in`, or `--at`.

Target defaults come from `NECTAR_CHANNEL`, `NECTAR_CHAT_ID`, `NECTAR_THREAD_ID`, and `NECTAR_AGENT_ID`, which Nectar injects into agent sessions.

### preview

```bash
nectar schedule preview --cron "0 9 * * 1-5" [--timezone Asia/Shanghai] [--json]
```

Calculates the next run times without creating a task. It accepts the same time flags as `add`.

### Manage

```bash
nectar schedule pause <id>
nectar schedule resume <id>
nectar schedule run <id>
nectar schedule cancel <id>
nectar schedule delete <id>
```

- `run`: run once now without changing the original schedule.
- `cancel`: cancel the current run and keep the task.
- `delete`: remove the task and stop future scheduling.

Aliases: `ls` = `list`, `stop` = `cancel`, `del` / `rm` = `delete`.

### update

```bash
nectar schedule update <id> \
  [--prompt "new prompt"] [--desc "new description"] [--timeout 15m] \
  [--cron "0 10 * * 1-5" | --every 2h | --at "..." | --in 30m] \
  [--timezone Asia/Shanghai] [--json] [--socket PATH]
```

At most one of `--cron`, `--every`, `--at`, or `--in` may be set.

## Agent skills

```bash
nectar skills install [--config PATH] [--data-dir DIR] [--no-workspace] [--quiet]
nectar skills uninstall [--config PATH] [--data-dir DIR] [--no-workspace] [--dry-run] [--quiet]
```

| Flag | Description |
|---|---|
| `--config PATH` | Config path |
| `--data-dir DIR` | Nectar data directory |
| `--no-workspace` | Do not modify workspace `AGENTS.md` |
| `--dry-run` | Preview uninstall changes |
| `--quiet` | Suppress non-error output |

`remove` is an alias for `uninstall`.

## Uninstall

```bash
nectar uninstall [--config PATH] [--binary PATH] [--purge-data] [--keep-binary] [--no-skills] [--dry-run]
```

| Flag | Description |
|---|---|
| `--purge-data` | Also remove config, task data, workspaces, and backups |
| `--keep-binary` | Keep the binary |
| `--no-skills` | Keep agent skills and managed `AGENTS.md` blocks |
| `--dry-run` | Preview cleanup |
| `--config PATH` | Config path |
| `--binary PATH` | Binary path |

## Chat commands

| Command | Purpose |
|---|---|
| `/new`, `/reset` | Clear the current session; the next message starts a new thread |
| `/status` | Show the current engine, model, and agent thread ID |
| `/stop` | Cancel the current run |
| `/cron`, `/schedule` | Scheduled task entry point |
| `/help`, `/commands` | Show the command card without invoking the engine |

`/help`, `/commands`, and short questions such as "what commands are available" return the command card without invoking the engine.

### /cron

```text
/cron
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
/cron update <id> prompt|cron|every|at|timezone|timeout ...
/cron help
```

`/cron` without a subcommand is equivalent to `/cron list`. Aliases: `ls` = `list`, `stop` = `cancel`, `del` / `rm` = `delete`, `?` = `help`.

`/cron update` forms:

```text
/cron update <id> prompt <new prompt>
/cron update <id> cron <min> <hour> <day> <month> <weekday> [prompt]
/cron update <id> every <duration> [prompt]
/cron update <id> at <RFC3339-time> [prompt]
/cron update <id> timezone <IANA-timezone>
/cron update <id> timeout <duration|0s>
```

Scheduled tasks are enabled by default. Management commands are limited to `bot.admin_users`; when that list is empty, admin access is unrestricted. See [Scheduled tasks](schedule.md).

## Agent session environment

Agent sessions receive:

| Variable | Description |
|---|---|
| `NECTAR_CHANNEL` | Current platform |
| `NECTAR_CHAT_ID` | Current chat ID |
| `NECTAR_THREAD_ID` | Current thread ID |
| `NECTAR_AGENT_ID` | Current engine |
| `NECTAR_API_SOCKET` | Schedule API socket |
| `NECTAR_CLI` | Absolute path to the Nectar binary |
| `NECTAR_USER_ID` | Current user ID |

Agents can usually call `nectar schedule` without passing the target chat explicitly.

Back to: [English README](../../README.md) · [Configuration](config.md) · [Install and upgrade](install.md)
