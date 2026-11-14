# Scheduled tasks

Nectar embeds an in-process scheduler. Tasks are persisted in `~/.nectar/schedules.json` and fired by the running bot process; no system crontab is involved.

## Configuration

Every setting below is optional. Nectar enables the scheduler by default (`enabled = true`), uses the host timezone, and applies the defaults shown here.

```toml
[schedule]
enabled = true
timezone = "Local"
startup_grace = "5m"
history_limit = 100
max_concurrent = 4      # simultaneous runs; 0 = unlimited
jitter = "0s"           # random delay cap for due recurring runs
timeout = "30m"         # default per-run timeout; 0s disables the scheduler deadline
```

`startup_grace` is how late a missed task may be while still running once after a restart. Use `0s` to disable catch-up. Older one-shot tasks are marked missed; recurring tasks move to their next future occurrence. `history_limit` keeps only the newest completed/failed tasks; the default is 100 and `0` means unlimited.

`max_concurrent` caps simultaneous scheduled runs; extra due tasks wait for a slot. `jitter` adds a random `[0, jitter)` delay to recurring runs so many tasks do not start in the same instant; one-shot tasks and manual `run` are not delayed. `timeout` is the default per-run deadline; a task can override it with `--timeout`, and `0s` disables the scheduler deadline while the engine's own timeout still applies. The deadline starts after the task gets an execution slot and includes any wait behind earlier turns in the same chat session.

## Chat commands

```text
/cron [list [all]]
/cron add <min> <hour> <day> <month> <weekday> <prompt>
/cron every <duration> <prompt>
/cron in <duration> <prompt>
/cron at <RFC3339-time> <prompt>
/cron pause|resume|run|cancel|delete <id>
/cron update <id> prompt|cron|every|at|timezone|timeout ...
```

Management commands are limited to `bot.admin_users` when that list is configured.

## CLI

```bash
nectar schedule list [--all] [--json]
nectar schedule add --cron "0 9 * * 1-5" --prompt "Check CI"
nectar schedule add --every 90m --prompt "Health check"
nectar schedule add --in 30m --prompt "Check the deployment"
nectar schedule add --every 5m --timeout 2m --prompt "Health check"
nectar schedule preview --cron "0 9 * * 1-5"
nectar schedule pause|resume|run|cancel|delete <id>
nectar schedule update <id> --timeout 15m --cron "0 10 * * 1-5"
```

The CLI talks to the bot over `~/.nectar/run/api.sock`. Agent sessions receive `NECTAR_CHANNEL`, `NECTAR_CHAT_ID`, `NECTAR_THREAD_ID`, `NECTAR_AGENT_ID`, and `NECTAR_CLI` (the absolute Nectar binary path), so the target chat is auto-detected and the command works even when the service `PATH` omits `~/.local/bin`.

## Natural language

The agent translates natural language into CLI parameters. Add rules like these to the workspace `AGENTS.md`, `CLAUDE.md`, or a Skill:

```text
Use `nectar schedule` to manage scheduled tasks.
- Calendar schedules (daily/weekly/weekdays/monthly): --cron.
- Fixed intervals (90m, 2w): --every.
- One-shot tasks: --in or --at.
- Add --timeout when a run needs a deadline.
- Always run `nectar schedule preview` and show next_runs to the user first.
- Rewrite the prompt so it is self-contained.
- Ask when the time, weekday, or timezone is ambiguous.
```

`install.sh` handles this by default:

- it installs the `nectar-schedule` skill into `~/.nectar/skills`,
  `~/.codex/skills`, `~/.claude/skills`, and `~/.agents/skills`;
- it adds a managed `<!-- BEGIN NECTAR SCHEDULE -->` block to `AGENTS.md` in the
  Nectar workspace and any configured `working_dir`, covering engines that
  primarily read `AGENTS.md` (OpenCode, Pi, Cursor);
- upgrades back up the old skill to `~/.nectar/skill-backups` and replace the
  managed block in place.

Pass `--no-skills` to leave global agent configuration untouched.

`nectar update` runs `nectar skills install` from the newly installed binary,
so the global skill and workspace `AGENTS.md` block stay in sync. You can also
run `nectar skills install` manually.

To remove only the skill and workspace hints:

```bash
nectar skills uninstall
```

This removes `nectar-schedule` from the Nectar, Codex, Claude, and shared Agents
skill roots without deleting config, task data, or `skill-backups`.

## Semantics

- Standard five-field cron expressions, including `*`, `*/n`, ranges, lists, and month/weekday names.
- Late recurring occurrences are not replayed; only one run within `startup_grace` is caught up at startup.
- A one-shot task interrupted by a restart may run once more inside `startup_grace` (at-least-once semantics).
- If a task is still running at its next occurrence, that occurrence is skipped and recorded in `last_error`.
- A run that exceeds its timeout cancels the agent process and records `run timed out after ...`; recurring tasks stay active.
- `nectar schedule cancel <id>` (or `/cron cancel <id>`) cancels the current run without deleting the task; recurring tasks continue on schedule.
- `schedule list` reports `running` while a task is executing or waiting for an execution slot.
- When `max_concurrent` is reached, due tasks wait for a slot; if a task waits longer than its interval, the next occurrence is skipped as “previous run still in progress”.
- Deleting a task stops future runs but does not interrupt an in-flight run.
- `nectar schedule run <id>` executes once without consuming a one-shot task's original plan.
- Tasks currently reuse the original chat session; `new-per-run` isolation is not implemented yet.
