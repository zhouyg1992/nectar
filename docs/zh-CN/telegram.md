# Telegram 配置与排查

Nectar 通过 Telegram Bot API 长轮询接收消息，不需要公网回调地址、域名或 HTTPS 证书。

## 创建机器人

1. 在 Telegram 里打开 [@BotFather](https://t.me/BotFather)。
2. 发送 `/newbot`，按提示设置显示名和用户名。
3. 复制 BotFather 返回的 bot token，形如 `123456789:AA...`。

不需要执行 `/setwebhook`。Nectar 使用 `getUpdates` 长轮询；如果这个 bot 已配置过 webhook，需要先调用 `deleteWebhook`。

## 配置

```toml
[bot]
platform = "telegram"
engine = "codex"
admin_users = []       # 可选：允许的 Telegram 数字 user ID
allowed_chats = []     # 可选：允许的 Telegram 数字 chat ID

[telegram]
bot_token = "your-bot-token"
```

如果配置里只有 `[telegram]`，可以省略 `bot.platform`，Nectar 会自动选择 Telegram。

可选项：

```toml
[telegram]
# 大陆网络通常需要代理；服务模式不会继承 shell 的 HTTPS_PROXY。
proxy = "http://127.0.0.1:7890"
# 自建 Bot API Server 或测试网关。默认 https://api.telegram.org。
api_base_url = "https://api.telegram.org"
# 日志级别：debug | info | warn | error。默认 info。
# log_level = "info"
```

也支持环境变量：`TELEGRAM_BOT_TOKEN`、`TELEGRAM_PROXY`、`TELEGRAM_API_BASE_URL`。后台服务仍以配置文件为准。

## 验证链路

启动 Nectar 后等待日志出现：

```text
telegram: connected username=your_bot id=123456789
```

先私聊发送 `/help`，它不经过 Agent，可以直接验证收消息和发消息链路。然后再发普通消息测试 Agent。

## 群聊与话题

- 把 bot 拉进群后，默认隐私模式下只有三类消息会送达：`/命令`、`@机器人`、对机器人消息的回复。
- 即使关闭隐私模式，Nectar 也只处理上述三类消息，不会响应群里所有聊天。
- 群消息里的 `@bot_username` 和 `/command@bot_username` 会自动清理后再交给命令解析。
- Telegram 论坛话题使用 `message_thread_id` 区分会话，每个话题有独立的 Agent session。
- `allowed_chats` 使用数字 chat ID。群和超级群通常是负数，TOML 里要加引号，例如 `allowed_chats = ["-1001234567890"]`。

## 流式输出

Telegram 没有飞书/钉钉那种原生卡片，Nectar 把状态卡渲染成 HTML 消息，并用 `editMessageText` 更新：

1. 收到消息后发送 `Working` 消息。
2. 流式文本按节流后的间隔更新同一条消息。
3. 完成后更新为 `Complete` 或 `Failed`。

Telegram 单条消息上限 4096 个 UTF-16 单元。超长输出会自动分片；代码块在分片处会闭合并在下一片重新打开。编辑触发 429 时会按 `retry_after` 退避。

## 附件

入站支持：

- 图片（自动选择最大尺寸）
- 文件
- 音频、语音
- 视频、动图
- 贴纸（作为文件）

附件会下载到当前聊天工作目录下的 `.nectar/attachments/`，Agent 运行结束后自动清理。相册（media group）会短暂聚合，避免一次发多张图触发多轮 Agent。

云 Bot API 的下载上限是 20MB，附件下载还受核心 30 秒超时限制；超过限制时需要在 Telegram 客户端压缩/拆分，或自建 Bot API Server。当前版本暂不支持出站图片、文件和音频。

频道（channel）消息暂不支持；请使用单聊、群聊或论坛话题。

## 定时任务

Telegram 的主动发送只需要 `chat_id`，因此定时任务比钉钉简单：

- 私聊任务要求用户已经启动过这个 bot。
- 群聊任务要求 bot 仍在群里。
- 论坛话题任务会带上原来的 `message_thread_id`。

## 没有回复时

按顺序排查：

1. **是否看到 `telegram: connected` 日志**
   没有日志通常是 token 错误或网络不可达。

2. **是否只运行了一个 Nectar 实例**
   多个实例同时长轮询会返回 `409 Conflict`。如果之前配置过 webhook，也会出现同样的错误。

3. **代理是否生效**
   `api.telegram.org` 在大陆网络中通常不可直连。把代理写进 `[telegram] proxy` 后重启服务。

4. **群聊是否 @ 了机器人**
   隐私模式下普通群消息不会推送给 bot。先发 `/help` 或 `@bot_username /help`。

5. **是否被 allowlist 拦截**
   `admin_users` 和 `allowed_chats` 填的是数字 ID，不是用户名。

6. **是否触发限流**
   群聊发送频率有上限；日志里出现 429 时 Nectar 会自动退避，频繁出现则减少并发任务。

## 安全提示

- bot token 等同于机器人密码，不要提交到 Git；配置文件权限保持 `0600`。
- 消息文本和附件会经过 Telegram 云端；处理私有源码时先确认数据边界。
- `admin_users` 为空时不限制管理员权限，`allowed_chats` 为空时不限制群聊。生产环境建议都显式配置。
