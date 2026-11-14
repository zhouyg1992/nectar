# Security Boundaries

Codex/OpenCode/Cursor/Claude Code/Pi read, write, and execute commands in the configured working directory. Nectar is only a bridge and cannot replace operating-system-level isolation.

## Required practices

- Do not commit `app_secret` / `client_secret` or `~/.nectar/config.toml` to the repository.
- Configure `bot.admin_users` / `bot.allowed_chats`. When empty, the startup log warns that the bot is open to every reachable user.
- Use group chats only within trusted circles; when needed, enable the corresponding engine's `per_chat_workspace` so each chat uses its own subdirectory.
- On shared machines, consider running Nectar and engine CLIs under a dedicated Unix user.
- Enable `codex.bypass_approvals` with care. The default is the `workspace-write` sandbox.
- OpenCode's `opencode.auto_approve = true` automatically approves tool permissions; use it carefully in untrusted group chats.
- Cursor's `cursor.auto_approve = true` is equivalent to `-f` and automatically approves tool calls; `cursor.trust_workspace = true` skips the working-directory trust prompt. Both are enabled by default; tighten them in untrusted environments.
- Pi's `pi.trust_workspace = true` is equivalent to `--approve` and loads project settings, skills, context files, and extensions from the working directory. Disable it in untrusted repositories.

## Known limitations

- Nectar removes `FEISHU_APP_SECRET`, `TELEGRAM_BOT_TOKEN`, and `TELEGRAM_PROXY` from the engine subprocess environment; `DINGTALK_CLIENT_SECRET` provided through the environment is not removed yet. For long-running deployments, keep platform credentials in the configuration file rather than shell environment variables.
- Codex/OpenCode/Cursor/Claude Code/Pi can still read `~/.nectar/config.toml` under the same user. Store high-value secrets under a separate user or a separate machine.
- The working directory is not a sandbox boundary; it is only the engine's default working root. Real isolation requires a separate user, a container, or a system-level sandbox.
- The installer downloads over HTTPS and verifies SHA-256, but `curl | sh` still executes remote code. Prefer downloading the script and reviewing it before running.

## Feishu

- Grant the app only the message permissions it needs.
- Limit the app's availability scope.
- Rotate the App Secret regularly.
- Do not add the bot to untrusted group chats.

## DingTalk

- Grant the app only the message permissions it needs; enable `AICard.Instance.Write` only when streaming cards are required.
- Limit the app's availability scope and add only the users who need access.
- Rotate the Client Secret regularly, and make sure `client_id` / `client_secret` come from the same enterprise internal app.
- Do not add the bot to untrusted group chats.

## Telegram

- The bot token is a password. Rotate it through BotFather regularly and keep the config file at mode `0600`.
- Message text and attachments pass through Telegram's cloud. Confirm the data boundary before processing private source code.
- Set `bot.admin_users` and `bot.allowed_chats` explicitly, using numeric IDs rather than mutable usernames.
- Do not add the bot to untrusted group chats. Privacy mode reduces accidental triggers but does not replace an allowlist.
