# Scheduled tasks

Nectar embeds an in-process scheduler. Tasks are persisted in `~/.nectar/schedules.json` and fired by the running bot process; no system crontab is involved.

## Configuration

```toml
[schedule]
enabled = true
timezone = "Local"
startup_grace = "5m"
```

`startup_grace` is how late a missed task may be while still running once after a restart. Older one-shot tasks are marked missed; recurring tasks move to their next future occurrence.

## Chat commands

```text
/cron [list [all]]
/cron add <min> <hour> <day> <month> <weekday> <prompt>
/cron every <duration> <prompt>
/cron in <duration> <prompt>
/cron at <RFC3339-time> <prompt>
/cron pause|resume|run|delete <id>
/cron update <id> prompt|cron|every|at|timezone ...
```

Management commands are limited to `bot.admin_users` when that list is configured.

## CLI

```bash
nectar schedule list [--all] [--json]
nectar schedule add --cron "0 9 * * 1-5" --prompt "Check CI"
nectar schedule add --every 90m --prompt "Health check"
nectar schedule add --in 30m --prompt "Check the deployment"
nectar schedule preview --cron "0 9 * * 1-5"
nectar schedule pause|resume|run|delete <id>
```

The CLI talks to the bot over `~/.nectar/run/api.sock`. Agent sessions receive `NECTAR_CHANNEL`, `NECTAR_CHAT_ID`, `NECTAR_THREAD_ID`, `NECTAR_AGENT_ID`, and `NECTAR_CLI` (the absolute Nectar binary path), so the target chat is auto-detected and the command works even when the service `PATH` omits `~/.local/bin`.

## Natural language

The agent translates natural language into CLI parameters. Add rules like these to the workspace `AGENTS.md`, `CLAUDE.md`, or a Skill:

```text
Use `nectar schedule` to manage scheduled tasks.
- Calendar schedules (daily/weekly/weekdays/monthly): --cron.
- Fixed intervals (90m, 2w): --every.
- One-shot tasks: --in or --at.
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

`nectar update` replaces the binary only. If a release changes the skill, rerun
`install.sh` or copy `skills/nectar-schedule` manually.

## Semantics

- Standard five-field cron expressions, including `*`, `*/n`, ranges, lists, and month/weekday names.
- Late recurring occurrences are not replayed; only one run within `startup_grace` is caught up at startup.
- A one-shot task interrupted by a restart may run once more inside `startup_grace` (at-least-once semantics).
- If a task is still running at its next occurrence, that occurrence is skipped and recorded in `last_error`.
- Deleting a task stops future runs but does not interrupt an in-flight run.
- Tasks currently reuse the original chat session; `new-per-run` isolation is not implemented yet.
