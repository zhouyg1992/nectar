# Telegram Setup and Troubleshooting

Nectar receives messages through Telegram Bot API long polling. No public callback URL, domain, or TLS certificate is required.

## Create a bot

1. Open [@BotFather](https://t.me/BotFather) in Telegram.
2. Send `/newbot` and follow the prompts.
3. Copy the bot token returned by BotFather, for example `123456789:AA...`.

Do not run `/setwebhook`. Nectar uses `getUpdates` long polling. If the bot already has a webhook configured, call `deleteWebhook` first.

## Configuration

```toml
[bot]
platform = "telegram"
engine = "codex"
admin_users = []       # optional: allowed numeric Telegram user IDs
allowed_chats = []     # optional: allowed numeric Telegram chat IDs

[telegram]
bot_token = "your-bot-token"
```

If only `[telegram]` is configured, `bot.platform` can be omitted and Nectar selects Telegram automatically.

Optional settings:

```toml
[telegram]
# Mainland China usually requires a proxy; services do not inherit HTTPS_PROXY.
proxy = "http://127.0.0.1:7890"
# Self-hosted Bot API server or test gateway. Default: https://api.telegram.org.
api_base_url = "https://api.telegram.org"
# Log level: debug | info | warn | error. Default: info.
# log_level = "info"
```

Environment variables are also supported: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_PROXY`, `TELEGRAM_API_BASE_URL`. Background services still read the config file.

## Verify the connection

After starting Nectar, wait for:

```text
telegram: connected username=your_bot id=123456789
```

Send `/help` in a direct chat first. It bypasses the agent and verifies both inbound and outbound paths. Then send a normal message to test the agent.

## Groups and topics

- With privacy mode enabled (the default), Telegram only delivers commands, `@bot` mentions, and replies to the bot's messages.
- Even with privacy mode disabled, Nectar only processes those three kinds of messages.
- `@bot_username` and `/command@bot_username` are stripped before command parsing.
- Forum topics use `message_thread_id`; each topic gets an independent agent session.
- `allowed_chats` uses numeric chat IDs. Groups and supergroups are usually negative, so quote them in TOML: `allowed_chats = ["-1001234567890"]`.

## Streaming output

Telegram has no native card equivalent to Feishu or DingTalk. Nectar renders the status card as an HTML message and updates it with `editMessageText`:

1. Send a `Working` message after receiving a prompt.
2. Update the same message with streamed text at a throttled interval.
3. Edit it to `Complete` or `Failed` when the run finishes.

Telegram limits one message to 4096 UTF-16 code units. Long output is split automatically; code fences are closed at the boundary and reopened in the next chunk. On 429, Nectar backs off using `retry_after`.

## Attachments

Inbound support:

- Photos (largest size)
- Documents
- Audio and voice messages
- Video and animations
- Stickers (as files)

Files are downloaded to `.nectar/attachments/` under the current chat workspace and removed after the agent run. Albums (media groups) are briefly coalesced so one album does not start several agent turns.

The cloud Bot API download limit is 20MB, and attachment downloads also use the core 30-second timeout. Compress or split larger files, or run a self-hosted Bot API server. Outbound images, files, and audio are not implemented yet.

Channel posts are not supported yet; use direct chats, groups, or forum topics.

## Scheduled tasks

Telegram proactive sends only need a `chat_id`, which makes scheduling simpler than DingTalk:

- Direct-chat tasks require the user to have started the bot.
- Group tasks require the bot to still be in the group.
- Forum-topic tasks preserve the original `message_thread_id`.

## No reply

Check in order:

1. **Is `telegram: connected` in the logs?**
   No log usually means an invalid token or unreachable network.

2. **Is only one Nectar instance running?**
   Multiple pollers get `409 Conflict`. A previously configured webhook causes the same error.

3. **Is the proxy working?**
   `api.telegram.org` is normally unreachable from mainland China. Put the proxy in `[telegram] proxy` and restart the service.

4. **Was the bot mentioned in a group?**
   Privacy mode does not deliver ordinary group chatter. Try `/help` or `@bot_username /help`.

5. **Is the allowlist blocking it?**
   `admin_users` and `allowed_chats` use numeric IDs, not usernames.

6. **Is Telegram rate limiting?**
   Group sends have limits. Nectar backs off on 429; if it happens constantly, reduce concurrent tasks.

## Security notes

- The bot token is a password. Never commit it; keep the config file at mode `0600`.
- Message text and attachments pass through Telegram's cloud. Confirm the data boundary before using it with private source code.
- Empty `admin_users` does not restrict admin access, and empty `allowed_chats` does not restrict groups. Set both explicitly in production.
