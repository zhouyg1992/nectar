# Roadmap

## v0.10.6 complete

- Memory system upgrade: local BM25 retrieval, match explanations, good/bad feedback ranking, and candidate extraction from ordinary conversations

## v0.10.5 complete

- DingTalk card layout improvements: plain paragraphs fill the card width, and small tables (up to 8 columns and 50 rows) render as Markdown, fixing the gray header and width mismatch

## v0.10.4 complete

- Completion reactions on final messages (`DONE` on Feishu, 👍 on Telegram), plus a fix for duplicated `v` prefixes in `/about` and the agent identity prompt

## v0.10.3 complete

- `nectar update` supports GitHub mirror prefixes and HTTP Range resume, making updates more reliable on weak networks or when GitHub is unreachable

## v0.10.2 complete

- Dashboard memory review with approve/reject/restore, bulk actions, status and tag filters, scope editing, and 50-item pagination for memories and scheduled tasks

## v0.10.1 complete

- Codex app-server `max_output_bytes` now applies per event, so long tasks are no longer rejected for cumulative output

## v0.10.0 complete

- Multiple bots with independent platform identities, engines, workspaces, sessions, memory, and permissions, plus per-bot service logs, schedules, and memory
- Per-session git worktrees and `nectar bots` / `nectar worktrees list|remove` cleanup commands
- Local dashboard with bot status, memory review, scheduled task management, and read-only config, disabled by default and bound to loopback

## v0.9.0 complete

- Steering: `/steer`, `/append`, `/ps`, and `/btw` append context to a running task, while ordinary messages still queue
- Steering backends for Codex app-server, Claude stream-json, Pi rpc, and OpenCode shared servers, enabled by default
- Outbound media: agents write images and files to `.nectar/outbox/`, and Nectar delivers them to Feishu, DingTalk, and Telegram after the run

## v0.8.1 complete

- Session management v2: stable Nectar session ids, names, active-session pointers, per-session usage and message counts, and automatic migration from version 1 session files
- New `/list`, `/switch`, `/current`, `/rename`, `/archive`, and `/unarchive` chat commands; `/new [name]` keeps previous sessions
- Per-session FIFO queues, with workspace-level serialization for runs that share files
- Queued messages reuse one card that updates in place from Queued to Working to Complete
- DingTalk cards preserve single line breaks, session cards use Markdown tables, and cards finalize in two stages before usage metadata arrives

## v0.8.0 complete

- Local-first memory layer with profile / project / chat scopes, explicit `/remember`, and `/memory` management
- Reviewable failure-lesson candidates, hash-deduplicated retrieval injection, kind quotas, and credential redaction before persistence
- `nectar memory` CLI, the `nectar-memory` agent skill, and sandboxed memory writes through the daemon socket
- Nectar runtime identity injection with `/about` / `/whoami`

## v0.7.0 complete

- Telegram Bot API platform adapter: long polling, direct and group chats, forum topics, attachment downloads, streaming message edits, and automatic 4096-character chunking
- `[telegram]` configuration with an optional proxy and self-hosted API base URL; the installer supports `--platform telegram`
- Telegram bot tokens are no longer exposed to agent subprocesses

## v0.6.0 complete

- Scheduled tasks: cron, fixed intervals, one-shot runs, persistence, startup catch-up, and execution previews
- Scheduling controls: concurrency limits, jitter, timeouts, cancellation, history retention, and running state
- Installation, upgrade, and removal of the `nectar-schedule` agent skill
- Complete `nectar uninstall` flow
- Reworked README and install, configuration, command, engine, and service documentation

## v0.5.0 complete

- DingTalk Stream-mode long connection, A2UI streaming cards, and image/file attachments
- Interactive installer: detects engines, writes config, and installs the background service
- `/status` shows the current engine and model

## v0.4.0 complete

- Cursor Agent CLI engine support
- Claude Code engine support
- Pi engine support
- Engines can return a runtime `context_window` in completion events, so the card's context usage adapts automatically

## v0.3.0 complete

- Feishu inbound images, files, and mixed text/image messages
- Native image attachment delivery for Codex/OpenCode

## v1 complete

- Feishu WebSocket long connection
- Direct chats and group chats with @Bot
- Codex `exec --json` / `resume`
- OpenCode `run --format json` / `--session`
- Cursor Agent `-p --output-format stream-json` / `--resume`
- Claude Code `-p --output-format stream-json` / `--resume`
- Pi `--mode json` / `--session`
- JSONL event parsing
- Session keys and per-session FIFO queue
- Interactive cards: Thinking / Working / Complete / Failed
- Context usage, model, elapsed time, and tokens on the completion card
- `/new`, `/status`, `/stop`, `/help`
- Failure lesson recording (pending, not injected)
- One-line installer and GitHub Release pipeline

## Explicitly out of scope for v1

- Token-by-token streaming output
- Lesson injection and review cards
- Cloud sync
- Platforms beyond Feishu / DingTalk / Telegram, and engines beyond Codex/OpenCode/Cursor/Claude Code/Pi

## Later

### v1.1

- Lesson log search
- Finer-grained streaming output

### v1.2

- Feishu review cards
- Inject lessons only after they move from `pending → active`

### v1.3

- Pollen cloud sync: Comb, `AGENTS.md`, skills
- Multi-device conflict handling

### Farther out

- WeChat, Slack
- Kimi
- Jev decision service
- Agent Teams / Multi-Bot Relay

For historical changes, see [changelog.md](changelog.md).
