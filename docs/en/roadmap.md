# Roadmap

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
- Session persistence
- Lesson injection and review cards
- Cloud sync
- Platforms beyond Feishu / DingTalk, and engines beyond Codex/OpenCode/Cursor/Claude Code/Pi

## Later

### v1.1

- Scheduled tasks (implemented, see [schedule.md](schedule.md))
- Lesson log search
- Finer-grained streaming output

### v1.2

- Feishu review cards
- Inject lessons only after they move from `pending → active`

### v1.3

- Pollen cloud sync: Comb, `AGENTS.md`, skills
- Multi-device conflict handling

### Farther out

- Telegram, WeChat, Slack
- Kimi
- Jev decision service
- Agent Teams / Multi-Bot Relay

For historical changes, see [changelog.md](changelog.md).
