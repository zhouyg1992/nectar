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

Nectar runs `codex exec --json` and resumes sessions with `resume`. Model, reasoning effort, sandbox, and workspace are configured in `[codex]`.

If the service environment cannot find `codex`, set `[codex].path` to an absolute path.

## Claude Code

Install and log in to Claude Code:

```bash
claude auth login
claude -p --permission-mode plan "Reply with exactly OK"
```

Nectar runs `claude -p --output-format stream-json --verbose --include-partial-messages`, passes the prompt on stdin, and resumes sessions with `--resume`.

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

## Pi

Install and log in to Pi, then verify:

```bash
pi auth check --provider anthropic
pi --mode json "Reply with exactly OK"
```

Nectar runs `pi --mode json`, passes the prompt on stdin, and resumes with `--session`. `provider`, `model`, and `thinking` are passed through to Pi.

## Troubleshooting

- **`command not found`**: make sure the engine CLI is on `PATH`; in service mode, prefer an absolute path in the config.
- **Expired login**: log in again in a terminal, then restart the Nectar service.
- **Stuck on Working**: run the verification command manually to rule out approvals, model, or network issues.
- **Permission errors**: Cursor needs `--trust`, Claude needs `auto_approve` or a suitable `permission_mode`, and Pi needs `trust_workspace`.

Back to: [English README](../../README.md) · [Configuration](config.md) · [Commands](commands.md)
