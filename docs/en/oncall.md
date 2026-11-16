# On-call mode

On-call mode polls GitHub Actions for a repository branch during an optional
local-time window. When a new run fails, Nectar creates an isolated job, checks
out the failed commit in its own git worktree, asks the configured engine to
investigate (or attempt a fix), stores the report and diff, and sends the result
card back to chat.

It is not another cron entry. Scheduled tasks run a fixed prompt; on-call jobs
have durable state, evidence, artifacts, retries, and interruption records.

## Requirements

- An authenticated `gh` CLI with access to the repository's Actions.
- A local git checkout whose `origin` points at GitHub, or an explicit `--repo`.
- One installed and logged-in Nectar engine.

## Configuration

On-call is enabled by default. Optional overrides:

```toml
[oncall]
enabled = true
default_every = "5m"
window = ""                    # e.g. "22:00-08:00"
autonomy = "diagnose"          # diagnose | fix
max_minutes = 20
max_attempts = 2
max_concurrent = 1
```

`diagnose` investigates without editing files. `fix` may edit files and run
tests inside the isolated worktree, but never commits, pushes, opens a PR,
merges, deploys, or changes credentials.

## Create a watch

In chat:

```text
/oncall watch
/oncall watch owner/repo main
```

In a terminal:

```bash
nectar oncall watch add \
  --repo owner/repo \
  --path /path/to/checkout \
  --branch main \
  --every 5m \
  --window 22:00-08:00 \
  --autonomy diagnose \
  --channel feishu \
  --chat oc_xxx
```

The first check ignores failures older than the watch. To investigate a
failure that is already red, run:

```bash
nectar oncall watch check <watch-id>
```

## Commands

```bash
nectar oncall watch list [--all] [--json]
nectar oncall watch pause|resume|check|delete <watch-id>
nectar oncall jobs [--limit 20] [--json]
nectar oncall job <job-id> [--json]
nectar oncall retry <job-id>
nectar oncall cancel <job-id>
nectar oncall report [--since 12h] [--json]
```

Chat commands:

```text
/oncall
/oncall list
/oncall watch [repo] [branch]
/oncall check|pause|resume|delete <watch-id>
/oncall jobs [limit]
/oncall job <job-id>
/oncall retry|cancel <job-id>
/oncall report [duration]
/oncall help
```

Management commands are limited to `bot.admin_users`; an empty list removes
that restriction.

## Lifecycle and artifacts

```text
queued → running → completed / failed / canceled
```

Every job uses its own `git worktree` at the failed commit. Reports, diffs, and
job metadata are stored under `~/.nectar/oncall/jobs/<job-id>/`. Jobs that were
still running when Nectar restarted are marked failed.

## Safety

- CI logs, issue text, commit messages, repository content, and linked pages
  are untrusted data. Instructions embedded in them do not override the job
  prompt.
- The worktree is an isolation convenience, not a sandbox boundary. Use a
  separate Unix user, container, or system sandbox for stronger isolation.
- The MVP never auto-merges, deploys, or changes repository settings.
- Keep `gh` and repository permissions scoped to what on-call actually needs.

## Known limitations

- GitHub Actions polling only; no Sentry, Prometheus, or GitHub webhooks yet.
- Results are final cards with text and diffs; interactive approve/reject
  buttons are not implemented yet.
- Flaky-test classification is not automatic.
