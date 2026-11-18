# Multi-bot collaboration (relay)

One Nectar process can run several bots. With relay enabled, bots in the same chat can **address** one another for help: a coder hands work to a reviewer, then continues from the reviewer's answer in its own session.

Relay is **disabled by default**. It only carries explicit hand-offs; bots never react to each other's chat messages.

## Quick start

Two bots share one workspace and relay is on:

```toml
[relay]
enabled = true

[bots.coder]
name = "Codex Coder"
platform = "feishu"
engine = "codex"
workspace = "~/code/project"
workspace_mode = "shared"
role = "coder"
description = "Implement features, edit code, run tests"
capabilities = ["code", "git", "test"]

[bots.coder.feishu]
app_id = "cli_xxx"
app_secret = "xxx"

[bots.reviewer]
name = "Claude Reviewer"
platform = "feishu"
engine = "claude"
workspace = "~/code/project"
workspace_mode = "shared"
role = "reviewer"
description = "Read-only diff review, risk findings"
capabilities = ["review", "security"]
relay_accept_from = ["coder"]

[bots.reviewer.feishu]
app_id = "cli_yyy"
app_secret = "yyy"
```

In the group, @coder: "implement this, have reviewer check it, then summarize". **Mention only the initiator**: write the target's name as plain text instead of picking it from the @ menu. A Feishu @ is message routing, so mentioning both bots delivers the message to both; Nectar handles it with only the **first mentioned bot** by default (`first_mention_only`), but mentioning only the initiator remains the clearer habit.

The coder runs:

```sh
nectar relay send --to reviewer "…self-contained brief and diff…"
```

and ends its turn. When the reviewer finishes, its final answer arrives in the coder's **original session as a new turn**, and the coder summarizes for the user.

The target only needs to be in the same chat; you do **not** have to @-mention it first. Relay delivers by the configured roster. Nectar cannot reliably know whether a bot is in the group, so it does not pre-check; if the target really is elsewhere, posting its output fails and shows up in the log.

Use `workspace_mode = "shared"` on both sides for `coder -> reviewer -> coder`: the reviewer then sees the coder's uncommitted changes. With `auto` / `worktree`, each session has its own worktree and the target cannot see uncommitted work, so the relay body must carry the diff or draft.

## How it works

```text
user → bot A → engine A ──relay send (local API + run token)
                               │
                           RelayHub (roster / tokens / budgets / journal)
                               │ enqueue
                               ▼
                         bot B's relay session → engine B
                               │ final answer
                               ▼
                  RelayHub → bot A's origin session (new turn)
```

- **The bus is the trigger; chat is a mirror.** Hand-offs go through the local relay bus, never through "one bot @-mentions another in the group". The group only gets a human-visible hand-off card; when an ask finishes, that card is updated in place with the result instead of a second target message (`echo` controls visibility). None of these cards can trigger anything.
- **Replies return to the origin session.** The target's answer resumes the sending bot's original session, keeping its context, workspace and uncommitted changes.
- **The target runs in its own session.** Each root maps to a detached session on the target (`relay:<root>`), so the human's active session is never hijacked. It shows up in `/list` and can be taken over with `/switch`.
- **Async rule.** After `relay send`, the sender must end its turn: no polling, sleeping or waiting. The reply wakes the origin session as a new turn; a turn that never ends would queue the reply behind itself and deadlock the session. Reply turns do **not** receive a relay token: the origin turn holds the workspace lock, so a nested ask from it would wait on a target waiting for the same lock. End the turn; further hops start from the target side. As a backstop, if the sender turn is still running after `sender_grace` (default 60s) while the target waits for the lock, Nectar cancels the sender turn, the lock is released, the target continues, and its result still arrives as a new turn in the origin session.
- **One target per message.** No broadcast, and no infinite ping-pong: replies never trigger another automatic reply.
- **Group chats only by default.** In a direct chat the target bot is not a participant, so its output could never be shown; relay refuses it with `ERR_DIRECT_CHAT`. Set `allow_direct = true` for private orchestration: target output is forced silent, the mirror is skipped, and only the final answer returns to the sender.
- **First mention wins.** When the platform reports who was mentioned (Feishu today), a group message that mentions several Nectar bots is handled only by the first one, so one human instruction is not duplicated across bots that end up reviewing each other.
- **Same-message mutual relays are refused.** When one human message reached two bots and an A→B ask is already in flight, a B→A ask returns `ERR_MUTUAL_RELAY`, so the bots cannot duplicate work and cross-review. Relays started from different human messages are independent and allowed.

## Configuration

### `[relay]`

| Field | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | Turn relay on. Only useful with at least two bots. |
| `allow_direct` | `false` | Allow starting a relay from a direct chat. Refused by default (`ERR_DIRECT_CHAT`) because the target bot is not in that DM and its output has nowhere to go. When enabled, target output is forced silent, the mirror is skipped, and only the final answer returns to the sender. |
| `first_mention_only` | `true` | When a group message mentions several configured bots, only the first mentioned **configured Nectar bot** acts; the others wait for a relay. Set `false` to restore "every mentioned bot responds". |
| `echo` | `"summary"` | Visibility of target output: `full` shows the target's progress and final cards and closes the hand-off card with a completion note; `summary` updates the hand-off card **in place** with the final answer instead of posting a second message; `off` sends no hand-off card and hides target output entirely (the answer still returns to the sender). The sender's own turn is always visible. |
| `max_generation` | `4` | Maximum hops in one chain; beyond it returns `ERR_GEN_LIMIT`. |
| `max_envelopes_per_root` | `12` | Maximum envelopes per root, replies included. |
| `max_runs_per_root` | `8` | Maximum engine runs per root, including the origin reply turn. |
| `root_ttl` | `"30m"` | Root lifetime; expired roots accept no new relays. |
| `sender_grace` | `"60s"` | If the turn that sent the relay is still running after this long while the target waits for the shared workspace lock, Nectar cancels the sender turn to release the lock and let the relay continue. |
| `session` | `"per_root"` | Target session model; only `per_root` is supported. |

### Bot fields

| Field | Meaning |
| --- | --- |
| `role` | Role name such as `coder` or `reviewer`. |
| `description` | One-line description shown by `relay bots` and in the peers block injected into the sender's prompt. |
| `capabilities` | Capability tags such as `["code", "review"]`, exposed by `relay bots --json` and `/relay/bots`. |
| `relay_accept_from` | Bot ids allowed to relay to this bot; empty accepts any bot in the roster (membership is not checked). Referencing an unknown bot refuses to start. |

The legacy single `[bot]` config does not support relay; enabling it only logs a warning.

## Commands

### CLI

```bash
nectar relay bots [--json]
nectar relay send --to BOT [--no-reply] [--idempotency-key K] -- "self-contained brief"
nectar relay roots [--json] [--chat CHAT_ID]
nectar relay show ROOT [--json]
nectar relay cancel ROOT
```

- `relay bots` lists peers with roles and busy state.
- `relay send` needs `NECTAR_RELAY_TOKEN`, injected by Nectar when an agent run starts; the CLI reads it automatically. Calling it outside an agent run returns `ERR_UNAUTHORIZED`.
- `--no-reply` hands work off without expecting an answer.
- `--idempotency-key` dedupes retries; the same key never starts a second run.
- `roots` / `show` / `cancel` are operator tools over root tasks; agents must not poll them.
- Every subcommand accepts `--bot` / `--config` / `--socket` (resolved like `nectar schedule`), and `bots` / `send` / `roots` / `show` support `--json`.

### Chat command

- `/relay stop` cancels every open relay root in this chat (admin users only).

## Guardrails

- **Generation limit, envelope budget, run budget and root TTL** together allow a normal coder → reviewer → coder exchange without burning tokens.
- **Idempotency**: the default key hashes from, to, root, parent, kind and body, so legitimate repeats are not collapsed.
- **Command isolation**: relay bodies that start with `/cron`, `/stop` or another slash command stay plain text; they cannot create schedules or stop sessions.
- **Target permissions**: the target's `allowed_chats` still applies to relay, so chats outside that list are refused; an empty `relay_accept_from` accepts any bot in the roster. Relay does not additionally require `admin_users`: that list only gates session and schedule management commands, and relay bodies never parse commands. Relay runs carry the originating user id, so permission checks inside the target run use the real requester.
- **Prompt fence**: relay content is wrapped with a randomized tag that also names its source, and the body cannot close the fence early. The fence only mitigates prompt injection; the real boundaries are the receiver's sandbox, permissions and budgets.
- **Memory isolation**: relay-triggered runs never write failure lessons or memory candidates; the target can still read its own memory as background.
- **Tokens are not a sandbox**: run tokens bind bot / session / root / generation to prevent identity confusion and misrouting. The local socket is 0600, so the access boundary is the same OS user.
- **Restart recovery**: pending envelopes are delivered after a restart; running ones are marked failed (never retried automatically), and an interrupted ask sends the origin an interruption notice. Startup prunes roots that are closed or older than 24 hours along with their envelopes.

## Platform differences

| Platform | Hand-off visibility | Inbound loop protection |
| --- | --- | --- |
| Feishu | Cards; thread mirrors need an origin anchor | Drops `sender_type=bot` immediately, with an invisible marker as fallback |
| DingTalk | Group text/markdown mirror; direct-message form is unverified | Callbacks carry no sender type; relies on the invisible marker in the card body and the visible mirror title |
| Telegram | Text | The adapter already drops `is_bot` messages, and the platform does not deliver bot messages to other bots |

Mirrors use plain bot names only (for example "Coder → Reviewer") and never a real @ mention, so the platform cannot deliver a mirror to the target and trigger a second run. The first-mention rule needs mention names plus a bot flag from the platform: Feishu provides both; Telegram only exposes `text_mention` (rarely a bot); DingTalk callbacks carry no names, so there the rule does not apply and "mention only the initiator" remains a usage rule.

Unverified without real credentials: whether Feishu or DingTalk deliver one bot's group messages to a sibling bot, whether DingTalk's `openConversationId` matches across robots in one group, DingTalk group proactive-send permission, and zero-width marker survival through each renderer. The marker is a fallback; the primary defence is the platform sender type plus mirror text that contains no real mention.

## Error codes

| Code | Meaning | Action |
| --- | --- | --- |
| `ERR_GEN_LIMIT` | Chain too deep | Stop and report |
| `ERR_BUDGET` | Root envelope/run/time budget exhausted | Stop and report |
| `ERR_TARGET_UNKNOWN` | No such bot or root | Run `relay bots` |
| `ERR_REJECTED` | Target refuses this sender or this chat (`relay_accept_from` / `allowed_chats`), or the request is invalid (empty/oversized body) | Check the target allowlists |
| `ERR_UNAUTHORIZED` | Missing or expired run token | `relay send` only works inside an agent run |
| `ERR_MUTUAL_RELAY` | The same message already has an ask in flight in the opposite direction | Do not relay back; finish your own work or report to the user |
| `ERR_DIRECT_CHAT` | Relay started from a direct chat (refused by default) | Use a group chat, or enable `allow_direct` |
| `ERR_DISABLED` | Relay is off | Set `[relay] enabled = true` |

## Security advice

- Relay is off by default; enable it only when cross-bot collaboration is needed.
- Use `relay_accept_from` to name allowed senders, especially for bots with write access.
- With a `shared` workspace, give both bots comparably strict sandboxes; keep the reviewer in a read-only approval mode.
- Relay never crosses machines: every bot must run in one Nectar process under one OS user.
