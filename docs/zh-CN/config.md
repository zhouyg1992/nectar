# 配置参考

配置文件默认位于 `~/.nectar/config.toml`。除所选平台的凭证外，所有配置都有内置默认值，可以按需覆盖。

安装器会写入一份最小可用配置，正常安装后不需要再手动补全。

## 安装器写入什么

| 配置 | 是否写入 | 说明 |
|---|---|---|
| `bot.engine` | 是 | 安装时选择的引擎 |
| `bot.platform` | 是 | 安装时选择的平台 |
| `[feishu]`、`[dingtalk]` 或 `[telegram]` 凭证 | 是 | 安装时填写 |
| 引擎参数（`[codex]` 等） | 否 | 使用内置默认值 |
| `[schedule]` | 否 | 默认开启，使用内置默认值 |
| `bot.admin_users` | 否 | 为空时不限制管理员 |
| `bot.allowed_chats` | 否 | 为空时不限制群聊 |

## 最小配置

飞书 / Lark：

```toml
[bot]
engine = "codex"
platform = "feishu"

[feishu]
app_id = "cli_xxx"
app_secret = "xxx"
```

钉钉：

```toml
[bot]
engine = "codex"
platform = "dingtalk"

[dingtalk]
client_id = "dingxxx"
client_secret = "xxx"
```

Telegram：

```toml
[bot]
engine = "codex"
platform = "telegram"

[telegram]
bot_token = "your-bot-token"
```

如果只配置了一个平台，`bot.platform` 可以省略，Nectar 会根据凭证自动判断；多个平台都配置时必须显式设置。

## 安全配置

```toml
[bot]
admin_users = ["ou_xxx"]
allowed_chats = ["oc_xxx"]
```

- `admin_users`：允许管理 `/cron` 和定时任务 API 的用户。为空时不做限制。
- `allowed_chats`：允许使用 bot 的群聊。为空时不限制群聊。
- 飞书使用 `open_id` / `chat_id`；钉钉使用 `staffId` / `openConversationId`；Telegram 使用数字 `user_id` / `chat_id`。

配置后重启服务：

```bash
nectar service restart
```

## 机器人

```toml
[bot]
name = "Nectar"          # 默认 Nectar
engine = "codex"         # codex | opencode | cursor | claude | pi
# platform = "feishu"    # feishu | dingtalk | telegram；单平台可省略
admin_users = []
allowed_chats = []
# shared | per_chat | per_session | worktree | auto
workspace_mode = "shared"
```

## 多 Bot

一个 Nectar 进程可以同时运行多个 Bot。每个 Bot 有独立的平台身份、引擎、工作区、会话、记忆和权限；在同一个群里分别 @ 对应 Bot 即可。

```toml
[bots.codex_dev]
name = "Codex Dev"
platform = "feishu"
engine = "codex"
workspace = "~/code/project-a"
workspace_mode = "worktree"

[bots.codex_dev.feishu]
app_id = "cli_xxx"
app_secret = "xxx"

[bots.claude_review]
name = "Claude Review"
platform = "feishu"
engine = "claude"
workspace = "~/code/project-a"

[bots.claude_review.feishu]
app_id = "cli_yyy"
app_secret = "yyy"
```

规则：

- 每个 Bot 必须使用独立的平台应用/token；重复凭据会启动失败，避免同一条消息被两个 Bot 重复处理。
- `[bots.<id>]` 默认使用 `~/.nectar/bots/<id>` 作为数据目录；可用 `data_dir` 覆盖。再增加或删除 Bot 不会改变已有 Bot 的默认路径。旧 `[bot]` 仍使用 `storage.data_dir`。
- 同一个 Bot 的不同 chat 默认并发执行，同一个 chat 内按会话串行；单聊和群聊不会互相等待。
- 新 Bot 默认 `workspace_mode = "auto"`：工作区是已提交的 Git 仓库时为每个 session 创建独立 worktree，否则回退到 `per_chat`。
- `workspace = "..."` 覆盖该 Bot 的引擎工作目录；`model = "..."` 覆盖该 Bot 当前引擎的模型。
- 省略 `engine` 时继承 `[bot].engine`；如果连旧 `[bot].engine` 也没有配置，则默认 `codex`。
- 旧 `[bot]` + 全局 `[feishu]` / `[codex]` 配置继续可用；新增 `[bots.*]` 时会同时启动旧默认 Bot 和新增 Bot。

`workspace_mode` 可选 `shared`、`per_chat`、`per_session`、`worktree`、`auto`。`worktree` 会从仓库当前 `HEAD` 创建 `nectar/<bot>/<session>` 分支和独立目录，适合多个 Agent 并行修改同一个仓库；worktree 目录在 `~/.nectar/worktrees/<bot>/<session>`。

`shared` 只表示所有 chat 共用同一个目录，不表示所有任务串行；不同 chat 仍可并发，同一个 chat 内按会话串行。如果两个 chat 会修改同一批文件，请使用 `worktree`。

worktree 只包含已提交的 `HEAD`，主工作区里未提交的改动不会自动带进去；需要先提交或在主工作区继续处理。

worktree 会按 session 复用。清理时可执行 `git -C <repo> worktree list` 查看，再用 `git -C <repo> worktree remove <path>` 删除。

`per_chat` / `per_session` 会让多个 Agent 同时操作同一个目录，适合读取、分析和互不重叠的任务；要并行修改同一批文件，请使用 `worktree`。

## 引擎配置

以下各段都可以整段省略，省略时使用默认值。

### Codex

```toml
[codex]
path = "codex"
backend = "app-server"         # app-server | exec；默认支持 /steer
model = ""                    # 空则使用 Codex 自身配置的模型
effort = ""                   # 例如 high，透传为 model_reasoning_effort
sandbox = "workspace-write"   # read-only / workspace-write / danger-full-access
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 从 Codex 模型目录自动解析
per_chat_workspace = false
bypass_approvals = false      # 谨慎开启
```

默认使用 `backend = "app-server"` 时，`max_output_bytes` 限制单条 JSON-RPC 事件；整个 turn 的 stdout 会持续流式处理，不受该总量的限制。使用 `backend = "exec"` 时，它仍限制总 JSONL 输出。

### OpenCode

```toml
[opencode]
path = "opencode"
model = ""                    # 例如 provider/model#variant
agent = ""                    # 可选 OpenCode agent
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 从 OpenCode 配置自动解析
standalone = false            # 默认使用共享 server，支持 /steer
server_url = ""               # 可选：run/export/steer 共用此 server，并忽略 standalone
auto_approve = true           # 非交互 bot 场景需要
per_chat_workspace = false
```

`model` 为空时，Nectar 会尝试从 `~/.config/opencode/opencode.json` 读取默认模型。上下文上限来自模型的 `limit.context`；模型未在 OpenCode 配置中声明时，可用 `context_window` 手动指定。

OpenCode 默认使用共享 server，插话通过 `delivery = "steer"` 实现。设置 `server_url` 后，普通运行、usage 和插话都会指向同一个 server，并忽略 `standalone`。只有 `standalone = true` 且没有 `server_url` 时才使用私有 server，此时 `/steer` 不可用。

### Cursor Agent

```toml
[cursor]
path = "agent"
model = ""                    # 例如 composer-2.5；空则使用 Cursor 默认模型
mode = ""                     # plan / ask；空为普通 Agent 模式
sandbox = ""                  # enabled / disabled；空则使用 Cursor 默认
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 未知，卡片底部省略上下文占比
auto_approve = true           # 等价 -f
trust_workspace = true        # 等价 --trust
per_chat_workspace = false
```

`path` 默认是 `agent`；找不到时会回退 `cursor-agent`。需要底部上下文占比时显式设置 `context_window`。

### Claude Code

```toml
[claude]
path = "claude"
backend = "stream-json"       # stream-json | print；默认支持 /steer
model = ""                    # 例如 claude-sonnet-4-5；空则使用 Claude Code 默认模型
fallback_model = ""           # 可选备用模型
permission_mode = ""          # acceptEdits | plan | bypassPermissions | manual | dontAsk | auto
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 从 result 事件自动读取
max_turns = 0                 # 0 = 不限制
auto_approve = true           # 等价 --dangerously-skip-permissions
per_chat_workspace = false
```

token 用量和上下文上限从最终 `result` 事件读取，`context_window` 仅用于手动覆盖。

### Pi

```toml
[pi]
path = "pi"
backend = "rpc"               # rpc | json；默认支持 /steer
provider = ""                 # 可选 provider 覆盖
model = ""                    # 例如 claude-sonnet-4-5；空则使用 Pi 默认模型
thinking = ""                 # off | minimal | low | medium | high | xhigh | max
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 未知，卡片底部省略上下文占比
trust_workspace = true        # 等价 --approve
per_chat_workspace = false
```

`trust_workspace = true` 允许 Pi 加载项目内的设置、skills、上下文文件和扩展。

## 存储

```toml
[storage]
data_dir = "~/.nectar"
```

`data_dir` 下会保存会话、定时任务、工作区、附件和 Skill 备份等运行数据。

## 记忆

```toml
[memory]
enabled = true                # false 时关闭记忆存储、命令和注入
default_scope = "bot"         # bot | chat | project；bot = 同一个 Bot 的所有 chat 共享
max_items = 6                 # 每轮最多注入多少条记忆
max_chars = 1500              # 每轮记忆上下文的最大字符数
auto_learn_failures = true    # 失败任务记录为 pending 候选，审核后才注入
rule_extract = false          # 仅提取显式的“记住/以后”类语句；默认关闭
agent_self_organize = true    # 允许 Agent Skill 在必要时自行搜索/写入记忆
feedback_enabled = true       # 记录 useful/harmful 反馈并参与排序
max_candidates_per_turn = 1   # 每轮规则提取的候选上限
```

记忆保存在 `~/.nectar/memory/memory.json`。默认 `/remember` 写入 bot scope，同一个 Bot 的单聊和群聊共享；设置 `default_scope = "chat"` 可退回按 chat 隔离，或用 `/remember chat <text>` 单次指定。`/remember project` 写入项目 scope。失败教训先进入 `candidate`，通过 `/memory approve <id>` 后才进入 `active` 并参与检索。

检索使用本地 BM25（中文按二元组切词），不依赖 embedding 或外部模型。`rule_extract` 默认关闭；开启后也只提取显式的“记住/以后/请始终/别再”类语句，问句、清单、验收条件和一次性任务会被跳过。项目事实建议用 `/remember project` 或 Agent Skill 写入。未审核的 candidate 14 天后会自动从待审核列表移入 archived。注入和显式反馈会写入 `~/.nectar/memory/feedback.jsonl`，用于排序和审计。

写入 Bot 级记忆需要管理员权限；`admin_users` 为空时不做限制。

Agent 通过 `nectar memory` CLI 读写记忆；`nectar skills install` 会同时安装 `nectar-memory` Skill 和 `AGENTS.md` 托管说明，让 Agent 能在任意语言下按自然语言意图调用。

记忆会记录 `locality`（portable / templated / device）、稳定的 `project_id`、revision 和 content hash。项目/主目录路径会转为 `${PROJECT_ROOT}` / `${HOME}`；其他绝对路径标记为 device，便于后续同步时避免跨机器失效。

## 定时任务

定时任务默认开启，不需要在配置里添加 `[schedule]`。以下是可选覆盖项：

```toml
[schedule]
enabled = true                # false 时关闭调度器和本地管理 socket
timezone = "Local"            # cron 默认时区，也可用 "Asia/Shanghai"
startup_grace = "5m"          # 重启后最多补跑多久以内的错过的任务
history_limit = 100           # 保留多少条已完成/失败记录；0 = 不限制
max_concurrent = 4            # 同时执行数；0 = 不限制
jitter = "0s"                 # 周期任务随机延迟上限
timeout = "30m"               # 单次运行默认超时；0s 关闭调度层超时
```

详细语义见 [定时任务](schedule.md)。

## 本地控制台

Dashboard 默认关闭，开启后只监听本机回环地址，并要求 Token 登录：

```toml
[dashboard]
enabled = true
listen = "127.0.0.1:8787"
# 默认 ~/.nectar/run/dashboard.token；首次启动时自动生成，权限 0600
token_file = "~/.nectar/run/dashboard.token"
# 允许监听非回环地址；开启前请自行加 TLS 反向代理
allow_remote = false
# true 时只允许查看，禁止修改记忆和定时任务
read_only = false
```

启动 Nectar 后运行：

```bash
nectar dashboard
```

命令会打印带一次性登录 Token 的 URL。控制台提供 Bot 状态总览、记忆检索与审核（批准、拒绝、恢复、批量操作、编辑时调整 scope）、定时任务管理和只读生效配置；平台凭证、引擎密钥等敏感字段不会返回给浏览器。控制台不修改配置文件，修改 `config.toml` 后仍需重启服务生效。

控制台界面默认英文，右上角的 Language 选择器可切换中文；选择会保存在当前浏览器。

`dashboard.listen` 默认只允许 `127.0.0.1` / `localhost` / 回环 IP。确需远程访问时设置 `allow_remote = true`，并只通过 HTTPS 反向代理暴露；Dashboard Token 等价于管理员密码，不要发到群聊或提交到仓库。

## 环境变量

环境变量会覆盖配置文件中的对应值：

```bash
export FEISHU_APP_ID=cli_xxx
export FEISHU_APP_SECRET=xxx
export DINGTALK_CLIENT_ID=dingxxx
export DINGTALK_CLIENT_SECRET=xxx
export TELEGRAM_BOT_TOKEN=your-bot-token
export TELEGRAM_PROXY=http://127.0.0.1:7890

export CODEX_PATH=codex
export OPENCODE_PATH=opencode
export CURSOR_AGENT_PATH=agent
export CLAUDE_PATH=claude
export PI_PATH=pi

export NECTAR_ENGINE=codex
export NECTAR_DATA_DIR=~/.nectar
export NECTAR_WORKING_DIR=~/.nectar/workspace
```

服务模式从配置文件读取平台凭证，不继承 shell 里的 `FEISHU_*` / `DINGTALK_*` / `TELEGRAM_*` 变量。

## 上下文占比

卡片底部的上下文占比依赖引擎返回的 token 用量和上下文上限：

- Codex：`context_window = 0` 时从 Codex 模型目录解析。
- OpenCode：从模型的 `limit.context` 读取。
- Claude Code：从最终 `result` 事件读取。
- Cursor、Pi：默认未知，需要时显式设置 `context_window`。

解析不到时，卡片底部会省略上下文占比，不影响 Agent 执行。

返回：[中文 README](../../README.zh-CN.md) · [引擎准备](engines.md) · [命令参考](commands.md)
