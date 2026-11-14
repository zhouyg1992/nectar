# 配置参考

配置文件默认位于 `~/.nectar/config.toml`。除所选平台的凭证外，所有配置都有内置默认值，可以按需覆盖。

安装器会写入一份最小可用配置，正常安装后不需要再手动补全。

## 安装器写入什么

| 配置 | 是否写入 | 说明 |
|---|---|---|
| `bot.engine` | 是 | 安装时选择的引擎 |
| `bot.platform` | 是 | 安装时选择的平台 |
| `[feishu]` 或 `[dingtalk]` 凭证 | 是 | 安装时填写 |
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

如果只配置了一个平台，`bot.platform` 可以省略，Nectar 会根据凭证自动判断；两个平台都配置时必须显式设置。

## 安全配置

```toml
[bot]
admin_users = ["ou_xxx"]
allowed_chats = ["oc_xxx"]
```

- `admin_users`：允许管理 `/cron` 和定时任务 API 的用户。为空时不做限制。
- `allowed_chats`：允许使用 bot 的群聊。为空时不限制群聊。
- 飞书使用 `open_id` / `chat_id`；钉钉使用 `staffId` / `openConversationId`。

配置后重启服务：

```bash
nectar service restart
```

## 机器人

```toml
[bot]
name = "Nectar"          # 默认 Nectar
engine = "codex"         # codex | opencode | cursor | claude | pi
# platform = "feishu"    # feishu | dingtalk；单平台可省略
admin_users = []
allowed_chats = []
```

## 引擎配置

以下各段都可以整段省略，省略时使用默认值。

### Codex

```toml
[codex]
path = "codex"
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
standalone = true             # 每次运行使用私有 server
auto_approve = true           # 非交互 bot 场景需要
per_chat_workspace = false
```

`model` 为空时，Nectar 会尝试从 `~/.config/opencode/opencode.json` 读取默认模型。上下文上限来自模型的 `limit.context`；模型未在 OpenCode 配置中声明时，可用 `context_window` 手动指定。

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

## 环境变量

环境变量会覆盖配置文件中的对应值：

```bash
export FEISHU_APP_ID=cli_xxx
export FEISHU_APP_SECRET=xxx
export DINGTALK_CLIENT_ID=dingxxx
export DINGTALK_CLIENT_SECRET=xxx

export CODEX_PATH=codex
export OPENCODE_PATH=opencode
export CURSOR_AGENT_PATH=agent
export CLAUDE_PATH=claude
export PI_PATH=pi

export NECTAR_ENGINE=codex
export NECTAR_DATA_DIR=~/.nectar
export NECTAR_WORKING_DIR=~/.nectar/workspace
```

服务模式从配置文件读取平台凭证，不继承 shell 里的 `FEISHU_*` / `DINGTALK_*` 变量。

## 上下文占比

卡片底部的上下文占比依赖引擎返回的 token 用量和上下文上限：

- Codex：`context_window = 0` 时从 Codex 模型目录解析。
- OpenCode：从模型的 `limit.context` 读取。
- Claude Code：从最终 `result` 事件读取。
- Cursor、Pi：默认未知，需要时显式设置 `context_window`。

解析不到时，卡片底部会省略上下文占比，不影响 Agent 执行。

返回：[中文 README](../../README.zh-CN.md) · [引擎准备](engines.md) · [命令参考](commands.md)
