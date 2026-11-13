# Feishu Setup and Troubleshooting

Nectar receives messages through the official Feishu SDK's long connection, so no public callback URL is required.

## Create the app

1. Create an "Enterprise self-built app" in the Feishu Open Platform.
2. Add the "Bot" capability.
3. Enable message-related permissions under "Permissions".
4. Under "Events and Callbacks", set the subscription mode to "Receive events through long connection".
5. Add "Receive message" `im.message.receive_v1` to the event list.
6. Create and publish a version under "Version Management and Release", and add users to the availability scope.

## Recommended permissions

Use the permissions currently shown in the Open Platform as the source of truth. At minimum you usually need:

- `im:message.p2p_msg:readonly`: read direct messages
- `im:message.group_at_msg:readonly`: read group messages that mention the bot
- `im:message:send_as_bot`: send messages as the bot
- `im:message`: update messages and cards
- `im:resource`: download images and files

## Verify the connection

Start Nectar and wait for this log line:

```text
feishu websocket connected; ready to receive messages
```

Send `/help` first. It bypasses Codex and verifies the Feishu inbound path and card sending. Then send a normal message to test Codex.

## Topic replies

Nectar reads `thread_id` from the original message event. If a message belongs to a topic, all replies and cards are sent to the same topic with Feishu's `reply_in_thread=true`; normal chat messages are still sent directly without quoting the original message.

The startup log shows `thread=` so you can confirm whether a message was recognized as a topic message.

## Images, files, and rich text

Feishu inbound messages support `text`, `post`, `image`, and `file`:

- An image-only message is passed to the agent with the default prompt.
- A mixed `post` message preserves the order of text and images, with image positions converted to `[Image N]`.
- Images and files are downloaded into a `.nectar/attachments/` subdirectory prefixed with the `message_id` under the current chat workspace, and cleaned up after the agent run finishes.
- Codex receives images through `--image`, OpenCode through `--file`; Cursor, Claude Code, Pi, and regular files are passed to the agent as absolute local paths in the prompt.
- Small text files such as text, Markdown, code, and JSON are injected directly into the current prompt (up to 48 KiB) so the agent does not ignore local paths.
- Audio, video, and merged forwarded messages are not supported yet.

## No reply

Check in order:

1. **Do you see the `connected` log?**
   The first connection may time out and reconnect automatically after a few dozen seconds. Messages sent before the connection succeeds are not replayed.

2. **Are multiple Nectar instances running?**
   Feishu dispatches events across online long-connection instances. Running an old and a new instance at the same time can cause messages to be handled by the other instance. Keep only one instance.

3. **Was the bot mentioned in a group chat?**
   Group chats only handle messages that mention the bot by default; direct messages do not need a mention.

4. **Does the App ID belong to the same app?**
   Confirm that `app_id` in `~/.nectar/config.toml` matches the app in the developer console.

5. **Is the event subscription correct?**
   "Receive message" `im.message.receive_v1` must be added, and the subscription mode must be long connection.

6. **Has the app version been published?**
   After changing permissions or event configuration, publish a new version and make it visible to users.

7. **Send `/help` first.**
   If `/help` replies but normal messages do not, Feishu inbound is working and the problem is on the Codex side.

8. **Run Codex manually.**
   In Nectar's working directory, run:

   ```bash
   codex exec --json --skip-git-repo-check "reply with OK"
   ```

## Common errors

| Symptom | Possible cause |
|---|---|
| `app_id is invalid` | The App ID is wrong, or the secret belongs to a different app |
| Messages arrive but the card does not update | Check the `im:message` permission and Feishu API errors in the log |
| Stuck on Thinking | Codex is not returning events; check Codex login, model configuration, and timeouts |
| The bot replies with an old format or quotes messages | An old Nectar version is still running; stop the old instance and restart |

Card updates rely on Feishu's message PATCH API. If an update fails, Nectar tries to send a new final card as a fallback.
