# Changelog

## Unreleased

### Fixed

- A run's relay root is now persisted on its token, so repeated `relay send` calls from one turn share one root, budgets and default idempotency keys instead of creating a new root every time; duplicate sends no longer journal or keep an empty root
- Relay enforces the target bot's `allowed_chats`, so a chat its adapter would refuse cannot reach it through relay; startup warns when a target has `admin_users` but an open relay allowlist
- Mention-name matching is deterministic (exact match first, unique containment only), persistence writes are serialized, and roots older than 24 hours are pruned on startup
- Relay runs carry the originating user id, so memory and admin-gated operations inside the target run are checked against the real requester
- Relay refuses direct chats by default with `ERR_DIRECT_CHAT`; `[relay] allow_direct` opts in and forces silent target output without a mirror
- A target answer now reaches the origin session even when its group echo fails (for example, the bot is not in that chat) instead of reporting a failed relay
- A relay target that waits on the shared workspace lock while the sending turn refuses to end is unblocked after `relay.sender_grace` (default 60s) by canceling the sender turn; the result still returns as a new origin turn
- Origin reply turns no longer receive a relay token, so an agent cannot start another hop while holding the workspace lock and deadlock against the target
- In summary mode the hand-off card is updated in place from running to `🔁 Relay result` instead of posting a second message; full mode closes the card with a completion note, and `echo = "off"` sends no hand-off card
- The prompt fence uses a randomized tag so relay bodies cannot close it early; mirror-title detection only matches a prefix; bot-authored messages are dropped only while relay is enabled; canceled envelopes keep the canceled state
- The local relay API returns the documented `ERR_DISABLED` code, so the CLI can show it instead of a bare error
- Card hand-off mirrors keep the invisible marker in the body, and the visible mirror title is also recognized on inbound, so platforms that strip zero-width characters still drop sibling mirrors

### Added

- Multi-bot relay: bots in one process can hand work to each other over a local, token-bound bus (`[relay]`), with role-based rosters, per-root generation/envelope/run budgets, idempotency and restart recovery; disabled by default
- `nectar relay bots|send|roots|show|cancel` and the admin chat command `/relay stop`
- Relay replies return to the sender's origin session, while the target runs in a detached per-root session; relay bodies never parse as slash commands
- Group messages that mention several configured bots are handled only by the first mentioned bot (`relay.first_mention_only`), and an opposite-direction ask started by the same human message is refused with `ERR_MUTUAL_RELAY`
- Hand-off mirrors no longer show a visible marker; rejected sends no longer journal empty roots, and empty roots are pruned on load
- Relay no longer refuses a target that has not been seen in the chat yet; membership is only logged, so a single @ on the initiator is enough
- `echo = "summary"` renders the target's final answer as one `🔁 Relay result` card (error state on failure) instead of plain text
- Group hand-off mirrors on Feishu, DingTalk and Telegram; bot-authored messages and marked mirrors are dropped before anchors or command parsing
- `nectar-relay` agent skill, installed alongside the schedule and memory skills

## v0.10.1 — 2026-11-17

### Fixed

- `max_output_bytes` for the Codex app-server backend now limits each JSON-RPC event instead of a turn's cumulative stdout, so long tasks are not rejected for total output; oversized single events still fail with a precise error and cancel the process
- Config docs and `config.example.toml` now clarify `max_output_bytes` semantics for the app-server and exec backends

## v0.10.0 — 2026-11-16

### Added

- Multiple bots: one Nectar process runs several bots, each with its own platform identity, engine, workspace, sessions, memory, and permissions; configure them under `[bots.<id>]`, and duplicate platform credentials refuse to start
- Each bot has an isolated data directory (default `~/.nectar/bots/<id>`), so adding or removing bots does not move existing ones
- Per-session git worktrees: `workspace_mode` now supports `per_chat`, `per_session`, `worktree`, and `auto`; `worktree` creates a `nectar/<bot>/<session>` branch and separate checkout per session
- `nectar bots`, `nectar worktrees list|remove`, `nectar service status|logs --bot`, `nectar schedule --bot|--all-bots`, and `nectar memory --bot`
- Local dashboard console: `[dashboard]` is disabled by default; when enabled it uses token login for bot status, memory search/review/pin/delete, scheduled task management, and read-only effective config
- `nectar dashboard` prints a tokenized login URL and opens the browser by default, with `--json` and `--no-browser`

### Changed

- The default memory scope changes from per-chat to `bot`, shared by every direct and group chat handled by the same bot; use `[memory] default_scope = "chat"` or `/remember chat` for per-chat isolation
- `/remember global` is now a compatibility alias for `bot`; writing, reviewing, and deleting bot-scoped memories requires admin rights
- `/memory forget` and `/forget` now require two-step confirmation, with the first call returning a confirmation card
- The dashboard listens on loopback only by default and generates a `0600` token file on first start; remote access needs `allow_remote = true` behind an HTTPS reverse proxy, and `read_only = true` disables dashboard mutations
- Workspace locks are now shared across bots; different chats still run concurrently, while runs within one chat are serialized per session
- With multiple bots, `nectar uninstall --purge-data` also prunes worktrees, and `--force-worktrees` force-removes checkouts with uncommitted changes

## v0.9.0 — 2026-11-16

### Added

- Steering during a running task: `/steer`, `/append`, `/ps`, and `/btw` append extra context to the active run; only explicit commands steer, while ordinary messages still queue
- Codex app-server backend using `turn/start`, `turn/steer`, and `turn/interrupt` for steering
- Steering backends for Claude `stream-json`, Pi `rpc`, and OpenCode shared servers
- Outbound media: agents write images or files to `.nectar/outbox/` (`NECTAR_OUTBOX_DIR`) in the workspace, and Nectar sends them to Feishu, DingTalk, and Telegram after the run completes, deleting each file on success

### Changed

- Engines default to their steering backend: Codex `app-server`, Claude `stream-json`, Pi `rpc`, and OpenCode shared server
- OpenCode steering requires a shared server: `standalone = false`, or `server_url` pointing at an existing server
- Outbound limits are 10 files per run and 50 MiB per file; adapters apply tighter caps of 10 MiB per image / 30 MiB per file on Feishu, 20 MiB per upload on DingTalk, and 10 MiB per image on Telegram
- Images are sent as standalone image messages and other files as file messages; Feishu topic replies use `reply_in_thread=true`
- Steering commands in group chats are limited to `bot.admin_users`

## v0.8.1 — 2026-11-16

### Added

- Session management v2: stable Nectar session ids, names, active-session pointers, per-session usage and message counts, and automatic migration from the version 1 session file
- Chat commands for `/list`, `/switch`, `/current`, `/rename`, `/archive`, and `/unarchive`; `/new [name]` now creates a new session without deleting the previous one
- Per-session FIFO queues, with workspace-level serialization for runs that share files

### Changed

- `/status` now shows both the underlying engine session id and the Nectar session id
- Auto-naming uses the first user message, and archived sessions remain visible through `/list all`
- Runs sharing a workspace are serialized, preventing concurrent agents from editing the same files
- `/stop` cancels all runs in the current chat, including runs that are no longer the active session
- Group-chat session management is limited to `bot.admin_users`; direct chats are unchanged
- Scheduled tasks capture the active session at creation time and fail rather than silently switching if it is archived
- Archived sessions cannot be revived by late engine events, and `/unarchive` restores them
- Numeric `/list` shortcuts only count non-archived sessions; archived entries are prefixed with `archived:`
- Session names strip Markdown/HTML metacharacters, and auto-generated names redact credentials
- Messages waiting behind another run now use a single card that transitions from Queued to Working to Complete
- DingTalk AI card Markdown now preserves single line breaks
- Session list, status, and session detail cards use Markdown tables for alignment
- Chat cards switch to a finalizing Complete state as soon as the answer is ready; usage metadata updates the footer afterward
- Codex `reasoning` items are detected without exposing their content; Working cards show a Thinking-through-the-task placeholder, while tool-only runs show Gathering results

## v0.8.0 — 2026-11-14

### Added

- Local-first memory layer with profile / project / chat scopes, explicit `/remember`, failure-lesson candidates, retrieval injection, and `/memory` commands
- `[memory]` config for enabling memory, per-turn item/character limits, and failure auto-learning
- `nectar memory` CLI and `nectar-memory` agent skill for natural-language memory access
- Cross-process locking and live reload for memory files; soft-delete tombstones; locality, project id, revision, and content hash metadata
- Group-chat writes from non-admins now become candidates; review, pin, and delete require an admin
- Injection is deduplicated by memory hash, with kind quotas, usage tracking, failure-noise filtering, and credential redaction before persistence
- Added the Nectar runtime identity and `/about` / `/whoami` so agents distinguish the product from the underlying engine
- Memory writes now go through the local daemon socket, so sandboxed agents do not need direct write access to `~/.nectar/memory`

## v0.7.0 — 2026-11-14

### Added

- Telegram Bot API platform adapter with long polling, direct chats, groups, forum topics, attachment downloads, streaming message edits, and automatic 4096-character chunking
- `[telegram] bot_token` with optional `proxy` and self-hosted `api_base_url`; the installer supports `--platform telegram`
- The Telegram bot token is removed from agent subprocess environments

### Changed

- README now includes Feishu, DingTalk, and Telegram interface previews

### Fixed

- The `/help` card now shows the engine name currently in use

## v0.6.1 — 2026-11-14

### Added

- `nectar update` now shows percentage, progress bar, and downloaded/total bytes while fetching release assets
- Added `--timeout`, `--attempts`, `--binary`, and `--no-progress` update options

### Changed

- Connection, TLS, response-header, metadata, and asset-download phases now use independent timeouts instead of a 60-second limit covering the complete download
- Transient connection, TLS, and HTTP 5xx failures are retried; the GitHub API is used as a fallback when the release page is unavailable
- Each download attempt defaults to 10 minutes, with up to 2 attempts for metadata and downloads

### Fixed

- Fixed release assets being interrupted by the overall request timeout on slow networks
- A temporary newer updater can now target a specific Nectar installation with `--binary PATH`

## v0.6.0 — 2026-11-14

### Added

- Built-in scheduled tasks with standard five-field cron, fixed intervals, and one-shot runs, manageable from chat, the CLI, and a local Unix socket API
- Persistent schedules with startup catch-up, timezone-aware previews, failure history, and a configurable history limit
- Scheduling controls: global concurrency limits, recurring-run jitter, per-task timeouts, in-flight cancellation, and a `running` state
- The `nectar-schedule` agent skill, installed by the installer, refreshed by `nectar update`, and independently removable
- Complete `nectar uninstall` support, with options to keep or remove config, task data, workspaces, and backups

### Changed

- Scheduled tasks are enabled by default; management commands are limited to `bot.admin_users`, and an empty list leaves administration unrestricted
- Reworked the README into a quick start and split engine, configuration, command, install, service, and scheduling references into dedicated docs

### Fixed

- Scheduled task status now changes only after the agent run actually completes, preventing long runs from being queued repeatedly
- Failed schedule persistence now rolls back in-memory mutations and logs background write errors
- Corrected cron wildcard step semantics for day-of-month/day-of-week combinations and the behavior of `startup_grace = "0s"`
- `schedule run` no longer consumes a one-shot task's original plan

## v0.5.3 — 2026-11-13

### Fixed

- systemd units now use an unquoted `WorkingDirectory`, so the service starts correctly on Linux

## v0.5.2 — 2026-11-13

### Added

- Progress cards now show a live tool-call count; intermediate tool output stays in the card body and hides when the final answer arrives
- `nectar update` self-updater: checks releases, verifies `checksums.txt`, atomically replaces the binary, and restarts the service; supports `--check`, `--force`, `--version`, and `--no-restart`
- Sessions persist across restarts: chat/thread to engine session mappings and last usage are stored in `~/.nectar/sessions.json` and restored on startup; resume failures show a notice and start a new session

### Changed

- User-facing card copy is now consistently in English
- Simplified the card footer and added a divider before it on Feishu and DingTalk

### Fixed

- Context percentage now uses the latest model call's token count across Codex, Claude, and Cursor; `/status` keeps the current turn's token usage

## v0.5.1 — 2026-11-13

### Changed

- Aligned Cursor CLI with the official `agent` primary command: the config default and installer detection now prefer `agent` while remaining compatible with legacy `cursor-agent`
- The installer accepts `--engine agent`, `--engine cursor`, and `--engine cursor-agent`

### Fixed

- Cursor login failures now show an actionable card hint to run `agent login` (or `cursor-agent login`) or set `CURSOR_API_KEY`

## v0.5.0 — 2026-11-13

### Added

- DingTalk platform adapter: receives direct and group @ messages over a Stream-mode long connection, with no public callback required
- DingTalk A2UI streaming cards with Markdown, native tables, and code blocks; falls back to plain Markdown when card permissions are unavailable
- DingTalk inbound image, file, and mixed rich-text messages are downloaded and handed to engines through the platform-neutral attachment model
- DingTalk emoji reactions and message deduplication
- `[dingtalk]` configuration section, `DINGTALK_CLIENT_ID` / `DINGTALK_CLIENT_SECRET` environment variables, and `bot.platform = "dingtalk"`
- Interactive installer: detects installed engine CLIs, lets you choose an engine and platform, writes a minimal config, and installs and starts the background service
- `/status` shows the current engine and model
- English documentation, DingTalk setup guide, and an animated flow diagram

### Changed

- Upgraded to Go 1.26
- Streamlined the documentation structure to usage guides only

### Fixed

- DingTalk tables now render with native A2UI table components instead of broken Markdown tables

## v0.4.1 — 2026-11-12

### Changed

- Card status emoji now use natural metaphors: `🌼 Thinking`, `🌱 New Session`, `🌿 Session Status`, `✋ Stop`
- `🐝 Working`, `🍯 Complete`, `⚠️ Failed`, and error messages are unchanged

## v0.4.0 — 2026-11-12

### Added

- Pi engine adapter: `--mode json`, `--session` continuation, tool events, and token usage mapping
- `[pi]` configuration section, `PI_PATH` environment variable, and `bot.engine = "pi"`
- Pi `provider`, `model`, `thinking`, and `trust_workspace` (`--approve`)
- Claude Code engine adapter: `-p --output-format stream-json`, `--resume` continuation, tool events, and token usage mapping
- `[claude]` configuration section, `CLAUDE_PATH` environment variable, and `bot.engine = "claude"`
- Claude Code `auto_approve` (`--dangerously-skip-permissions`), `permission_mode`, `fallback_model`, and `max_turns`
- Engines can return a runtime `context_window` in completion events, so the card's context usage adapts automatically
- Cursor Agent CLI engine adapter: `-p --output-format stream-json`, `--resume` continuation, tool events, and token usage mapping
- `[cursor]` configuration section, `CURSOR_AGENT_PATH` environment variable, and `bot.engine = "cursor"`
- Cursor `auto_approve` (`-f`) and `trust_workspace` (`--trust`) non-interactive switches

## v0.3.0 — 2026-11-12

### Added

- Feishu inbound rich text: `post` messages mixing text and images, preserving text/image order
- Feishu image-only and file messages are downloaded and handed to Codex/OpenCode
- Codex receives image attachments through `--image`; OpenCode through `--file`
- Platform-neutral inbound attachment model and `EngineOptions.Attachments` extension point

### Fixed

- Flat-structure Feishu `post` messages no longer degrade to `[rich text message]`; language-wrapped structures are also supported
- Markdown images and `img` elements in rich text are both extracted as real attachments
- Top-level `files` fields in rich text are extracted as file attachments, covering "text + .txt/.log file" messages
- Text file contents are injected directly into the prompt so the agent does not reuse historical context and ignore current attachments

## v0.2.0 — 2026-11-12

### Added

- OpenCode engine adapter: `run --format json`, `--session` continuation, and tool event mapping
- `bot.engine` engine selection and a `[opencode]` configuration section
- OpenCode footer shows the real model, last-turn token usage, and context usage
- OpenCode parses the context limit from the model's `limit.context`; `opencode.context_window` can override it
- OpenCode tool failures are treated as recoverable errors; the card still shows Complete when a final answer is available
- Feishu cards migrated to Card 2.0: titles, quotes, inline code, code blocks, and lists use native markdown rendering
- Feishu cards render Markdown tables with native table elements (up to 5 per card; excess tables fall back to code blocks)
- Feishu cards convert Markdown images to links/text to avoid card PATCH failures caused by external images
- Reference-style images (`![alt][ref]`) are also converted to links/text

## v0.1.3 — 2026-11-12

### Fixed

- Feishu topic messages are now answered inside the same topic instead of sending cards to the main chat
- Session keys include `ThreadID`, so different topics in the same group use separate Codex sessions

## v0.1.2 — 2026-11-12

### Added

- `/help`, `/commands`, and short questions such as "what commands are there" return the platform command card directly without invoking Codex
- `/new`, `/status`, `/stop`, empty messages, and command errors also use platform cards consistently, with text as fallback
- Codex intermediate messages update the Working card in real time; the final card shows only the last result
- Feishu card bodies use the `markdown` component, fixing backtick and code block rendering
- New `CardInfo` card state; Feishu uses a wathet header

## v0.1.1 — 2026-11-12

### Added

- `nectar service install|uninstall|start|stop|restart|status|logs`
- macOS LaunchAgent support
- Linux systemd user service support, with optional `--linger`
- `install.sh --service` / `--linger` / `--no-start`

## v0.1.0 — 2026-11-12

First usable version (v1).

### Added

- Feishu WebSocket long connection and message normalization
- Codex `exec --json` / `resume` adapter
- Codex JSONL event parsing
- Triple session keys and a per-session FIFO serial queue
- Feishu interactive cards: Thinking / Working / Complete / Failed
- Static `🐝` working state, `🍯 Complete`, `⚠️ Failed`
- Context usage, model, elapsed time, and tokens on the completion card
- `/new`, `/status`, `/stop`, `/help`
- Failure lessons recorded to `~/.nectar/comb/lessons.json` (pending, not injected)
- Working directory, timeout, and output limits
- One-line `install.sh` installer
- GitHub Actions Release pipeline
- Prebuilt binaries for four platforms with SHA-256 verification

### Fixed

- Feishu `channel.OnMessage` silently dropped messages because of a missing `EventDispatcher`
- Custom cancellation for the Codex subprocess now uses `exec.CommandContext`
- The per-session queue is strictly FIFO to prevent out-of-order messages

### Known limitations

- Sessions are not persisted
- Inbound media is not forwarded
- No token-by-token streaming output
- Lessons are recorded but not injected

See [roadmap.md](roadmap.md) for details.
