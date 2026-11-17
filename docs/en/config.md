# Configuration Reference

The config file lives at `~/.nectar/config.toml` by default. Except for the selected platform's credentials, every setting has a built-in default and can be omitted.

The installer writes a minimal working config. No manual edits are required after a normal install.

## What the installer writes

| Setting | Written? | Notes |
|---|---|---|
| `bot.engine` | Yes | The engine selected during install |
| `bot.platform` | Yes | The platform selected during install |
| `[feishu]`, `[dingtalk]`, or `[telegram]` credentials | Yes | Entered during install |
| Engine sections (`[codex]`, etc.) | No | Built-in defaults apply |
| `[schedule]` | No | Enabled by default with built-in defaults |
| `bot.admin_users` | No | Empty means admin access is unrestricted |
| `bot.allowed_chats` | No | Empty means group chats are unrestricted |

## Minimal config

Feishu / Lark:

```toml
[bot]
engine = "codex"
platform = "feishu"

[feishu]
app_id = "cli_xxx"
app_secret = "xxx"
```

DingTalk:

```toml
[bot]
engine = "codex"
platform = "dingtalk"

[dingtalk]
client_id = "dingxxx"
client_secret = "xxx"
```

Telegram:

```toml
[bot]
engine = "codex"
platform = "telegram"

[telegram]
bot_token = "your-bot-token"
```

If only one platform is configured, `bot.platform` can be omitted and Nectar detects it from the credentials. Set it explicitly when multiple platforms are configured.

## Access control

```toml
[bot]
admin_users = ["ou_xxx"]
allowed_chats = ["oc_xxx"]
```

- `admin_users`: users allowed to manage `/cron` and the scheduled-task API. Empty means unrestricted.
- `allowed_chats`: groups allowed to use the bot. Empty means unrestricted.
- Feishu uses `open_id` / `chat_id`; DingTalk uses `staffId` / `openConversationId`; Telegram uses numeric `user_id` / `chat_id`.

Restart after editing:

```bash
nectar service restart
```

## Bot

```toml
[bot]
name = "Nectar"          # default: Nectar
engine = "codex"         # codex | opencode | cursor | claude | pi
# platform = "feishu"    # feishu | dingtalk | telegram; optional for a single platform
admin_users = []
allowed_chats = []
# shared | per_chat | per_session | worktree | auto
workspace_mode = "shared"
```

## Multiple bots

One Nectar process can run several bots. Each bot has its own platform identity, engine, workspace, sessions, memory, and access lists. In a group, @mention the bot you want.

```toml
[bots.codex_dev]
name = "Codex Dev"
platform = "feishu"
engine = "codex"
workspace = "~/code/project-a"
workspace_mode = "worktree"

[bots.codex_dev.feishu]
app_id = "cli_xxx"
app_secret = "xxx"

[bots.claude_review]
name = "Claude Review"
platform = "feishu"
engine = "claude"
workspace = "~/code/project-a"

[bots.claude_review.feishu]
app_id = "cli_yyy"
app_secret = "yyy"
```

Rules:

- Each bot needs its own platform app or token. Duplicate credentials are rejected so one message is not processed twice.
- `[bots.<id>]` defaults to `~/.nectar/bots/<id>` and can be overridden with `data_dir`. Adding or removing another bot never changes an existing bot's default data path. The legacy `[bot]` continues to use `storage.data_dir`.
- Different chats for the same bot run concurrently; sessions inside one chat are serialized. Direct messages and group chats do not block each other.
- New bots default to `workspace_mode = "auto"`: a committed git repository gets one worktree per session, otherwise Nectar falls back to `per_chat`.
- `workspace = "..."` overrides the engine working directory; `model = "..."` overrides the selected engine model.
- An omitted `engine` inherits `[bot].engine`; if that is also unset, Nectar defaults to `codex`.
- The legacy `[bot]` plus global `[feishu]` / `[codex]` config keeps working. Adding `[bots.*]` starts both the legacy default bot and the new bots.

`workspace_mode` accepts `shared`, `per_chat`, `per_session`, `worktree`, or `auto`. `worktree` creates a `nectar/<bot>/<session>` branch and directory from the repository's current `HEAD`; worktrees live under `~/.nectar/worktrees/<bot>/<session>`.

`shared` only means every chat uses the same directory; it does not serialize runs. Different chats still run concurrently, while sessions in one chat are serialized. Use `worktree` when two chats may edit the same files.

A worktree starts from the committed `HEAD`; uncommitted changes in the main workspace are not copied.

Worktrees are reused per session. Use `git -C <repo> worktree list` to inspect them and `git -C <repo> worktree remove <path>` to clean up.

`per_chat` / `per_session` let multiple agents work in the same directory concurrently. They are a good fit for read-only, analysis, or non-overlapping tasks; use `worktree` when agents edit the same repository in parallel.

## Multi-bot relay

Relay is disabled by default. When enabled, bots in the same Nectar process can ask one another through the local bus, while the group gets a visible mirror. The bus is the source of truth; the IM message is only for visibility.

```toml
[relay]
enabled = true
echo = "summary"              # full | summary | off
max_generation = 4
max_envelopes_per_root = 12
max_runs_per_root = 8
root_ttl = "30m"
session = "per_root"

[bots.coder]
name = "Codex Coder"
platform = "feishu"
engine = "codex"
workspace = "~/code/project"
workspace_mode = "shared"
role = "coder"
description = "Implement features and change code"
capabilities = ["code", "git", "test"]
relay_accept_from = ["reviewer"]
relay_prompt = "full"

[bots.reviewer]
name = "Claude Reviewer"
platform = "feishu"
engine = "claude"
workspace = "~/code/project"
workspace_mode = "shared"
role = "reviewer"
description = "Read-only diff review and risk analysis"
capabilities = ["review", "security"]
relay_accept_from = ["coder"]
relay_prompt = "minimal"
```

Rules:

- `relay.enabled` defaults to `false`. Once enabled, `nectar relay bots` lists the in-process roster.
- The target bot uses a detached per-root session; replies return to the origin session that sent the `ask`, preserving its workspace and engine context.
- M0 recommends a shared workspace for both bots so the reviewer sees uncommitted changes. Worktree artifact handoff comes later.
- `relay_accept_from` is the receiver allowlist. When empty, only same-process, same-chat, known bots are accepted; configure it explicitly for a new bot's first relay.
- `relay_prompt` accepts `full`, `minimal`, or `none`. `none` only disables Nectar's identity/peers/memory/skill injections; the engine's own `AGENTS.md` and skills can still load.
- `echo = "off"` suppresses the target bot and mirror only; the sender's origin turn still runs and displays.
- M0 drops sibling-bot mirrors/cards using Feishu `sender_type=bot`, with an invisible marker as fallback. Real `@` mentions still need a chat-member open_id cache later.

## Engines

Every section below can be omitted; defaults apply.

### Codex

```toml
[codex]
path = "codex"
backend = "app-server"         # app-server | exec; default enables /steer
model = ""                    # empty = model from Codex's own config
effort = ""                   # e.g. high; passed as model_reasoning_effort
sandbox = "workspace-write"   # read-only / workspace-write / danger-full-access
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = auto-detect from the Codex model catalog
per_chat_workspace = false
bypass_approvals = false      # use with care
```

With the default `backend = "app-server"`, `max_output_bytes` limits one JSON-RPC event. Total stdout for a turn is streamed and is not capped by this setting. With `backend = "exec"`, it limits total JSONL output.

### OpenCode

```toml
[opencode]
path = "opencode"
model = ""                    # e.g. provider/model#variant
agent = ""                    # optional OpenCode agent
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = read from OpenCode config
standalone = false            # shared server by default; enables /steer
server_url = ""               # optional shared server for run/export/steer; overrides standalone
auto_approve = true           # required for non-interactive bot use
per_chat_workspace = false
```

When `model` is empty, Nectar tries to resolve the default model from `~/.config/opencode/opencode.json`. The context limit comes from the model's `limit.context`; set `context_window` manually when the model is not declared there.

OpenCode uses its shared server by default and steers with `delivery = "steer"`. When `server_url` is set, normal runs, usage, and steering all use that server and `standalone` is ignored. A private server is used only when `standalone = true` and no `server_url` is set; in that case `/steer` is unavailable.

### Cursor Agent

```toml
[cursor]
path = "agent"
model = ""                    # e.g. composer-2.5; empty = Cursor default
mode = ""                     # plan / ask; empty = normal agent mode
sandbox = ""                  # enabled / disabled; empty = Cursor default
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = unknown; footer omits context usage
auto_approve = true           # same as -f
trust_workspace = true        # same as --trust
per_chat_workspace = false
```

`path` defaults to `agent` and falls back to `cursor-agent`. Set `context_window` explicitly if you want context usage in the footer.

### Claude Code

```toml
[claude]
path = "claude"
backend = "stream-json"       # stream-json | print; default enables /steer
model = ""                    # e.g. claude-sonnet-4-5; empty = Claude Code default
fallback_model = ""           # optional fallback model
permission_mode = ""          # acceptEdits | plan | bypassPermissions | manual | dontAsk | auto
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = read from the result event
max_turns = 0                 # 0 = no limit
auto_approve = true           # same as --dangerously-skip-permissions
per_chat_workspace = false
```

Token usage and the context limit come from the final `result` event; `context_window` is only an override.

### Pi

```toml
[pi]
path = "pi"
backend = "rpc"               # rpc | json; default enables /steer
provider = ""                 # optional provider override
model = ""                    # e.g. claude-sonnet-4-5; empty = Pi default
thinking = ""                 # off | minimal | low | medium | high | xhigh | max
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = unknown; footer omits context usage
trust_workspace = true        # same as --approve
per_chat_workspace = false
```

`trust_workspace = true` lets Pi load project-local settings, skills, context files, and extensions.

## Storage

```toml
[storage]
data_dir = "~/.nectar"
```

`data_dir` stores sessions, scheduled tasks, workspaces, attachments, and skill backups.

## Memory

```toml
[memory]
enabled = true                # false disables storage, commands, and injection
default_scope = "bot"         # bot | chat | project; bot shares across every chat of this bot
max_items = 6                 # maximum memories injected per turn
max_chars = 1500              # maximum memory context characters per turn
auto_learn_failures = true    # record failures as pending candidates for review
```

Memories are stored in `~/.nectar/memory/memory.json`. By default `/remember` writes bot-scoped memory shared by every chat handled by the same bot. Set `default_scope = "chat"` to isolate memories by chat, or use `/remember chat <text>` for one entry. `/remember project` writes project-scoped memory. Failure lessons start as `candidate` and only join retrieval after `/memory approve <id>`.

Writing bot-scoped memory requires an admin user; an empty `admin_users` list removes that restriction.

Agents read and write memory through the `nectar memory` CLI. `nectar skills install` also installs the `nectar-memory` skill and its managed `AGENTS.md` instructions so the agent can handle natural-language requests in any supported language.

Memories record a `locality` (portable / templated / device), a stable `project_id`, revision, and content hash. Project/home paths become `${PROJECT_ROOT}` / `${HOME}`; other absolute paths are marked device-local so future sync does not assume they exist everywhere.

## Scheduled tasks

Scheduled tasks are enabled by default; no `[schedule]` section is required. Optional overrides:

```toml
[schedule]
enabled = true                # false disables the scheduler and local API socket
timezone = "Local"            # default cron timezone, e.g. "Asia/Shanghai"
startup_grace = "5m"          # run missed tasks at most this old after restart
history_limit = 100           # completed/failed records to keep; 0 = unlimited
max_concurrent = 4            # simultaneous runs; 0 = unlimited
jitter = "0s"                 # random delay cap for recurring runs
timeout = "30m"               # default per-run timeout; 0s disables it
```

See [Scheduled tasks](schedule.md) for the semantics.

## Local console

The dashboard is disabled by default. When enabled it binds to loopback and
requires token authentication:

```toml
[dashboard]
enabled = true
listen = "127.0.0.1:8787"
# Defaults to ~/.nectar/run/dashboard.token; generated with 0600 on first start.
token_file = "~/.nectar/run/dashboard.token"
# Allow a non-loopback listener; put it behind a TLS reverse proxy if enabled.
allow_remote = false
# true exposes the console in read-only mode.
read_only = false
```

After Nectar starts, run:

```bash
nectar dashboard
```

The command prints a one-time login URL with a token. The console provides bot
status overview, memory search and review, scheduled-task management, and a
read-only view of effective configuration. Platform credentials and engine
secrets are never returned to the browser. The console does not edit the config
file; restart the service after changing `config.toml`.

The console defaults to English. Use the Language selector in the top bar to
switch to Chinese; the choice is remembered in the current browser.

The listener is restricted to `127.0.0.1` / `localhost` / loopback IPs by
default. Set `allow_remote = true` only when you have an HTTPS reverse proxy.
The dashboard token is equivalent to an admin password.

## Environment overrides

Environment variables override the corresponding config values:

```bash
export FEISHU_APP_ID=cli_xxx
export FEISHU_APP_SECRET=xxx
export DINGTALK_CLIENT_ID=dingxxx
export DINGTALK_CLIENT_SECRET=xxx
export TELEGRAM_BOT_TOKEN=your-bot-token
export TELEGRAM_PROXY=http://127.0.0.1:7890

export CODEX_PATH=codex
export OPENCODE_PATH=opencode
export CURSOR_AGENT_PATH=agent
export CLAUDE_PATH=claude
export PI_PATH=pi

export NECTAR_ENGINE=codex
export NECTAR_DATA_DIR=~/.nectar
export NECTAR_WORKING_DIR=~/.nectar/workspace
```

Service mode reads platform credentials from the config file and does not inherit `FEISHU_*` / `DINGTALK_*` / `TELEGRAM_*` from the shell.

## Context usage

The footer's context percentage depends on token usage and the context limit reported by the engine:

- Codex: `context_window = 0` resolves from the Codex model catalog.
- OpenCode: read from the model's `limit.context`.
- Claude Code: read from the final `result` event.
- Cursor and Pi: unknown by default; set `context_window` explicitly.

When the limit is unknown, the footer omits context usage. Agent execution is unaffected.

Back to: [English README](../../README.md) · [Engine setup](engines.md) · [Commands](commands.md)
