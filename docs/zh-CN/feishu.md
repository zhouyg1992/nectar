# 飞书配置与排查

Nectar 通过飞书官方 SDK 的长连接接收消息，不需要公网回调地址。

## 创建应用

1. 在飞书开放平台创建「企业自建应用」。
2. 添加「机器人」能力。
3. 在「权限管理」开通消息相关权限。
4. 在「事件与回调」把订阅方式设为「使用长连接接收事件」。
5. 在事件列表添加「接收消息」`im.message.receive_v1`。
6. 在「版本管理与发布」创建并发布版本，把使用者加入可用范围。

## 推荐权限

以开放平台当前展示为准，通常至少需要：

- `im:message.p2p_msg:readonly`：读取单聊消息
- `im:message.group_at_msg:readonly`：读取群里 @机器人 的消息
- `im:message:send_as_bot`：以机器人身份发送消息
- `im:message`：更新消息/卡片
- `im:resource`：下载图片和文件

## 验证链路

启动 Nectar 后等待日志出现：

```text
feishu websocket connected; ready to receive messages
```

先发 `/help`。它不经过 Codex，可以直接验证飞书入站和卡片发送链路。然后再发普通消息测试 Codex。

## 话题回复

Nectar 会从消息原始事件里读取 `thread_id`。如果消息属于某个话题，所有回复和卡片都会使用飞书的 `reply_in_thread=true` 发送到同一话题；普通聊天消息仍然直接发送，不引用原消息。

启动日志里会显示 `thread=`，方便确认消息是否被识别为话题消息。

## 图片、文件与富文本

飞书入站支持 `text`、`post`、`image` 和 `file`：

- 纯图片会使用默认提示词交给 Agent 分析。
- 图文混合的 `post` 会保留文字和图片顺序，图片位置转换为 `[Image N]`。
- 图片和文件会下载到当前聊天工作目录下以 `message_id` 为前缀的 `.nectar/attachments/` 子目录，Agent 运行结束后自动清理。
- Codex 使用 `--image`、OpenCode 使用 `--file` 接收图片；Cursor 和普通文件通过 prompt 中的本地绝对路径交给 Agent 读取。
- 文本、Markdown、代码、JSON 等小型文本文件会直接注入本轮 prompt（最多 48 KiB），避免 Agent 忽略本地路径。
- 暂不支持音频、视频和合并转发消息。

## 没有回复时

按顺序排查：

1. **是否看到 `connected` 日志**  
   首次连接可能超时并在几十秒后自动重连。连接成功前发送的消息不会补发。

2. **是不是多个 Nectar 实例同时在跑**  
   飞书会把事件分发到在线的长连接实例。新旧实例同时运行会导致消息被另一个实例处理。只保留一个实例。

3. **群聊是否 @ 了机器人**  
   群聊默认只处理 @Bot 的消息；单聊不需要 @。

4. **App ID 是否对应同一个应用**  
   确认 `~/.nectar/config.toml` 里的 `app_id` 就是当前开发者后台里的应用。

5. **事件订阅是否正确**  
   「接收消息」`im.message.receive_v1` 必须已添加，订阅方式必须是长连接。

6. **应用版本是否已发布**  
   权限和事件配置修改后，需要发布新版本并对使用者可见。

7. **先发 `/help`**  
   `/help` 有回复但普通消息没有，说明飞书入站正常，问题在 Codex 执行侧。

8. **手动执行 Codex**  
   在 Nectar 的工作目录里运行：

   ```bash
   codex exec --json --skip-git-repo-check "reply with OK"
   ```

## 常见错误

| 现象 | 可能原因 |
|---|---|
| `app_id is invalid` | App ID 写错，或使用了另一个应用的 Secret |
| 收到消息但卡片不动 | 检查 `im:message` 权限，以及日志里的飞书 API 错误 |
| 一直显示 Thinking | Codex 没有返回事件；检查 Codex 登录、模型配置和超时 |
| 机器人回复旧格式或引用消息 | 仍在运行旧版本 Nectar，停掉旧实例并重启 |

卡片更新依赖飞书消息 PATCH 接口。如果更新失败，Nectar 会尝试发送一张新的最终卡片作为降级。
