# Command Reference

Nectar has two command surfaces:

- CLI commands: run `nectar ...` in a terminal.
- Chat commands: send `/new`, `/cron`, and similar messages in Feishu / DingTalk.

## CLI overview

```text
nectar [-config PATH]                 Run the bot in the foreground
nectar -version                       Print the version
nectar help                           Show CLI help
nectar bots                           List configured bots
nectar service <subcommand>           Manage the background service
nectar update                         Update Nectar
nectar schedule <subcommand>          Manage scheduled tasks
nectar skills <subcommand>            Manage agent skills
nectar memory <subcommand>            Manage local memory
nectar dashboard [--config PATH]      Print the local console login URL
nectar uninstall                      Uninstall Nectar
```

Global flags:

| Flag | Description |
|---|---|
| `-config PATH` / `--config PATH` | Config path, default `~/.nectar/config.toml` |
| `-version` / `--version` | Print the version and exit |
| `-h` / `--help` | Show help |

> The installer starts the background service by default. To run the bot in the foreground, stop the service first with `nectar service stop` to avoid two instances connecting to the platform.

## Local console

```bash
nectar dashboard [--config PATH] [--json] [--no-browser]
```

Enable it first:

```toml
[dashboard]
enabled = true
listen = "127.0.0.1:8787"
```

Restart Nectar and run `nectar dashboard` to print a tokenized login URL and
open it in the default browser. Use `--no-browser` to print the link only, or
`--json` to return `url`, `listen`, and `token_file` without opening a browser.
The token file is generated with `0600` permissions on first start. The listener
is loopback-only by default; remote access requires `allow_remote = true` plus
an HTTPS reverse proxy.

The console covers bot status, memory search, approve/reject/restore for
pending memories, batch review, pin/edit/delete, scheduled-task
CRUD/pause/resume/run, and a read-only effective-config view. Status counts are
clickable filters, and tags can be clicked to filter the list. The memory editor
can also move an item between `bot`, `project`, and `chat` scopes. Memory and
scheduled-task lists show 50 items per page with previous/next controls.
Configuration changes still belong in `config.toml` and require a service
restart.

## Multiple bots

```bash
nectar bots [-config PATH]
```

Lists the `[bot]` and `[bots.*]` entries after resolution, including ID, platform, engine, workspace, workspace mode, and data directory. `nectar` or the background service still starts all of them.

## Worktrees

```bash
nectar worktrees list [--config PATH] [--bot ID] [--json]
nectar worktrees remove <session-id|path> [--config PATH] [--bot ID] [--force] [--delete-branch]
nectar worktrees remove --all [--config PATH] [--bot ID] [--force] [--delete-branch]
```

Lists or removes worktrees for the selected bot (`workspace_mode = worktree`, or `auto` on a git repository). Branches are kept by default; dirty worktrees require `--force`, and `--delete-branch` also removes the `nectar/<bot>/<session>` branch.

## Background service

```bash
nectar service install [--config PATH] [--binary PATH] [--linger] [--no-start]
nectar service start [--config PATH] [--binary PATH]
nectar service stop [--config PATH] [--binary PATH]
nectar service restart [--config PATH] [--binary PATH]
nectar service status [--config PATH] [--binary PATH]
nectar service logs [--config PATH] [--binary PATH] [-n N] [-f] [--bot ID]
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
| `--bot ID` | Show logs for one bot |

Examples:

```bash
nectar service status
nectar service logs -n 500
nectar service logs -f
nectar service restart
```

With multiple bots, `service status` also prints each configured bot with its platform, engine, workspace, and data directory.

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
nectar update --version v0.10.2
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

With multiple bots, pass `--bot ID` to target one bot. `schedule list --all-bots` queries every bot and prints its ID and name.

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

The installer writes both the `nectar-schedule` and `nectar-memory` agent
skills and maintains their managed blocks in workspace `AGENTS.md` files.

## Memory

`nectar memory` reads and writes the local memory store. Agents call it as a
tool; it is also useful for manual inspection:

```bash
nectar memory add [--kind profile|project|fact|lesson|episode] \
  [--scope bot|chat|project|global] [--status candidate|active] \
  [--tags a,b] [--source agent] <text>
nectar memory search [--scope bot|chat|project|global|all] [--limit N] <query>
nectar memory list [--status active|pending|archived|all] [--json]
nectar memory get <id> [--json]
nectar memory update <id> <new text> [--json]
nectar memory approve|reject|pin|unpin <id>
nectar memory forget <id> --confirm
```

With multiple bots, pass `--bot ID` to select that bot's data directory and access policy, or keep using `--socket` to target a specific bot API socket.

- `--status candidate` is the default; candidates join retrieval only after approval. Use `--status active` when the user explicitly asks to remember something.
- Scope follows `[memory] default_scope` (default `bot`, shared by every chat of the bot). Pass `chat` explicitly to keep an entry in one conversation.
- Agent sessions call the daemon through `NECTAR_API_SOCKET`, so sandboxed agents do not need write access to `~/.nectar/memory`; manual CLI without a socket reads/writes the local file.
- `bot` / `global` memories can only be written, approved, pinned, or deleted by admins.
- In group chats, non-admin writes become candidates and review/pin/delete operations require an admin.
- Deletion requires `--confirm` so agents cannot remove memory by accident.
- Project/home paths are normalized to `${PROJECT_ROOT}` / `${HOME}`; other absolute paths are marked device-local.

## Uninstall

```bash
nectar uninstall [--config PATH] [--binary PATH] [--purge-data] [--force-worktrees] [--keep-binary] [--no-skills] [--dry-run]
```

| Flag | Description |
|---|---|
| `--purge-data` | Also remove config, task data, workspaces, and backups |
| `--force-worktrees` | With `--purge-data`, remove worktrees that contain uncommitted changes |
| `--keep-binary` | Keep the binary |
| `--no-skills` | Keep agent skills and managed `AGENTS.md` blocks |
| `--dry-run` | Preview cleanup |
| `--config PATH` | Config path |
| `--binary PATH` | Binary path |

## Chat commands

| Command | Purpose |
|---|---|
| `/new [name]`, `/reset` | Start a new session; previous sessions stay listed |
| `/list [all] [limit]` | List sessions for this chat; `all` includes archived sessions, default limit 50 |
| `/switch <id\|#\|name>` | Switch the active session |
| `/current` | Show the active session |
| `/rename <name>` | Rename the active session |
| `/archive [id]` | Archive a session; the next message starts a new one if it was active |
| `/unarchive <id\|name>` | Restore an archived session |
| `/status` | Show the current engine, model, and session |
| `/about`, `/whoami` | Show the Nectar version, platform, underlying engine, and model |
| `/stop` | Cancel the current run |
| `/steer <text>`, `/append <text>`, `/ps <text>`, `/btw <text>` | Insert text into a running turn; requires engine support |
| `/cron`, `/schedule` | Scheduled task entry point |
| `/memory` | Inspect, review, pin, or forget memories |
| `/remember` | Explicitly save a memory |
| `/help`, `/commands` | Show the command card without invoking the engine |

`/help`, `/commands`, and short questions such as "what commands are available" return the command card without invoking the engine.

Steering only applies to the explicit commands. Ordinary messages still queue while a task is running. Codex, Claude Code, Pi, and OpenCode use steering-capable transports by default. Steering is disabled only when Codex is explicitly set to `exec`, Claude to `print`, Pi to `json`, or OpenCode uses `standalone = true` without a `server_url`. Unsupported engines such as Cursor return an explicit error instead of silently queueing.

### Sessions

Nectar keeps a list of sessions per chat, thread, and engine. `/new` creates a
new active session without deleting the previous one. `/list` shows the most
recent sessions with their short id, agent, context usage, and last activity.
Use `/switch <id>`, `/switch #2`, or `/switch <name>` to choose another active
session. Numeric shortcuts only count non-archived sessions; archived sessions
are shown by `/list all` with an `archived:` prefix. `/rename`, `/archive`, and
`/unarchive` manage the list.

In group chats, `/new`, `/list`, `/switch`, `/current`, `/rename`, `/archive`,
`/unarchive`, `/status`, and `/stop` require a user in `bot.admin_users`.
Direct chats keep the normal single-user behavior.

Sessions are persisted in `~/.nectar/sessions.json`. Version 1 files are
migrated automatically on startup, and the next write uses the version 2
layout.

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

### /memory

```text
/memory
/memory list [active|pending|archived|all]
/memory search <query>
/memory show <id>
/memory approve <id>
/memory reject <id>
/memory pin <id>
/memory unpin <id>
/memory forget <id> [confirm]
/remember [bot|chat|project] <text>
/forget <id> [confirm]
```

- `/memory` without a subcommand is equivalent to `/memory list`.
- Failed runs are stored as `pending` candidates and are not injected until approved.
- `/remember` writes bot-scoped memory by default (shared by every chat of this bot); `chat` keeps it in the current chat; `project` writes to the current workspace; `global` is a compatibility alias for `bot` and requires an admin.
- Global memories can only be approved, pinned, or deleted by an admin.
- In group chats, non-admin `/remember` creates a pending candidate; review and deletion require an admin.
- `/memory forget` is a two-step confirmation: the first call returns a confirmation card; the second call with `confirm` performs the deletion.

Memories are stored in `~/.nectar/memory/memory.json` for inspection and backup.

## Agent session environment

Agent sessions receive:

| Variable | Description |
|---|---|
| `NECTAR_CHANNEL` | Current platform |
| `NECTAR_CHAT_ID` | Current chat ID |
| `NECTAR_THREAD_ID` | Current thread ID |
| `NECTAR_AGENT_ID` | Current engine |
| `NECTAR_WORKING_DIR` | Current workspace |
| `NECTAR_API_SOCKET` | Schedule API socket |
| `NECTAR_CLI` | Absolute path to the Nectar binary |
| `NECTAR_USER_ID` | Current user ID |

Agents can usually call `nectar schedule` without passing the target chat explicitly.

Back to: [English README](../../README.md) · [Configuration](config.md) · [Install and upgrade](install.md)
