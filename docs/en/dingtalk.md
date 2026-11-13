# DingTalk Setup and Troubleshooting

Nectar receives messages through a DingTalk Stream mode long connection. No public callback URL, domain, or HTTPS certificate is required.

## Create the app

1. Create an "Enterprise internal app" in the [DingTalk Open Platform](https://open.dingtalk.com/).
2. Enable "Bot" under "App features".
3. Set the bot message receiving mode to "Stream mode".
4. Request message-related permissions under "Permission management".
5. Create and publish a version under "Version management and release", and add users to the availability scope.

You do not need to create a card template in the card platform. Nectar uses A2UI JSON cards generated directly by code.

## Recommended permissions

Use what the Open Platform currently shows as the source of truth. At minimum you usually need:

- Bot message subscription: receive direct messages and group messages that mention the bot
- Bot message sending: send Markdown replies and proactive messages
- `AICard.Instance.Write`: create and update streaming cards
- Bot message file download: download inbound images and files
- Bot emoji reply (optional): show a `🤔Thinking` emoji when cards are unavailable

If you do not grant `AICard.Instance.Write` for now, Nectar automatically falls back to plain Markdown and the bot still works.

To enable card permissions:

1. DingTalk Open Platform → App → Permission management.
2. Search for and request `AICard.Instance.Write` (AI card instance write permission).
3. Publish a new version and make sure current users are in the app's availability scope.
4. Restart Nectar to clear the 30-minute in-memory card fallback state.

## Configuration

```toml
[bot]
platform = "dingtalk"
engine = "codex"
admin_users = []       # Optional: allowed DingTalk staffId values
allowed_chats = []     # Optional: allowed openConversationId values

[dingtalk]
client_id = "dingxxxxxxxxxxxxxxxx"      # Formerly AppKey
client_secret = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # Formerly AppSecret
```

If the configuration contains only `[dingtalk]`, you can omit `bot.platform`; Nectar selects DingTalk automatically.

Optional settings:

```toml
[dingtalk]
# robot_code = "dingxxxxxxxxxxxxxxxx"  # Defaults to client_id
# log_level = "info"
# card_enabled = true                  # When false, use plain Markdown only
```

Legacy field names are also supported:

```toml
[dingtalk]
app_key = "dingxxxxxxxxxxxxxxxx"
app_secret = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

## Verify the connection

Start Nectar and wait for this log line:

```text
dingtalk: stream connected
```

Send `/help` first. It bypasses Codex and directly verifies DingTalk inbound handling and card sending. Then send a normal message to test the agent.

## Streaming cards

One agent run reuses the same card:

1. Create a `Working` card after receiving a message.
2. Tool calls and streaming text update the card at the Router's 2-second interval.
3. Update to `Complete` when finished, or `Failed` on error.

If the card API returns a permission error or `system.featureDisabled`, Nectar falls back to plain Markdown for 30 minutes.
During the fallback period, a `🤔Thinking` emoji is added to the user's message and removed when the agent finishes. If emoji permission is also unavailable, no extra indicator is shown, but final answers are still returned normally.

## Rich text and attachments

Inbound support:

- Plain text
- `richText` with mixed text and images; image positions are converted to `[Image N]`
- Image messages
- File messages
- Quoted replies; quoted content is passed to the agent as context

Images and files are downloaded to `.nectar/attachments/` under the current chat workspace and cleaned up automatically after the agent run.

GFM tables inside cards are parsed into DingTalk's native `Table` component; older clients fall back to raw table text.

Outbound images, files, and audio are not supported in the current version.

## Group and direct chats

- Direct chats work by sending a message directly.
- Group chats require mentioning the bot.
- Group chats share one session by default; different users in the same group share the agent session.
- If a group message still starts with `@bot`, Nectar strips it automatically in command scenarios.

## No reply

Check in order:

1. **Do you see the `dingtalk: stream connected` log?**
   The first connection retries every 3 seconds after a failure.

2. **Is only one Nectar instance running?**
   When multiple instances are online, messages may be handled by another instance.

3. **Was the bot mentioned in a group chat?**
   DingTalk group messages are usually delivered only when the bot is mentioned.

4. **Do the Client ID and Client Secret belong to the same app?**
   Both values must come from the same enterprise internal app.

5. **Has the app version been published?**
   After changing permissions or bot configuration, publish a new version and make it visible to users.

6. **Send `/help` first.**
   If `/help` replies but normal messages do not, inbound handling works and the problem is on the agent side.

## Common errors

| Symptom | Possible cause |
|---|---|
| `client_id and client_secret are required` at startup | The configuration is missing values or still contains placeholders |
| `access denied` / `AICard.Instance.Write` | AI card permission is not enabled; try setting `card_enabled = false` first |
| The card stays on `Working` | The agent is not returning events, or the card update API is rate-limited |
| No response in group chats | The message did not mention the bot, or the bot is not in the group |
| Image/file download fails | Missing message file download permission, or `downloadCode` has expired |
