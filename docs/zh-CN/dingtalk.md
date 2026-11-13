# 钉钉配置与排查

Nectar 通过钉钉 Stream 模式的长连接接收消息，不需要公网回调地址，也不需要域名或 HTTPS 证书。

## 创建应用

1. 在[钉钉开放平台](https://open.dingtalk.com/)创建「企业内部应用」。
2. 在「应用功能」中启用「机器人」。
3. 机器人消息接收模式选择「Stream 模式」。
4. 在「权限管理」申请消息相关权限。
5. 在「版本管理与发布」创建版本并发布，把使用者加入可用范围。

不需要在卡片平台创建模板。Nectar 使用 A2UI JSON 卡片，卡片结构由代码直接生成。

## 推荐权限

以开放平台当前展示为准，通常至少需要：

- 机器人消息订阅：接收单聊和群聊 @ 消息
- 机器人消息发送：发送 Markdown 回复和主动消息
- `AICard.Instance.Write`：创建和更新流式卡片
- 机器人消息文件下载：下载入站图片和文件
- 机器人表情回复（可选）：卡片不可用时显示 `🤔Thinking` 表情

如果暂时不授予 `AICard.Instance.Write`，Nectar 会自动降级为普通 Markdown，机器人仍然可用。

开通卡片权限的路径：

1. 钉钉开放平台 → 应用 → 权限管理。
2. 搜索并申请 `AICard.Instance.Write`（AI 卡片实例写权限）。
3. 发布新版本，并确保当前使用者在该应用可用范围内。
4. 重启 Nectar，清除内存中的 30 分钟卡片降级状态。

## 配置

```toml
[bot]
platform = "dingtalk"
engine = "codex"
admin_users = []       # 可选：允许的钉钉 staffId
allowed_chats = []     # 可选：允许的 openConversationId

[dingtalk]
client_id = "dingxxxxxxxxxxxxxxxx"      # 旧版 AppKey
client_secret = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # 旧版 AppSecret
```

如果配置里只有 `[dingtalk]`，可以省略 `bot.platform`，Nectar 会自动选择钉钉。

可选项：

```toml
[dingtalk]
# robot_code = "dingxxxxxxxxxxxxxxxx"  # 默认等于 client_id
# log_level = "info"
# card_enabled = true                  # false 时只用普通 Markdown
```

也支持旧字段名：

```toml
[dingtalk]
app_key = "dingxxxxxxxxxxxxxxxx"
app_secret = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

## 验证链路

启动 Nectar 后等待日志出现：

```text
dingtalk: stream connected
```

先发 `/help`，它不经过 Codex，可以直接验证钉钉入站和卡片发送链路。然后再发普通消息测试 Agent。

## 流式卡片

一次 Agent 执行会复用同一张卡片：

1. 收到消息后创建 `Working` 卡片。
2. 工具调用和流式文本按 Router 的 2 秒间隔更新卡片。
3. 完成后更新为 `Complete`，失败时更新为 `Failed`。

如果卡片接口返回权限不足或 `system.featureDisabled`，Nectar 会在 30 分钟内降级为普通 Markdown。
降级期间会在用户消息上贴一个 `🤔Thinking` 表情，Agent 完成后再撤回；如果表情权限也未开通，
则不会显示额外提示，但最终答案仍会正常返回。

## 富文本与附件

入站支持：

- 纯文本
- 图文混排的 `richText`，图片位置会转换为 `[Image N]`
- 图片消息
- 文件消息
- 引用回复，引用内容会作为上下文传给 Agent

图片和文件会下载到当前聊天工作目录下的 `.nectar/attachments/`，Agent 运行结束后自动清理。

卡片内的 GFM 表格会解析为钉钉原生 `Table` 组件；旧客户端无法渲染时使用原始表格文本兜底。

当前版本暂不支持出站图片、文件和音频。

## 群聊与单聊

- 单聊直接发消息即可。
- 群聊需要 @ 机器人。
- 群聊默认共享一个会话；不同用户在同一群里共享 Agent 会话。
- 如果群消息文本仍带有 `@机器人` 前缀，Nectar 会在命令场景下自动剥离。

## 没有回复时

按顺序排查：

1. **是否看到 `dingtalk: stream connected` 日志**
   首次连接失败会每 3 秒重试一次。

2. **是否只运行了一个 Nectar 实例**
   多个实例同时在线时，消息可能被另一个实例处理。

3. **群聊是否 @ 了机器人**
   钉钉群消息通常只有 @ 机器人时才会投递。

4. **Client ID / Client Secret 是否对应同一个应用**
   两个值必须来自同一个企业内部应用。

5. **应用版本是否已发布**
   权限和机器人配置修改后，需要发布新版本并对使用者可见。

6. **先发 `/help`**
   `/help` 有回复但普通消息没有，说明入站正常，问题在 Agent 执行侧。

## 常见错误

| 现象 | 可能原因 |
|---|---|
| 启动时报 `client_id and client_secret are required` | 配置没有填写，或使用了占位值 |
| `access denied` / `AICard.Instance.Write` | 没有开通 AI 卡片权限；可先关闭 `card_enabled` |
| 卡片一直停在 `Working` | Agent 没有返回事件，或卡片更新接口限流 |
| 群聊没有反应 | 消息没有 @ 机器人，或机器人不在群里 |
| 图片/文件下载失败 | 缺少消息文件下载权限，或 `downloadCode` 已过期 |

## 相关文档

- [钉钉平台接入设计](dingtalk-design.md)
- [架构与扩展点](architecture.md)

返回：[文档索引](README.md)
