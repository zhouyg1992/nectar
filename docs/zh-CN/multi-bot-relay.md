# Nectar 多 Bot 协作方案 v2

> 状态：设计稿，未实现。基于 v1《nectar 多 bot 协作方案》，按当前 v0.10.1 源码核对后修订。
>
> 一句话：**总线为真、IM 为镜；reply 回原会话，产物用复制而不是传路径。**

## M0 实现进度

当前分支已实现本文 M0 的原型：

- `[relay]` 与 Bot 的 `role` / `description` / `capabilities` / `relay_accept_from` / `relay_prompt` 配置。
- 进程内 RelayHub、`relay/state.json` 持久化、运行令牌、花名册。
- `nectar relay bots` / `send` / `roots` / `show` / `cancel`，以及每个 Bot 本地 API 上的 `/relay/*`。
- 目标 Bot 的 detached per-root 会话；reply 回到 origin session。
- `echo=summary/off`、飞书移交卡 / review 结果卡、`sender_type=bot` 过滤、不可见 marker 兜底。
- 代数上限、幂等、根任务信封/运行预算、`/relay cancel` 的跨 Bot 取消。
- 假引擎测试覆盖 ask → reply → origin、mirror 丢弃、gen limit、幂等、HTTP API。

尚未实现（按 v2 的 M1/M2 计划）：

- 飞书真实 `@` 的 open_id 缓存。M0 的 mirror 用纯文本 `@name`。
- artifact 复制/materialize、`--wait`、`--mode steer`、房间日志、团队卡、dashboard relay 页。
- 乒乓检测、单目标熔断、`/relay status/stop/on/off` 聊天命令。

## 结论先行

方向可行。Nectar 已经具备同进程多 Bot、独立平台身份、独立引擎/工作区/会话/记忆、per-session FIFO 队列和本地 Unix socket，做 Bot 间协作不需要引入分布式消息系统。

但 v1 有两个会直接打断主场景的语义问题，必须先改：

1. **回复不能投到新的 per-root 会话。** 否则发件 Bot 被唤醒时拿到的是新会话、新 worktree、空引擎上下文，看不到自己刚改的代码。回复必须回到**发起该根任务的 origin session**。
2. **不能把“文件路径”当成交付物。** `auto/worktree` 下每个会话是独立 worktree，从 `HEAD` 创建，未提交改动不会被复制。目标 Bot 看不到源 Bot 的改动。M0 要么双方 `shared` 同一 workspace，要么把 diff/草稿内联，或由 RelayHub 把产物复制到目标 workspace。

因此 v2 的三个关键决策是：

1. **进程内 RelayHub + 每个 Bot 自己的 API socket。** 不是“一个 daemon socket”，而是 Hub 挂在各 Bot 的本地 API 上。
2. **目标 Bot 用 detached per-root 会话；发件 Bot 的 reply 回到 origin session。**
3. **M0 只做异步、同群、飞书、`shared` 工作区；`--wait`、worktree、dispatch 后移。**

## 目标与非目标

### 目标

- 一个群里的 Bot 可以点名请求另一个 Bot，跨引擎协作。
- 协作过程在群里可见、可审计；不依赖“IM 里 Bot @ Bot 能触发另一个 Bot”这种平台特性。
- 有代数上限、根任务预算、幂等和熔断，默认不会无限互聊、不会烧穿 token。
- daemon 重启后，未处理的信封可以继续投递；运行中的任务不自动重跑。
- 不破坏现有单 Bot、多 Bot、记忆、定时任务和权限模型。

### 非目标（M0）

- 不做动态规划、自动组队、lead 自由拆任务。
- 不做跨进程/跨机器。
- 不做 `--wait`、房间日志、团队卡。
- 不做 worktree 并行写同一仓库。
- 不做飞书之外的镜像；总线本身仍然平台无关。

## 现状核对

| 能力 | 代码位置 | 对 relay 的意义 |
| --- | --- | --- |
| 同进程多 Bot，每个 Bot 独立配置、引擎、工作区、会话、记忆 | `main.go:112` | RelayHub 可以直接在内存里找到目标 runtime |
| 每个 Bot 独立数据目录和本地 Unix socket | `main.go:174`、`main.go:217` | 不是单一 daemon socket；Hub 要挂到每个 Bot 的 API 上 |
| 入站消息统一进 Router，按 session ID 进 FIFO 队列 | `internal/core/router.go:322`、`internal/core/router.go:405` | 新增 relay 执行入口，不需要重写入站分发 |
| 系统任务可以指定 session、等待完成、跳过斜杠命令 | `internal/core/router.go:420` | `ExecuteScheduled` 是 relay 执行路径的现成模板 |
| 每次运行注入 `NECTAR_*` 环境变量 | `internal/core/router.go:489` | 可以直接加 `NECTAR_RELAY_TOKEN`、`NECTAR_RELAY_ROOT` |
| 工作区锁是进程级共享 | `internal/exec/locks.go:5` | 两个 Bot 指向同一 workspace 时会被串行化 |
| worktree 按 session 从 `HEAD` 创建 | `internal/exec/workspace.go:388` | 未提交改动不会出现在目标 Bot 的 worktree |
| 本地 API socket 0600，文件系统权限是认证边界 | `internal/core/schedule_api.go:30` | 适合本机 agent/CLI；不要把它当成对抗同用户恶意进程的边界 |
| 平台消息有 `Origin` 字段 | `internal/platform/platform.go:38` | 加 `relay` 来源，跳过命令解析 |
| 平台 `Reply` 没有 mention 字段 | `internal/platform/platform.go:49` | 飞书 mirror 的 `@` 需要补 `Mentions` |
| `execute` 只返回 error | `internal/core/router.go:468` | 需要 `RunResult` 才能把最终回答回投 |
| 飞书 SDK 只过滤 Bot 自己，不按 sender_type 过滤其他 Bot | `internal/platform/feishu/adapter.go:468` 周边 | 兄弟 Bot 的 mirror 消息如果被投递过来，会进 Router，必须做丢弃 |

v1 的四个缺口仍然成立：没有花名册、没有 Bot 间通道、没有跨 Bot 预算/防环路、没有团队视图。v2 在此之上补两个语义：**origin session** 和 **artifact handoff**。

## 核心设计

### 1. 架构：进程内 RelayHub

```text
human ──> Bot A adapter ──> Router A ──> engine A
                                  │
                                  │ nectar relay send (token)
                                  ▼
                            RelayHub
                 journal / tokens / budgets / roster
                    │                    │
                    │ enqueue            │ mirror (best effort)
                    ▼                    ▼
              Router B queue        IM group
                    │
              engine B ──> result ──> RelayHub ──> Router A origin session
```

- `RelayHub` 在 `runBot` 里创建，进程内唯一，使用全局数据目录下的 `relay/`。
- 每个 `botRuntime` 创建后注册到 Hub；Hub 保存 `botID -> runtime` 的路由表。
- 每个 Bot 的 `ScheduleAPI` 挂上 `/relay/*` 路由；agent 侧仍然只认自己的 `NECTAR_API_SOCKET`。
- RelayHub 不直接持有引擎；它只调用目标 Router 的 `ExecuteRelay`，由目标 Router 决定会话、工作区、沙箱和平台输出。
- 如果某个 Bot 初始化失败，Hub 的路由表里没有它，`relay send` 返回 `ERR_TARGET_UNKNOWN`，不影响其他 Bot。

### 2. 根任务、身份与令牌

**根任务（root）由人类消息或定时任务创建。** Relay 消息继承 root，不新建 root。

| 场景 | root | gen |
| --- | --- | --- |
| 人类在群里发消息 | 新建 | 0 |
| 定时任务触发 | 新建 | 0 |
| Bot 发出 `ask`/`handoff`/`notify` | 继承 | 父信封 gen + 1 |
| 目标 Bot 发出 `reply` | 继承 | 被回复信封 gen + 1 |
| `reply` 唤醒 origin session 后产生的新一轮 | 继承 | 该 reply 的 gen |

Root 记录至少保存：

- `origin_bot`、`origin_session_id`、`origin_run_id`
- `origin_channel`、`origin_chat_id`、`origin_thread_id`
- `origin_workspace`、`origin_workspace_mode`
- `origin_prompt`（截断后的原始任务，用于 origin session 丢失时恢复）
- `created_at`、`budget`、`state`

**运行令牌。** daemon 每次拉起引擎子进程时注入：

```text
NECTAR_API_SOCKET=<已有>
NECTAR_RELAY_TOKEN=<32 字节随机值>
NECTAR_RELAY_ROOT=<root id>
NECTAR_RELAY_ENVELOPE=<触发本轮的信封 id，人类发起时为空>
```

令牌只在本次运行内有效，绑定 `(run_id, bot_id, session_id, root_id, gen, kind)`。Hub 在运行结束时撤销。服务端**自己计算** from、chat、root、gen，忽略客户端传来的身份字段。agent 最多只能“以自己的名义”发 relay，不能冒充别的 Bot，也不能把代数改小。

令牌不是沙箱边界。它解决的是身份混淆和误路由；真正的边界仍然是 OS 用户、workspace sandbox、`relay_accept_from` 和预算。不要把它宣传成“agent 绝对无法绕过”。

### 3. 信封

字段命名向 A2A 靠拢，但 M0 不追求兼容。

```go
type Envelope struct {
    ID              string       // ULID
    RootID          string
    ParentID        string       // 触发本信封的上一条；人类发起为空
    OriginSessionID string       // ask 发起时的 origin session，reply 必须回到这里
    OriginRunID     string
    Channel         string
    ChatID          string
    ThreadID        string
    From            string       // bot id
    To              string       // bot id
    Gen             int
    Kind            string       // ask | reply | handoff | notify
    Mode            string       // queue | steer
    IdempotencyKey  string
    Deadline        *time.Time
    Body            string
    Artifacts       []ArtifactRef
    PromptMode      string       // full | minimal | none
    Echo            string       // full | summary | off
    CreatedAt       time.Time
    State           string       // pending | running | done | failed | timeout | canceled
    Error           string
}
```

关键语义：

- **只有 `ask` 和 M3 的 `dispatch` 子任务自动回投结果。** `reply`、`notify`、`handoff` 不自动回投，否则一问一答会变成无限乒乓，只能靠代数上限兜底。
- `handoff` 表示“交棒，不需要回复”；`notify` 表示“告知，不期待动作”。
- `reply` 必须带 `OriginSessionID`，由 Hub 投回 origin session，而不是新开一轮 per-root 会话。
- `idempotency_key` 默认 `hash(from, to, root, parent, kind, body)`，允许调用方显式覆盖。不要只 hash 正文，合法重复会误去重。
- `deadline` 到期后，Hub 向 origin session 投一条 timeout reply，并终止该信封对应的运行（如果还在排队）。
- `mode=steer` 复用目标 Bot 的 steering 能力，但必须绕过“只认 active session + 管理员”的现有逻辑。

### 4. 会话模型

#### 4.1 目标 Bot：detached per-root 会话

目标 Bot 为每个 `(root, target bot)` 创建一个独立会话：

- 名称：`relay:<root 前 8 位>`。
- 与人类当前活跃会话同一个 `SessionKey`（Bot + 平台 + chat + thread + agent），但**不设为 active**。
- 会话 ID 由 Hub 持久化；daemon 重启后优先复用，找不到再创建。
- 多个信封属于同一个 root 时，复用同一个目标会话，保留目标 Bot 的上下文。

实现上不需要给 `SessionKey` 加 root 维度，也不要把 root 塞进 `ThreadID`。后者会让 `replyTarget` 误判成线程回复（`internal/core/router.go:1147`）。给 `SessionStore` 增加：

```go
func (s *SessionStore) CreateDetached(key SessionKey, name string) (Session, error)
func (s *SessionStore) FindByName(key SessionKey, name string) (Session, bool)
```

`CreateDetached` 创建会话但不修改 `active`，因此：

- 人类消息仍然进原来的活跃会话；
- `/list` 能看到 relay 会话（现有 `List` 按 key 列出全部会话）；
- 人类可以 `/switch` 进去接管，符合 v1 的预期；
- 不需要改 `sessions.json` 的 schema。

#### 4.2 发件 Bot：reply 回到 origin session

这是 v2 相对 v1 最重要的修正。

`ask` 发出时，Hub 记录 `OriginSessionID`。目标完成后，Hub 调用发件 Bot 的：

```go
func (r *Router) ExecuteRelay(ctx context.Context, envelope Envelope) (RunResult, error)
```

其中 `sessionID = envelope.OriginSessionID`，走与 `ExecuteScheduled` 相同的入队路径：

- 如果 origin session 正在运行，reply 排在它后面，不会插入当前轮；
- 如果 origin session 空闲，直接开始新的一轮；
- 不新建会话，不新建 worktree，engine session 继续 resume；
- 因此 coder 能看到自己刚才的改动和上下文。

如果 origin session 已被归档或删除：

- 在同一个 lane 创建 detached 恢复会话 `relay-recovery:<root 前 8 位>`；
- 把 root 的 `origin_prompt` 和当前 reply 一起注入；
- 在卡片/日志里标注“origin session 已不可用，已恢复为新会话”。

#### 4.3 为什么 `--wait` 要单独设计

`--wait` 不是把 reply 当一个普通 queued turn 投回 origin session。那样会形成队列自死锁：

```text
origin session S0 的队列槽被 A 占用
A 在槽内等待 B
B 的 reply 作为普通 turn 排到 S0 后面
A 等 B，B 的 reply 等 A
```

即使 worktree 不同，这个死锁仍然存在。M2 的 `--wait` 应该是：

- CLI 在 socket 请求里等待 Hub 的 future；
- Hub 调用目标 `ExecuteRelay`，拿到最终回答后直接返回给 CLI；
- origin agent 的当前轮继续执行，不需要被 reply 唤醒；
- 如果 origin 与目标共享同一个串行 workspace，直接返回 `ERR_DEADLOCK`，提示改用异步。

### 5. 工作区与产物交接

#### 5.1 M0 的推荐配置

主场景 `coder -> reviewer -> coder` 在 M0 使用：

```toml
[bots.coder]
workspace = "~/code/project"
workspace_mode = "shared"

[bots.reviewer]
workspace = "~/code/project"
workspace_mode = "shared"
```

这样：

- 两个 Bot 看到同一个工作目录；
- coder 的未提交改动对 reviewer 可见；
- 进程级 workspace lock 会串行化同一 chat/thread 的运行；
- reviewer 读 `git diff` 不需要任何额外产物机制；
- origin session 回到自己的 worktree/共享目录，继续改代码。

**不要在这个场景推荐 `auto`。** coder 在 `auto` 下会进入自己的 worktree，reviewer 的 `auto` 会从 `HEAD` 再建一个，两者互相看不见。

#### 5.2 后续的 worktree + artifact handoff

当确实需要并行或隔离时，使用 artifact 复制，而不是传路径：

- Hub 在发送时把附件/产物复制到 `~/.nectar/relay/artifacts/<envelope-id>/`，记录 `sha256`、大小、文件名。
- 目标 Bot 运行前，Hub 把产物 materialize 到目标 workspace 的 `.nectar/relay/<envelope-id>/`。
- 注入 `NECTAR_RELAY_ARTIFACTS=<absolute path>`；prompt 里只给这个目录下的相对路径。
- 运行结束后保留到 root TTL 过期，便于审计和重试；由清理任务删除。
- 对代码审查，M0 可以直接在 `ask` 正文里放 `git diff`；大 diff 用 artifact。
- 对写作，正文放草稿或 artifact；不要写“请读 ~/code/project/xxx”，因为那可能是源 Bot 的 worktree。

产物路径必须做 jail：目标只能读 materialize 后的目录，不能通过 `..` 或绝对路径跳出。

### 6. 传输与可见性

#### 6.1 总线为真，IM 为镜

- 总线：Hub 校验 -> 追加 journal -> 入目标 Router 队列。
- 镜像：Hub 通过源 Bot 的平台 adapter 往群里发一条带标记的消息，只给人看，不参与触发。
- 顺序：先落盘，再发 mirror 拿 `message_id`，再入队目标；这样目标卡片可以挂在 mirror 下面。mirror 失败不阻塞总线，只是少一条可见消息。
- 目标运行结果仍然通过目标 Bot 的平台 adapter 输出；`echo` 控制输出量。
- **异步硬规则**：源 Bot 发完 `relay send` 必须结束当前回合，不能轮询 `relay roots/show`、sleep 或等待目标。否则 reply 会排在源 Bot 当前回合后面，形成队列自死锁。reply 会作为新回合唤醒 origin session。
- mirror 只展示“谁转交给了谁 + 一行任务摘要 + 等待回复”，不展示完整 prompt 或原始 marker；marker 用不可见 Unicode 编码，仅用于入站丢弃。

#### 6.2 `echo` 三档

| 值 | 目标 Bot 进度卡 | 目标最终回答 | mirror | 发件方 origin 轮 |
| --- | --- | --- | --- | --- |
| `full` | 显示 | 显示 | 显示 | 正常显示 |
| `summary` | 不显示 | 显示 | 显示一行 | 正常显示 |
| `off` | 不显示 | 不显示 | 不显示 | 正常显示 |

M0 默认建议 `summary`。原因是自动回投会让一次 ask 至少产生“目标最终回答 + 发件方 origin 轮”两块内容，`full` 再叠加目标进度卡，群里会很吵。`full` 留给演示和排障。

`echo=off` 不是“整个链路静默”：发件方 origin 轮是它自己的正常会话，仍然会显示。这一点要在文档里写清楚。

#### 6.3 mirror 去重

飞书 SDK 只过滤 Bot 自己，不按 `sender_type` 过滤其他 Bot。如果飞书把兄弟 Bot 的 mirror 消息投递过来，它会进入目标 Router，可能重复触发。必须做两层防护：

1. **身份优先**：目标 Bot 在入站前读取 Feishu 原始事件的 `sender_type=bot`，直接丢弃兄弟 Bot 的 mirror/card。
2. **不可见标记兜底**：正文带零宽 Unicode marker；在没有 sender_type 的平台或旧消息上，在 `anchors.Set` 和命令解析之前丢弃。

只在 prompt 里写“不要响应”不算防护。

#### 6.4 飞书 @

- `platform.Reply` 增加 `Mentions []platform.Mention`（或等价的 adapter 方法）。
- 飞书 adapter 的 `Send` 把 `Mentions` 传给 `channel.SendInput.Mentions`；SDK 支持这个字段。
- open_id 是 app 维度的：源 Bot 需要从自己的 app 视角查询群成员并缓存目标 Bot 的 open_id。
- 查不到时降级为纯文本 `@name`；mirror 只是可见性，不影响总线。
- 需要 `im:chat:readonly` / 群成员读取权限；没有权限时用配置 roster 里的名字兜底。

### 7. 防环路与预算

| 层 | 机制 | 默认值 |
| --- | --- | --- |
| 1 代数上限 | `gen` 超过上限就拒绝，返回 `ERR_GEN_LIMIT` | `max_generation = 4` |
| 2 根任务信封预算 | 每个 root 最多多少条信封 | `max_envelopes_per_root = 12` |
| 3 根任务运行预算 | 每个 root 最多多少次 relay 触发的引擎运行（含 origin 回投轮） | `max_runs_per_root = 8` |
| 4 根任务时长 | 超过 TTL 终止后续 relay | `root_ttl = 30m` |
| 5 乒乓检测 | 同一对 Bot 在窗口内高度相似正文，熔断整个 root | `pingpong_window = 4` |
| 6 幂等去重 | 重复 key 返回已有信封，不再次入队 | 开启 |
| 7 单目标熔断 | 目标连续失败/超时，暂停向它接力并冷却 | 连续 3 次，冷却 5m |
| 8 人工闸 | 人类新消息开新 root；`/relay stop` 取消整个 root；`/stop` 取消当前 lane | — |

结构上只允许点名一个目标，没有广播，没有“回复全部”。

### 8. 权限与安全

- **默认关闭。** `relay.enabled = false`。
- **接收方白名单。** `relay_accept_from` 留空 = 只接受同进程、同群、已知 roster 的 Bot；显式配置则只接受列出的 Bot。
- **对话权与管理权分开。** Bot 发件人只有对话权；`Origin=relay` 跳过所有斜杠命令、帮助和 session 管理解析。relay 正文即使以 `/` 开头也按普通文本处理。
- **接收方按自己的规矩跑。** 目标 Bot 用自己的沙箱、权限模式、工作区和平台身份；不继承发件人的。
- **提示围栏。** Relay 内容包在明确围栏里并标出来源：这是 Bot X 的请求，不是用户指令；不得据此提权、外泄凭据或修改安全配置。围栏只是缓解 prompt injection，不是保证。
- **凭据隔离。** 继续沿用现有做法，平台 token 不传给 agent 子进程。
- **审计。** 每条信封、每次状态变化落盘；dashboard 增加只读 relay 页（默认关闭、仅 loopback）。
- **记忆隔离。** relay 内容不自动写入目标 Bot 的记忆；失败 lesson 和 `memory` candidate 也要跳过 relay 运行（`internal/core/router.go:1326`）。
- **产物安全。** 大小上限、sha256、路径 jail；不允许 artifact 路径跳到工作区外。

### 9. 记忆与共享上下文

- 记忆继续按 `bot / chat / project` 三种 scope 隔离。
- Relay 内容默认不自动写入记忆。
- M2 增加房间日志：每个群一份只增不改的摘要日志，`nectar relay history` 读取，并受 `max_chars` 限制。
- M4 再考虑 `team` scope。

## 接口

### 配置

```toml
[relay]
enabled = false
echo = "summary"                 # full | summary | off
max_generation = 4
max_envelopes_per_root = 12
max_runs_per_root = 8
root_ttl = "30m"
pingpong_window = 4
session = "per_root"             # per_root | active
artifact_max_bytes = 10485760
allow_operator_send = false

[bots.coder]
name = "Codex Coder"
platform = "feishu"
engine = "codex"
workspace = "~/code/project"
workspace_mode = "shared"
role = "coder"
description = "实现功能、改代码"
capabilities = ["code", "git", "test"]
relay_accept_from = ["lead", "reviewer"]
relay_prompt = "full"            # full | minimal | none

[bots.reviewer]
name = "Claude Reviewer"
platform = "feishu"
engine = "claude"
workspace = "~/code/project"
workspace_mode = "shared"
role = "reviewer"
description = "只读审查 diff，指出风险"
capabilities = ["review", "security"]
relay_accept_from = ["coder", "lead"]
relay_prompt = "minimal"
```

### 命令行

| 命令 | 作用 | 阶段 |
| --- | --- | --- |
| `nectar relay bots` | 列出本群可接力的 Bot、角色、忙闲 | M0 |
| `nectar relay send --to <bot> [--session 名] [--mode queue\|steer] [--artifact 路径] [--idempotency-key K] "正文"` | 异步发信封，立即返回信封 id | M0 |
| `nectar relay ls` · `show <root>` · `cancel <root>` | 查看和取消根任务 | M1 |
| `nectar relay send --wait [--timeout 10m]` | 同步等待，直接拿到目标最终回答 | M2 |
| `nectar relay history [--root ID] [--limit N]` | 读房间日志 | M2 |
| `nectar relay dispatch` · `retry <task>` | 派活与重试 | M3 |
| `nectar relay artifacts <root>` | 查看根任务产物 | M2 |

CLI 继续复用 `NECTAR_API_SOCKET`；`--bot` / `--socket` 的解析方式与 `nectar schedule` 一致。

### 聊天命令

仅 `admin_users` 可用：

- `/relay status`：本群当前根任务、预算用量、目标忙闲。
- `/relay stop`：取消当前 root 下排队和运行中的 relay。
- `/relay on` / `/relay off`：按群开关（M1）。
- `/relay sessions`：列出当前 root 的 origin/target 会话（M1）。

### API

挂载在每个 Bot 的 `ScheduleAPI` 上：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/relay/bots` | 花名册、忙闲、可接受来源 |
| `POST` | `/relay/send` | 发信封；agent 用 Bearer token，操作员用 `allow_operator_send` |
| `GET` | `/relay/roots` | 根任务列表 |
| `GET` | `/relay/roots/{id}` | 根任务详情 |
| `POST` | `/relay/roots/{id}/cancel` | 取消根任务 |
| `GET` | `/relay/history` | 房间日志（M2） |
| `POST` | `/relay/dispatch` | 派活（M3） |

`POST /relay/send` 的服务端只信任 token 绑定的身份。请求体里的 `from`、`gen`、`chat` 一律忽略或校验后丢弃。

错误码：

- `ERR_GEN_LIMIT`
- `ERR_BUDGET`
- `ERR_TARGET_UNKNOWN`
- `ERR_NOT_IN_CHAT`
- `ERR_REJECTED`
- `ERR_DEADLOCK`
- `ERR_IDEMPOTENT`

幂等命中时返回已有信封和状态，HTTP 200，不产生第二次运行。

### Agent skill：`nectar-relay`

与 `nectar-memory`、`nectar-schedule` 一样由 `nectar skills install` 安装。Skill 要教四件事：

1. 什么时候该接力：自己不擅长、需要独立审查、需要另一个引擎的视角；不要出于礼貌接力。
2. 正文必须自包含：目标、约束、期望产出、相关 diff/草稿；不要假设对方看过上下文。
3. 产物用正文或 `--artifact`，不要传源 worktree 的绝对路径。
4. 收到 `reply` 后只在确有必要时再接力；遇到 `ERR_GEN_LIMIT`、`ERR_BUDGET` 等错误就停下来向人汇报，不要换个说法重试。

`relay_prompt=none` 只代表 Nectar 不注入身份、peers、记忆和技能说明；引擎自己的 `AGENTS.md` / skills 仍然可能被加载。文档不要把它写成“绝对零注入”。

## 分阶段落地

### M0：异步可见接力

交付：

- `[relay]` 与 Bot 的 `role` / `description` / `capabilities` / `relay_accept_from` / `relay_prompt` 配置。
- 进程内 RelayHub、journal、token、roster。
- `nectar relay send` / `relay bots`。
- 目标 detached per-root 会话；reply 回 origin session。
- `echo=summary/off`；飞书移交卡 + review 结果卡；`sender_type=bot` 过滤 + 不可见 marker 去重。
- 代数上限、幂等、根任务基本预算。
- 假引擎测试脚本。

验收：

1. 群里 @coder，coder relay 给 reviewer；reviewer 看到 coder 的未提交 diff 并给出审查。
2. reviewer 的回答回到 coder 的 origin session；coder 能在原 worktree 继续改代码。
3. 构造两个 Bot 互相 ask 的提示，第 4 跳被拒，agent 收到 `ERR_GEN_LIMIT`。
4. 同一 `idempotency_key` 重发不产生第二次运行。
5. daemon 重启后，未处理的信封继续投递；运行中的信封标记 failed，不自动重跑。
6. `/relay stop` 之后该 root 不再产生新运行。
7. mirror 消息不会触发目标 Bot 的第二次运行（模拟兄弟 Bot 入站）。

规模：M–L。v1 估 M 偏乐观，主要成本在会话/产物/镜像三块。

### M1：防护与可观测

- 根任务预算完整化、乒乓检测、单目标熔断。
- `relay_accept_from`、提示围栏、Bot 发件人命令隔离。
- `relay ls/show/cancel`、`/relay status/stop/on/off`。
- dashboard relay 页。
- `echo=full`。

验收：预算耗尽时 root 被终止并给出原因；正文伪造 `/cron add` 不会创建任务；不在白名单的 Bot 被拒并留审计。

### M2：同步等待、房间日志、跨平台镜像

- `--wait` 走 socket 直接返回结果，不产生 origin 的额外 turn。
- 共享串行 workspace 时拒绝 `--wait` 或返回 `ERR_DEADLOCK`。
- `--mode steer`，目标是指定 session 而不是 active session。
- 房间日志与 `relay history`。
- 产物 materialize、artifact 列表与清理。
- 钉钉、Telegram 的 mirror 适配。

验收：`--wait` 直接拿到最终回答；共享 workspace 不卡死；M0 场景在三个平台跑通（镜像层）。

### M3：派活编排

- `relay dispatch` / `retry`。
- 团队记录与原地刷新的团队卡。
- `--after` 依赖。
- worker 零注入；worker 建议 `workspace_mode=auto` + artifact handoff。
- 失败、超时、重派。

验收：lead 派 coder 与 reviewer 两个任务，团队卡随状态刷新；中途杀 coder，lead 收到 error 并能重派；lead 会话被压缩/重启后任务状态仍在。

### M4：跨进程（可选）

- 带 Bearer token 的 HTTP peers。
- 跨实例 `ask/reply`。
- 记忆 `team` scope。

## 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| **origin session 丢失**：回复回不去，或回到空上下文 | root 保存 origin prompt；找不到时创建 recovery 会话并注入 brief |
| **worktree 隔离导致目标看不到产物** | M0 用 `shared`；后续 artifact 复制；禁止只传路径 |
| **`--wait` 队列/工作区死锁** | `--wait` 直接返回结果；共享 workspace 拒绝；超时和 deadlock 检测 |
| **mirror 重复触发** | Feishu `sender_type=bot` 过滤 + 不可见 marker，入站最早期丢弃 |
| **成本放大** | 代数上限、信封/运行预算、TTL、乒乓检测、`handoff` |
| **群里噪声** | 默认 `summary`；M3 只保留一张团队卡 |
| **跨 Bot 提示注入** | 围栏只是缓解；真正的边界是命令隔离、接收方沙箱、预算和权限 |
| **重启恢复** | 未处理信封重投；运行中信封 failed，不自动重跑 |
| **飞书 open_id 权限** | 查群成员并缓存；失败降级纯文本 @，不影响总线 |
| **同用户 socket 信任** | 0600 + token；明确它不是对抗同用户恶意进程的边界 |
| **journal 膨胀** | 按 root TTL 清理；artifact 同生命周期清理 |

## 与 v1 的差异

| v1 | v2 | 原因 |
| --- | --- | --- |
| 一个 daemon socket | 每个 Bot 自己的 socket + 进程内 Hub | 当前架构就是每 Bot 一个 `api.sock` |
| 发件 Bot 被唤醒新开一轮 | reply 回到 origin session | 避免丢 worktree 和引擎上下文 |
| 目标用 per-root 会话 | 保留；用 detached session 实现 | 不劫持人类活跃会话，也不改 `SessionKey` |
| 传产物路径 | 内联内容或 artifact 复制 | worktree 之间路径不通用 |
| M0 示例用 `auto` | M0 示例用 `shared` | 主场景要看到未提交 diff |
| `--wait` 只考虑 workspace 死锁 | 还有队列自死锁；改为直接返回 | 避免 origin 队列自等 |
| `echo` 默认 `full` | 默认 `summary` | 自动回投已经产生两块内容，减少噪声 |
| 幂等默认 hash 正文 | 加 parent/gen/kind | 避免合法重复误去重 |
| per-root 会话“可在 /list /switch” | 用 detached session 兑现 | `Create` 会设为 active，不能直接用 |
| `relay_prompt=none` 绝对零注入 | 仅 Nectar 不注入 | 引擎自己的 AGENTS.md/skills 仍会加载 |
| M0 规模 M | M–L | 会话、产物、镜像、静默模式都是实打实的工作 |

## 需要拍板的决策

1. **`echo` 默认 `summary` 还是 `full`？** 建议 `summary`，演示时再切 `full`。
2. **自动回投只对 `ask`？** 建议是；`reply/notify/handoff` 不自动回投。
3. **M0 用 `shared` 还是 `worktree`？** 建议 `shared`，先把链路跑通。
4. **重启时运行中的信封是否自动重跑？** 建议不重跑，标记 failed，由发件方决定 `retry`。
5. **`allow_operator_send` 是否默认开启？** 建议默认关闭。
6. **M0 是否只做飞书镜像？** 建议是；总线与平台无关，镜像层后续补。

## 实现映射

| 文件/目录 | 变更 |
| --- | --- |
| `config/config.go`、`config/bots.go` | `[relay]`、Bot 的 `role/description/capabilities/relay_*` |
| `internal/core/relay_types.go`、`relay_router.go`、`relay_api.go`（新增） | Envelope、Hub、token、journal、目标执行、HTTP API |
| `internal/core/router.go` | `ExecuteRelay`、`RunResult`、quiet/echo、relay env、`Origin=relay` 跳过命令、`/stop` 根任务 |
| `internal/core/session.go` | `CreateDetached`、`FindByName` |
| `internal/core/schedule_api.go` | 挂 `/relay/*`；relay 开启时即使 scheduler/memory 都关也启动 |
| `main.go` | 创建 RelayHub、注册 runtime、注入 Router/API |
| `internal/cli/relay.go`（新增） | `relay` 子命令 |
| `internal/cli/run.go` | 命令分发 |
| `internal/skills/install.go`、`internal/skills/assets/nectar-relay/` | 安装 relay skill |
| `internal/platform/platform.go` | `Origin=relay`、`Reply.Mentions` |
| `internal/platform/feishu/adapter.go` | mirror 发送、mention、marker/身份丢弃 |
| `internal/dashboard/` | relay 只读页 |
| 测试 | 假引擎脚本覆盖 gen、幂等、取消、重启、mirror 丢弃、origin 回投 |

## 来源

- 当前仓库源码与 v0.10.1 文档。
- v1《nectar 多 bot 协作方案》中对 cc-connect、metabot、botmux、lark-multi-cli-bridge、telegram-ai-bridge 的对比。
- 本文中的实现细节均以当前代码为准；未验证项已在文中标注。
