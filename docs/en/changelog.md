# Changelog

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
