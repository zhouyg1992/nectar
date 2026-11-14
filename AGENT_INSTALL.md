# Installing Nectar as an AI Agent

You are an AI coding agent. Follow this file when the user asks you to install, set up, or deploy Nectar. It is written to be executed, not summarized.

Contract version: 1, for Nectar v0.7.0 and later.

Nectar connects Feishu / Lark, DingTalk, or Telegram to a coding-agent CLI already installed on the user's machine (Codex, Claude Code, Cursor Agent, OpenCode, or Pi). It runs as a background service.

## Rules

- Install the released binary with `install.sh`. Do not clone the repository or build from source unless the user explicitly asks for a source build.
- Never invent, guess, or reuse credentials. Ask the user for them.
- Never print a token or secret back to the user, and never write one into a repository file.
- If you cannot pass a secret without exposing it in shell history or command logs, ask the user to run the final install command themselves.
- Ask before installing a background service. The installer installs and starts one by default.
- Do not install an engine CLI on the user's behalf without permission.
- Do not use `sudo` unless the user explicitly asks. The default install paths are user-writable.
- Do not claim success until the verification commands in this file pass.

## 1. Confirm the environment

```sh
uname -s        # Darwin or Linux. On Windows, use WSL.
uname -m        # x86_64/amd64 or arm64/aarch64
command -v nectar >/dev/null 2>&1 && nectar --version || echo "nectar not installed"
for c in codex opencode agent cursor-agent claude pi; do
  command -v "$c" || true
done
```

Stop and ask the user if:

- The OS is not macOS or Linux.
- No supported engine CLI is installed. The user must install and log in to one first; see [engine setup](docs/en/engines.md).
- A `nectar` binary already exists. Treat this as an upgrade, not a fresh install (see section 6).

The installer needs `curl` or `wget`, `tar`, and `sha256sum` or `shasum`.

## 2. Collect inputs

| Input | Allowed values | How to obtain it |
| --- | --- | --- |
| Platform | `feishu`, `dingtalk`, `telegram` | Ask the user which chat platform they use |
| Engine | `codex`, `opencode`, `cursor`, `claude`, `pi` | Pick one installed and logged-in engine; ask if several are available |
| Credentials | See the platform section | Ask the user; never fabricate |
| Version | `latest` or a `vX.Y.Z` tag | Use latest unless the user wants a pinned version |
| Background service | yes / no | Ask the user; recommended |

Platform credential formats:

- Feishu: `app_id` (usually starts with `cli_`) and `app_secret`.
- DingTalk: `client_id` (usually starts with `ding`) and `client_secret`.
- Telegram: bot token from BotFather, shaped like `123456789:AA...`.

## 3. Install

Telegram is the only platform that can be installed with a single user-supplied credential. Feishu and DingTalk require the user to create and publish an app in the platform console first.

In the commands below, replace `<engine>` with the chosen engine.

### Telegram

Ask the user to complete these steps:

1. Open [@BotFather](https://t.me/BotFather) in Telegram and send `/newbot`.
2. Follow the prompts and copy the bot token.
3. If the bot previously used a webhook, delete it; Nectar uses `getUpdates` long polling.

Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env TELEGRAM_BOT_TOKEN="$TELEGRAM_BOT_TOKEN" \
  sh -s -- --engine <engine> --platform telegram
```

Pass the token through an environment variable, not as a literal argument.

### Feishu / Lark

Ask the user to complete these steps in the Feishu Open Platform:

1. Create an enterprise self-built app.
2. Add the Bot capability.
3. Enable message permissions: `im:message.p2p_msg:readonly`, `im:message.group_at_msg:readonly`, `im:message:send_as_bot`, `im:message`, and `im:resource`.
4. Under Events and Callbacks, select "Receive events through long connection" and add `im.message.receive_v1`.
5. Create and publish a version, then add the intended users to the availability scope.
6. Provide the app_id and app_secret.

Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env FEISHU_APP_ID="$FEISHU_APP_ID" FEISHU_APP_SECRET="$FEISHU_APP_SECRET" \
  sh -s -- --engine <engine> --platform feishu
```

### DingTalk

Ask the user to complete these steps in the [DingTalk Open Platform](https://open.dingtalk.com/):

1. Create an enterprise internal app.
2. Enable the Bot capability and set message receiving to Stream mode.
3. Request the message-related permissions. `AICard.Instance.Write` enables streaming cards; without it Nectar falls back to Markdown.
4. Create and publish a version, then add the intended users to the availability scope.
5. Provide the client_id and client_secret.

Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env DINGTALK_CLIENT_ID="$DINGTALK_CLIENT_ID" DINGTALK_CLIENT_SECRET="$DINGTALK_CLIENT_SECRET" \
  sh -s -- --engine <engine> --platform dingtalk
```

### Installer options

| Need | Add |
| --- | --- |
| Pin a release | `--version vX.Y.Z` |
| Custom binary directory | `--dir DIR` |
| Custom config path | `--config PATH` |
| Replace an existing config | `--reconfigure` |
| No background service | `--no-service` |
| Install the service without starting it | `--no-start` |
| Start at boot without login (Linux) | `--linger` |

Defaults: binary in `~/.local/bin/nectar`, config in `~/.nectar/config.toml`, service enabled and started, `nectar-schedule` skill installed. The installer writes the config with mode `0600`.

If the user chose a custom `--dir`, use `<dir>/nectar` instead of `~/.local/bin/nectar` in the commands below.

## 4. Verify

```sh
export PATH="$HOME/.local/bin:$PATH"
nectar --version
nectar service status
nectar service logs -n 200
```

Wait up to about 60 seconds for the platform connection line:

| Platform | Expected log |
| --- | --- |
| Feishu | `feishu websocket connected; ready to receive messages` |
| DingTalk | `dingtalk: stream connected` |
| Telegram | `telegram: connected username=` |

Then ask the user to send `/help` to the bot. `/help` does not invoke the engine, so it verifies the inbound and outbound chat path. A normal message verifies the engine path.

If the connection line never appears, report the actual error and stop. Do not guess or restart repeatedly.

## 5. Report

Report these facts to the user:

- Installed version, platform, and engine.
- Config path and binary path.
- Service status.
- Whether the connection log appeared.
- The next action: send `/help` in chat.

## 6. Upgrade, reconfigure, uninstall

If Nectar is already installed, upgrade instead of reinstalling:

```sh
nectar update --check
nectar update
```

Re-running `install.sh` with an existing config keeps that config. Use `--reconfigure` only when the user wants to change the platform or credentials; the old config is backed up first.

Never run two Nectar instances against the same platform credentials at the same time. Long-connection platforms deliver each message to only one of the connected instances.

Uninstall:

```sh
nectar uninstall                  # service, binary, skills, and managed AGENTS.md blocks
nectar uninstall --purge-data     # also config, task data, workspaces, and backups
```

Pass `--binary` and `--config` when the user installed to non-default paths.

## 7. Stop conditions

Stop and ask the user when:

- Credentials are missing or malformed.
- The platform console steps have not been completed.
- The engine CLI is installed but not logged in.
- Service installation fails. Show the exact error and the recovery command printed by the installer.
- The platform connection log does not appear.

Do not disable checksum verification, change the download URL, or patch installed files to work around a failure.

## 8. Security defaults

Nectar runs with empty `bot.admin_users` and `bot.allowed_chats` allowlists unless the user configures them. Empty lists leave administration and group access unrestricted.

After installation, ask the user whether the bot is exposed to shared chats. If yes, offer to restrict `bot.allowed_chats` (which groups may use the bot) and `bot.admin_users` (who may manage scheduled tasks) in the config file, then restart the service. See [docs/en/config.md](docs/en/config.md).

For more detail, see [install and upgrade](docs/en/install.md), [Feishu setup](docs/en/feishu.md), [DingTalk setup](docs/en/dingtalk.md), and [Telegram setup](docs/en/telegram.md).
