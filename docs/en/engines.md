# Engine Setup and Login Checks

Nectar does not require every engine. Install, log in to, and verify one coding-agent CLI in non-interactive mode.

The installer detects installed engines. If none are found, it asks you to install one first.

## Choose an engine

| Engine | Nectar section | Best for |
|---|---|---|
| Codex CLI | `[codex]` | The default; model catalog and context auto-detection |
| Claude Code | `[claude]` | Existing Claude Code users or fallback models |
| Cursor Agent CLI | `[cursor]` | Existing Cursor subscriptions or Agent CLI |
| OpenCode | `[opencode]` | OpenCode's provider / model setup |
| Pi | `[pi]` | Pi provider and thinking configuration |

To switch engines, edit `~/.nectar/config.toml`:

```toml
[bot]
engine = "codex"   # codex | opencode | cursor | claude | pi
```

Then restart the service:

```bash
nectar service restart
```

## Codex CLI

Install Codex CLI and log in, then verify:

```bash
codex exec --json --skip-git-repo-check "reply with OK"
```

Nectar runs `codex exec --json` by default and resumes sessions with `resume`. Model, reasoning effort, sandbox, and workspace are configured in `[codex]`.

To steer a running turn (`/steer`, `/append`, `/ps`, `/btw`), switch Codex to the app-server backend:

```toml
[codex]
backend = "app-server"
```

The app-server backend uses Codex's `turn/start`, `turn/steer`, and `turn/interrupt` APIs. The protocol is still experimental; pin a known-good Codex version. The default `exec` backend does not support steering; the commands return an explicit unsupported message.

If the service environment cannot find `codex`, set `[codex].path` to an absolute path.

## Claude Code

Install and log in to Claude Code:

```bash
claude auth login
claude -p --permission-mode plan "Reply with exactly OK"
```

Nectar runs `claude -p --output-format stream-json --verbose --include-partial-messages` by default, passes the prompt on stdin, and resumes sessions with `--resume`.

To enable steering, switch Claude to streaming-input mode:

```toml
[claude]
backend = "stream-json"
```

That mode uses `--input-format stream-json` to keep the process alive; `/steer` appends a user message to the same process. The default `print` backend does not support steering.

`auto_approve = true` maps to `--dangerously-skip-permissions` for unattended bot use. When disabled, Nectar passes `permission_mode` instead.

## Cursor Agent CLI

Install Cursor Agent:

```bash
curl https://cursor.com/install -fsS | bash
```

The official installer provides both `agent` (primary) and `cursor-agent` (legacy alias). Nectar and the installer prefer `agent` and fall back to `cursor-agent`, so no symlink is needed.

Make sure `~/.local/bin` is in `PATH`, then verify:

```bash
agent login
agent -p --trust --mode ask "Reply with exactly OK"
```

Nectar runs `-p --output-format stream-json --stream-partial-output` and resumes with `--resume`. `auto_approve` maps to `-f`, and `trust_workspace` maps to `--trust`; both are required for headless runs.

## OpenCode

Install OpenCode, configure a provider / model, and verify:

```bash
opencode run --format json "Reply with exactly OK"
```

Nectar runs `opencode run --format json` and resumes with `--session`. When `model` is empty, Nectar tries to resolve the default model from `~/.config/opencode/opencode.json`.

OpenCode steering uses the shared server API's `delivery = "steer"` path. The default `standalone = true` starts a private server for each run, so steering is unavailable; set `standalone = false` or provide `server_url` for an existing OpenCode server.

## Pi

Install and log in to Pi, then verify:

```bash
pi auth check --provider anthropic
pi --mode json "Reply with exactly OK"
```

Nectar runs `pi --mode json` by default, passes the prompt on stdin, and resumes with `--session`. `provider`, `model`, and `thinking` are passed through to Pi.

To enable steering, switch Pi to RPC mode:

```toml
[pi]
backend = "rpc"
```

The RPC backend uses `pi --mode rpc`; `/steer` sends Pi's native `steer` command. The default `json` backend does not support steering.

## Troubleshooting

- **`command not found`**: make sure the engine CLI is on `PATH`; in service mode, prefer an absolute path in the config.
- **Expired login**: log in again in a terminal, then restart the Nectar service.
- **Stuck on Working**: run the verification command manually to rule out approvals, model, or network issues.
- **Permission errors**: Cursor needs `--trust`, Claude needs `auto_approve` or a suitable `permission_mode`, and Pi needs `trust_workspace`.

Back to: [English README](../../README.md) · [Configuration](config.md) · [Commands](commands.md)
