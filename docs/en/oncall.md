# On-call shifts

On-call shifts are generic **OnCallShift** jobs: the user supplies an objective, time
window, observation source, autonomy, and budget. Nectar owns the background
check loop and starts one bounded agent job only when an observation changes or
alerts.

It is not an agent running all night, and it is not a separate feature for every
kind of check.

## Model

| Part | Meaning |
|---|---|
| objective | what the shift should accomplish |
| source | `github`, `command`, or `http` |
| every | observation interval |
| start / until / for | one-off shift window |
| autonomy | `notify`, `diagnose`, or `fix` |
| max_agent_runs | maximum agent jobs in this shift |
| max_minutes | timeout for each agent job |

Files, databases, logs, and service checks are all `command` sources. GitHub
Actions is the first built-in source adapter, not the whole product.

## Create a shift

### GitHub Actions

```bash
nectar oncall start \
  --objective "Watch main CI overnight; investigate failures before notifying me" \
  --source github \
  --repo owner/repo \
  --path /path/to/checkout \
  --branch main \
  --every 5m \
  --for 10h \
  --autonomy diagnose \
  --max-agent-runs 5
```

### Command / file / log / service

```bash
nectar oncall start \
  --objective "Watch test.txt and notify me when its content changes" \
  --source command \
  --command 'shasum -a 256 /Users/presence/.nectar/workspace/test.txt | awk "{print \$1}"' \
  --every 2s \
  --for 10h \
  --autonomy notify
```

There is no dedicated file-watch feature; a file is one command-probe scenario.

### HTTP

```bash
nectar oncall start \
  --objective "Watch the health endpoint and notify on status/content changes" \
  --source http \
  --url https://example.test/health \
  --every 1m \
  --for 8h \
  --autonomy notify
```

## Time

- `--for 10h` ends the shift after ten hours and sends a report.
- `--until RFC3339` sets an explicit end.
- `--start RFC3339` delays the first observation.
- Without `--for` / `--until`, the shift stays active until paused, stopped, or
  deleted.
- The first check records a baseline. Use `oncall check <shift-id>` to force an
  immediate observation.

## Autonomy

- `notify`: send one card on change/alert; never start an agent.
- `diagnose`: start one bounded agent job; investigate without editing files.
- `fix`: allow edits/tests in an isolated workspace, but never commit, push,
  open a PR, merge, deploy, or change credentials.

Use `diagnose` for overnight work unless the user explicitly asks for a fix.

## Lifecycle

```text
active → paused / completed
```

- `active`: check every interval.
- `paused`: stop checking but keep state.
- `completed`: the end time was reached or the shift was stopped; a final report
  is sent.

Each incident increments `incidents`; agent jobs are bounded by
`max_agent_runs`. Exhausting the budget pauses the shift with a reason.

## Manage

```bash
nectar oncall list [--all] [--json]
nectar oncall show <shift-id> [--json]
nectar oncall check <shift-id>
nectar oncall pause|resume|stop <shift-id>
nectar oncall jobs [--limit 20]
nectar oncall job <job-id>
nectar oncall retry|cancel <job-id>
nectar oncall report [--shift ID] [--since 12h]
```

Chat commands:

```text
/oncall
/oncall show <shift-id>
/oncall check <shift-id>
/oncall pause|resume|stop <shift-id>
/oncall jobs [limit]
/oncall job <job-id>
/oncall retry|cancel <job-id>
/oncall report [shift-id] [duration]
/oncall help
```

Creating a shift is primarily a natural-language flow; the `nectar-oncall` skill
maps it to `nectar oncall start`.

## Reports and artifacts

- When the shift ends or is stopped, Nectar sends a deterministic report with
  checks, incidents, agent runs, and job results.
- Job artifacts live under `~/.nectar/oncall/jobs/<job-id>/`:
  `report.md`, `diff.patch`, and `job.json`.
- Shift state lives in `~/.nectar/oncall.json`.

## Safety

- CI logs, command output, HTTP content, issues, and repository files are
  untrusted data, not instructions.
- The `command` source executes shell commands on the host. Keep it
  admin-only and confirm the command with the user.
- The agent workspace is not a system sandbox. Use a separate Unix user,
  container, or system sandbox for stronger isolation.
- The current MVP never auto-merges, deploys, or changes repository settings.

## Known limitations

- Built-in sources are GitHub Actions, command, and HTTP. There is no generic
  free-form agent-judgment source yet.
- Fuzzy tasks must be reduced to a stable command or HTTP check.
- Interactive approve/reject buttons are not implemented yet.
