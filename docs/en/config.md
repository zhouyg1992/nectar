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
```

## Engines

Every section below can be omitted; defaults apply.

### Codex

```toml
[codex]
path = "codex"
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
standalone = true             # private server per run
auto_approve = true           # required for non-interactive bot use
per_chat_workspace = false
```

When `model` is empty, Nectar tries to resolve the default model from `~/.config/opencode/opencode.json`. The context limit comes from the model's `limit.context`; set `context_window` manually when the model is not declared there.

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
max_items = 6                 # maximum memories injected per turn
max_chars = 1500              # maximum memory context characters per turn
auto_learn_failures = true    # record failures as pending candidates for review
```

Memories are stored in `~/.nectar/memory/memory.json`. Explicit `/remember` entries become active immediately; failure lessons start as `candidate` and only join retrieval after `/memory approve <id>`.

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
