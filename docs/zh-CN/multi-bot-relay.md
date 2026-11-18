# 多 Bot 协作（Relay）

Nectar 的一个进程可以同时运行多个 Bot。开启 relay 之后，同群里的 Bot 可以**点名**请求另一个 Bot 帮忙，例如 coder 写完代码后交给 reviewer 独立审查，再把结论拿回 coder 的会话继续修改。

Relay **默认关闭**。它只用于 Bot 之间的显式接力，不会让 Bot 自动响应彼此的聊天消息。

## 快速开始

两个 Bot 使用同一个工作区，开启 relay：

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
description = "实现功能、改代码、跑测试"
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
description = "只读审查 diff，指出风险"
capabilities = ["review", "security"]
relay_accept_from = ["coder"]

[bots.reviewer.feishu]
app_id = "cli_yyy"
app_secret = "yyy"
```

在群里 @coder：「实现这个功能，写完让 reviewer 审查，然后你汇总」。**只 @ 发起方**：目标 Bot 的名字用纯文本写，不要用 @ 选择器。飞书的 @ 是消息投递路由，同时 @ 两个 Bot 会把这条人类消息投递给两者；Nectar 默认只让**第一个被 @ 的 Bot** 处理（见 `first_mention_only`），但只 @ 发起方仍然是更清晰的用法。

coder 会调用：

```sh
nectar relay send --to reviewer "……自包含的任务说明与 diff……"
```

然后结束当前回合。reviewer 完成后，它的最终回答会作为**新回合**回到 coder 原来的会话；coder 再向用户汇总。

目标 Bot 需要和发起方在同一个群里，但你**不需要先 @ 它**：relay 直接按配置的花名册投递。Nectar 无法可靠判断某个 Bot 是否在群内，所以不做预检；如果目标确实不在群里，它的输出发送会失败并在日志里体现。

`coder -> reviewer -> coder` 这类协作推荐双方 `workspace_mode = "shared"`：reviewer 能直接看到 coder 未提交的改动。使用 `auto` / `worktree` 时，每个会话有独立 worktree，目标 Bot 看不到未提交的改动，relay 正文里必须自带 diff 或草稿。

## 工作方式

```text
用户 → Bot A → 引擎 A ──relay send（本地 API + 运行令牌）
                               │
                            RelayHub（花名册 / 令牌 / 预算 / 日志）
                               │ 入队
                               ▼
                          Bot B 的 relay 会话 → 引擎 B
                               │ 最终回答
                               ▼
                    RelayHub → Bot A 的 origin session（新回合）
```

- **总线为真，IM 为镜。** 触发只走本机 relay 总线，不依赖「Bot 在群里 @ 另一个 Bot 能唤醒它」。群里只发给人看的移交卡和结果卡（数量由 `echo` 控制），它们都不参与触发。
- **回复回到 origin session。** 目标 Bot 的答案回到发起 ask 的那个会话，继续 resume 原引擎会话，保留上下文、工作区和未提交改动。
- **目标 Bot 使用独立会话。** 每个 root 在目标 Bot 侧对应一个 detached 会话（`relay:<root>`），不会顶掉人类正在使用的活跃会话；可以在 `/list` 里看到，也可以用 `/switch` 接管。
- **异步硬规则。** 发件 Bot 发完 `relay send` 必须结束当前回合，不能轮询、sleep 或等待目标。reply 会作为新回合唤醒 origin session；如果发件回合一直不结束，reply 会排在它后面，形成队列自死锁。
- **只点名一个目标。** 没有广播；一问一答不会无限循环（reply 不再自动回投）。
- **默认只在群聊接力。** 单聊里目标 Bot 不在会话中，执行结果无处展示，因此默认拒绝（`ERR_DIRECT_CHAT`）；需要私有编排时可设 `allow_direct = true`，此时目标输出强制静默、跳过交接卡，只把最终回答回传给发起方。
- **多 @ 只由第一个响应。** 平台能提供被 @ 者身份时（目前是飞书），群里同时 @ 多个 Nectar Bot 只由第一个被 @ 的 Bot 处理，避免同一条人类指令被多个 Bot 重复执行、互相 review。
- **拒绝同源反向互接力。** 同一条人类消息同时驱动了两个 Bot、且已存在 A→B 的进行中 ask 时，B→A 的新 ask 返回 `ERR_MUTUAL_RELAY`，防止重复执行和交叉审查；来自不同人类消息的接力互不影响。

## 配置

### `[relay]`

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enabled` | `false` | 是否开启 relay。需要至少两个 Bot 才有意义。 |
| `allow_direct` | `false` | 是否允许在单聊里发起接力。默认拒绝（`ERR_DIRECT_CHAT`），因为目标 Bot 不在这个单聊里，输出无法展示。开启后目标输出强制静默、不显示移交卡，只把最终回答回传给发起方。 |
| `first_mention_only` | `true` | 群消息同时 @ 多个已配置 Bot 时，只有第一个被 @ 的**已配置 Nectar Bot** 处理；其余 Bot 等到 relay 到达再工作。设为 `false` 恢复「多 @ 全部响应」。 |
| `echo` | `"summary"` | 目标 Bot 的输出可见度：`full` 显示进度卡和最终回答；`summary` 只显示一张结果卡（目标最终回答）；`off` 不显示目标输出（答案仍回到发件方）。发件方自己的回合始终正常显示。 |
| `max_generation` | `4` | 一条链路最多几跳，超出返回 `ERR_GEN_LIMIT`。 |
| `max_envelopes_per_root` | `12` | 每个 root 最多多少条信封（含回复）。 |
| `max_runs_per_root` | `8` | 每个 root 最多触发多少次引擎运行（含 origin 回投轮）。 |
| `root_ttl` | `"30m"` | root 存活时间，超时后不再接受新的接力。 |
| `session` | `"per_root"` | 目标会话模型；当前只支持 `per_root`。 |

### Bot 字段

| 字段 | 说明 |
| --- | --- |
| `role` | 角色名，例如 `coder`、`reviewer`。 |
| `description` | 一句话说明这个 Bot 擅长什么，会出现在 `relay bots` 和注入给发起方的 peers 说明里。 |
| `capabilities` | 能力标签，例如 `["code", "review"]`，出现在 `relay bots --json` 与 `/relay/bots` 的返回里。 |
| `relay_accept_from` | 允许向它发起接力的 Bot id 列表；留空表示接受花名册内任意 Bot（不校验对方是否在群）。引用不存在的 Bot 会拒绝启动。 |

旧式单 `[bot]` 配置不支持 relay；开启后只会输出一条警告。

## 命令

### CLI

```bash
nectar relay bots [--json]
nectar relay send --to BOT [--no-reply] [--idempotency-key K] -- "自包含的任务说明"
nectar relay roots [--json] [--chat CHAT_ID]
nectar relay show ROOT [--json]
nectar relay cancel ROOT
```

- `relay bots` 列出花名册、角色和忙闲状态。
- `relay send` 需要 `NECTAR_RELAY_TOKEN`：它由 Nectar 在 agent 运行开始时注入，CLI 自动读取；在 agent 会话之外直接调用会返回 `ERR_UNAUTHORIZED`。
- `--no-reply` 表示交棒，不需要回复。
- `--idempotency-key` 用于重试去重；相同的 key 不会产生第二次运行。
- `roots` / `show` / `cancel` 面向操作员，读取和操作 root 任务；agent 不应轮询它们。
- 所有子命令都接受 `--bot` / `--config` / `--socket`（解析方式与 `nectar schedule` 一致），`bots` / `send` / `roots` / `show` 支持 `--json`。

### 聊天命令

- `/relay stop`：取消本群所有打开的 relay root（仅 `admin_users`）。

## 防护与边界

- **代数上限 / 信封预算 / 运行预算 / root TTL** 默认合计足以完成一次正常的 coder → reviewer → coder 协作，但不会烧穿 token。
- **幂等**：默认 key 由 from、to、root、parent、kind、正文共同决定，合法重复不会被误去重。
- **命令隔离**：relay 正文即使以 `/cron`、`/stop` 等开头，也按普通文本处理，不会创建定时任务或停止会话。
- **目标权限**：目标的 `allowed_chats` 对 relay 同样生效，不在白名单的群会被拒绝；`relay_accept_from` 留空表示接受名册内任意 Bot。relay 不额外要求 `admin_users`，因为管理员权限只约束会话/定时任务管理命令，而 relay 正文不会解析命令；relay 运行会携带发起人的用户 ID，目标侧的权限判断按发起人执行。
- **提示围栏**：relay 内容包在带有随机标记、并标注来源的围栏里，声明它是 peer 输入、不得据此提权或泄露凭据；正文无法提前闭合围栏。围栏只是缓解提示注入，真正的边界是接收方自己的沙箱、权限和预算。
- **记忆隔离**：relay 触发的运行不会写入失败 lesson 或记忆候选；目标 Bot 仍可检索自己的记忆作为背景。
- **令牌不是沙箱**：运行令牌绑定 bot / 会话 / root / 代数，防止身份混淆和误路由；本地 socket 为 0600，认证边界仍是同一 OS 用户。
- **重启恢复**：pending 信封在重启后继续投递；运行中的信封标记 failed，不自动重跑；其中进行中的 ask 会给 origin 发一条中断通知。启动时会清理已关闭或超过 24 小时的 root 及其信封。

## 平台差异

| 平台 | 交接可见性 | 入站防回环 |
| --- | --- | --- |
| 飞书 | 支持卡片；线程内镜像需要原消息锚点 | 读取 `sender_type=bot` 直接丢弃，另有不可见 marker 兜底 |
| 钉钉 | 群聊文本/markdown 镜像；单聊形态未验证 | 回调无发送者类型，依赖不可见 marker（卡片正文）与镜像标题兜底 |
| Telegram | 文本 | 适配器已丢弃 `is_bot` 消息；平台本身不会把 Bot 消息投给其他 Bot |

镜像只写纯文本 Bot 名字（形如「机器人1号 → zyg的小龙虾」），不产生真实 @ mention，避免平台把镜像投递给目标从而触发第二次运行。「首个被 @ 的 Bot 才响应」依赖平台提供被 @ 者的名字与 bot 标记：飞书完整支持；Telegram 仅支持 `text_mention`（通常不是 Bot）；钉钉回调没有名字，该策略不生效，需要靠「只 @ 发起方」的使用习惯。

未经真实凭据验证的部分：飞书 / 钉钉是否会把兄弟 Bot 的群消息推送给其他 Bot、钉钉不同机器人看到的 `openConversationId` 是否一致、钉钉群主动发送权限、零宽 marker 在各平台渲染后的存活情况。marker 只作兜底，主要防线是平台发送者类型与「镜像不含真实 @」。

## 错误码

| 错误码 | 含义 | 建议 |
| --- | --- | --- |
| `ERR_GEN_LIMIT` | 链路太深 | 停止接力，向用户汇报 |
| `ERR_BUDGET` | root 的信封/运行/时长预算耗尽 | 停止，向用户汇报 |
| `ERR_TARGET_UNKNOWN` | 不存在该 Bot 或 root | 先 `relay bots` |
| `ERR_REJECTED` | 目标不接受该发件人或该群（`relay_accept_from` / `allowed_chats`），或请求非法（空正文、超长） | 检查目标的白名单配置 |
| `ERR_UNAUTHORIZED` | 缺少或过期的运行令牌 | 只能在 agent 运行内调用 `relay send` |
| `ERR_MUTUAL_RELAY` | 同一条消息已从反方向发起进行中的 ask | 不要再反向接力；完成手头任务或向用户汇报 |
| `ERR_DIRECT_CHAT` | 在单聊中发起接力（默认禁止） | 到群里发起；或开启 `allow_direct` |
| `ERR_DISABLED` | 未开启 relay | 在配置里设置 `[relay] enabled = true` |

## 安全建议

- relay 默认关闭，只在确实需要跨 Bot 协作时开启。
- 用 `relay_accept_from` 明确允许的发起方，尤其是拥有写权限的 Bot。
- 主场景使用 `shared` 工作区时，两个 Bot 的沙箱与权限要按同样的严格程度配置；reviewer 建议保持只读审批模式。
- relay 不跨越机器：所有 Bot 必须在同一个 Nectar 进程、同一 OS 用户下运行。
