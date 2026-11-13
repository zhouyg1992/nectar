<h1 align="center">🍯 Nectar</h1>

<p align="center">
  <strong>Memory that grows with you.</strong><br>
  Go 编写的聊天应用 ↔ 编码 Agent 桥接器
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/zhouyg1992/nectar/releases"><img src="https://img.shields.io/github/v/release/zhouyg1992/nectar?display_name=tag&sort=semver" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zhouyg1992/nectar" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="macOS | Linux">
  <a href="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml"><img src="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

Nectar 把飞书或钉钉消息交给 Codex CLI、OpenCode、Cursor Agent、Claude Code 或 Pi 执行，再以交互卡片把结果回传。v1 支持飞书 / Lark、钉钉 + 上述任一引擎，安装后一条命令即可启动。

<p align="center">
  <img src="docs/assets/nectar-flow.svg" alt="飞书和钉钉消息经过 Nectar 转发给 Codex、OpenCode、Cursor Agent、Claude Code 或 Pi" width="860">
</p>

## 支持矩阵

**v1 支持：飞书 / Lark、钉钉 + Codex CLI / OpenCode / Cursor Agent / Claude Code / Pi。**

图例：✅ 已支持 · 🚧 计划中

### 平台

| 平台 | 状态 | 配置 | 说明 |
|---|---|---|---|
| 飞书 / Lark | ✅ 已支持 | [飞书配置](docs/zh-CN/feishu.md) | 长连接 + @Bot<br>卡片：🌼 Thinking → 🐝 Working → 🍯 Complete / ⚠️ Failed<br>底部：上下文占比、模型、耗时、token |
| 钉钉 | ✅ 已支持 | [钉钉配置](docs/zh-CN/dingtalk.md) | Stream 模式 + @Bot<br>A2UI 流式卡片<br>无需公网回调或卡片模板 |
| Telegram | 🚧 计划中 | — | Long Polling |
| 企业微信 / 微信 | 🚧 计划中 | — | 视平台能力适配 |
| Slack | 🚧 计划中 | — | Socket Mode |
| 更多平台 | 🚧 计划中 | — | 通过 `PlatformAdapter` 扩展 |

### 引擎

| 引擎 | 状态 | 说明 |
|---|---|---|
| Codex CLI | ✅ 已支持 | `exec --json` / `resume` |
| OpenCode | ✅ 已支持 | `run --format json` / `--session` |
| Cursor Agent CLI | ✅ 已支持 | `-p --output-format stream-json` / `--resume` |
| Claude Code | ✅ 已支持 | `-p --output-format stream-json` / `--resume` |
| Pi | ✅ 已支持 | `--mode json` / `--session` |
| Kimi Code | 🚧 计划中 | 本地 Server API 适配 |
| 更多引擎 | 🚧 计划中 | 通过 `EngineAdapter` 扩展 |

> 计划中的条目是方向，不代表具体排期；实际进度以 [Roadmap](docs/zh-CN/roadmap.md) 和 Releases 为准。

## 安装

macOS / Linux（amd64 / arm64）：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

安装脚本是交互式的，会：

- 自动识别操作系统和 CPU 架构，从 GitHub Releases 下载对应版本并校验 `checksums.txt` 里的 SHA-256
- 检测当前电脑已安装的引擎 CLI（`codex`、`opencode`、`agent` / `cursor-agent`、`claude`、`pi`），一个都没有时提示并退出
- 让你选择引擎和平台（默认飞书 / Lark，可选钉钉）
- 写入一份最小化的 `~/.nectar/config.toml`
- 默认安装并启动后台服务

如果 `~/.nectar/config.toml` 已存在，默认保留现有配置。

指定版本或安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --version v0.5.1
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --dir /usr/local/bin
```

建议先下载检查再执行：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh -o install.sh
sh install.sh
```

Windows 请使用 WSL，或从 Releases 手动下载。仓库必须是公开的，`curl | sh` 才能匿名访问。

非交互安装可以显式传入引擎、平台和凭证：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  FEISHU_APP_ID=cli_xxx FEISHU_APP_SECRET=xxx sh -s -- --engine codex --platform feishu
```

钉钉使用 `--platform dingtalk`，并通过 `DINGTALK_CLIENT_ID` / `DINGTALK_CLIENT_SECRET` 传入凭证。

用 `--no-service` 只安装二进制，用 `--reconfigure` 备份并替换现有配置，用 `--config PATH` 指定配置路径。Linux 额外加 `--linger` 可开机启动，无需登录。

卸载：

```bash
nectar service uninstall
rm -f ~/.local/bin/nectar
rm -rf ~/.nectar   # 可选：删除配置、数据、工作区和教训记录
```

如果安装时用了 `--dir` 或 `--config`，请替换成对应路径。

> 后台服务支持需要 v0.1.1 及以上版本。

## 前置条件

以下引擎**任选其一**安装并登录即可，不需要全部安装；安装脚本会检测已安装的引擎并让你选择。

### Codex CLI

安装 Codex CLI 并完成登录，确认下面的命令可以正常运行：

```bash
codex exec --json --skip-git-repo-check "reply with OK"
```

### OpenCode CLI

安装 OpenCode，配置好 provider/model，并确认下面的命令可以正常运行：

```bash
opencode run --format json "Reply with exactly OK"
```

### Cursor Agent CLI

安装 Cursor Agent：

```bash
curl https://cursor.com/install -fsS | bash
```

官方安装脚本会同时提供 `agent`（主命令）和 `cursor-agent`（兼容名）。Nectar 和安装脚本优先使用 `agent`，找不到时回退 `cursor-agent`，不需要手动创建软链接。

确保 `~/.local/bin` 在 `PATH` 中，然后确认下面的命令可以正常运行：

```bash
agent login
agent -p --trust --mode ask "Reply with exactly OK"
```

### Claude Code

安装并登录 Claude Code，确认下面的命令可以正常运行：

```bash
claude auth login
claude -p --permission-mode plan "Reply with exactly OK"
```

### Pi

安装并登录 Pi，确认下面的命令可以正常运行：

```bash
pi auth check --provider anthropic
pi --mode json "Reply with exactly OK"
```

## 配置

安装脚本会创建 `~/.nectar/config.toml`。从源码运行时可以手动复制：

```bash
mkdir -p ~/.nectar
[ -f ~/.nectar/config.toml ] || cp config.example.toml ~/.nectar/config.toml
```

完整配置：

```toml
[feishu]
app_id = "cli_xxx"
app_secret = "xxx"
log_level = "info"            # debug / info / warn / error

[codex]
path = "codex"
model = ""                    # 空则使用 Codex 自身配置的模型
effort = ""                   # 如 high，透传为 model_reasoning_effort
sandbox = "workspace-write"   # read-only / workspace-write / danger-full-access
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 从 Codex 模型目录自动解析，也可手动指定
per_chat_workspace = false    # true 时每个 chat 使用工作目录子目录
bypass_approvals = false      # 谨慎开启

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

[cursor]
path = "agent"
model = ""                    # 例如 composer-2.5；空则使用 Cursor 默认模型
mode = ""                     # plan / ask；空为普通 Agent 模式
sandbox = ""                  # enabled / disabled；空则使用 Cursor 默认
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 未知，卡片底部省略上下文占比
auto_approve = true           # 等价 -f，非交互 bot 场景需要
trust_workspace = true        # 等价 --trust，无头运行需要
per_chat_workspace = false

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

[pi]
path = "pi"
provider = ""                 # 可选 provider 覆盖
model = ""                    # 例如 claude-sonnet-4-5；空则使用 Pi 默认模型
thinking = ""                 # off | minimal | low | medium | high | xhigh | max
working_dir = "~/.nectar/workspace"
timeout = "30m"
max_output_bytes = 2097152
context_window = 0            # 0 = 未知，卡片底部省略上下文占比
trust_workspace = true        # 等价 --approve，加载项目内的 Pi 资源
per_chat_workspace = false

[bot]
name = "Nectar"
engine = "codex"              # codex | opencode | cursor | claude | pi
admin_users = []              # open_id 白名单；非空时限制单聊
allowed_chats = []            # chat_id 白名单；非空时限制群聊

[storage]
data_dir = "~/.nectar"
```

`context_window` 为 `0` 时，Nectar 会读取 `$CODEX_HOME/config.toml`（默认 `~/.codex/config.toml`）里的 `model` 和 `model_catalog_json`，从模型目录中取 `context_window`。解析不到时卡片底部会省略上下文占比；也可以手动设置，例如 `context_window = 258000`。

页脚的模型名优先使用 `codex.model`，为空时读取 Codex 自身配置；显示时取模型标识的最后一段，例如 `deepseek/deepseek-flash` 显示为 `deepseek-flash`。

使用 OpenCode 时设置 `bot.engine = "opencode"`，Nectar 会读取 `[opencode]`；`model` 为空时从 `~/.config/opencode/opencode.json` 读取，并通过 `opencode session export` 获取最后一轮 assistant 的 token 用量。上下文上限从模型的 `limit.context` 自动读取；模型未在 OpenCode 配置中声明时，可用 `opencode.context_window` 手动指定。

使用 Cursor 时设置 `bot.engine = "cursor"`，Nectar 会读取 `[cursor]`，以 Cursor CLI（优先 `agent`，找不到时回退 `cursor-agent`）加 `-p --output-format stream-json --stream-partial-output` 运行，并用 `--resume` 续接会话。`auto_approve` 映射为 `-f`，`trust_workspace` 映射为 `--trust`，无头 bot 场景两者都需要。附件通过 prompt 中的本地路径交给 Agent。需要上下文占比时显式设置 `cursor.context_window`。

使用 Claude Code 时设置 `bot.engine = "claude"`，Nectar 会读取 `[claude]`，以 `claude -p --output-format stream-json --verbose --include-partial-messages` 运行，通过 stdin 传入 prompt，并用 `--resume` 续接会话。`auto_approve` 映射为 `--dangerously-skip-permissions`；关闭时使用 `permission_mode`。token 用量和上下文上限从最终 `result` 事件读取，`claude.context_window` 仅用于手动覆盖。附件通过 prompt 中的本地路径交给 Agent。

使用 Pi 时设置 `bot.engine = "pi"`，Nectar 会读取 `[pi]`，以 `pi --mode json` 运行，通过 stdin 传入 prompt，并用 `--session` 续接会话。`provider`、`model`、`thinking` 直接透传为 Pi 参数；`trust_workspace` 映射为 `--approve`，允许 Pi 加载项目内的设置、skills、上下文文件和扩展。token 用量从 assistant 消息事件读取；需要底部上下文占比时设置 `pi.context_window`。附件通过 prompt 中的本地路径交给 Agent。

也可以用环境变量覆盖：

```bash
export FEISHU_APP_ID=cli_xxx
export FEISHU_APP_SECRET=xxx
export CODEX_PATH=codex
export OPENCODE_PATH=opencode
export CURSOR_AGENT_PATH=agent
export CLAUDE_PATH=claude
export PI_PATH=pi
export NECTAR_ENGINE=codex
export NECTAR_DATA_DIR=~/.nectar
export NECTAR_WORKING_DIR=~/.nectar/workspace
```

## 使用

```bash
nectar
```

如果 `~/.local/bin` 不在 `PATH` 里，按安装脚本提示添加。也可以直接从源码运行：

```bash
go build -o nectar .
./nectar
```

启动后先等待日志出现：

```text
feishu websocket connected; ready to receive messages
```

如果网络抖动导致首次连接失败，SDK 会自动重连；连接成功前发送的消息不会补发。建议先发 `/help` 验证入站链路，`/help` 不经过引擎。

`/help`、`/commands`，以及“有哪些命令”这类短问句会直接返回命令卡片，不调用引擎。

所有内置命令结果（`/new`、`/status`、`/stop` 和命令错误）都使用平台卡片；不支持卡片的平台自动降级为文本。

Agent 执行过程中，中间消息会实时更新 `🐝 Working...` 卡片（带节流）；最终 `🍯 Complete` 卡片只显示最后一条结果，不再混入中间叙述。

> 帮助/命令卡片和实时进度需要 v0.1.2 及以上版本。

话题里的消息会在同一话题内回复；普通聊天消息仍然直接发送，不引用原消息。

> 话题回复需要 v0.1.3 及以上版本。

- 单聊直接发消息
- 群聊需要 @Bot

### 后台服务

编辑好 `~/.nectar/config.toml` 后：

```bash
nectar service install
```

- macOS：安装 LaunchAgent，登录后启动，崩溃自动拉起
- Linux：安装 systemd user service；加 `--linger` 可开机启动，无需登录
- `--no-start`：安装并启用服务，但不立即启动
- 管理命令：`nectar service status|logs|restart|stop|uninstall`

服务模式从配置文件读取所选平台的凭证，不继承 shell 里的环境变量。详细说明见 [后台服务](docs/zh-CN/service.md)。

### 命令

| 命令 | 作用 |
|---|---|
| `/new`、`/reset` | 清空当前会话，下一条消息启动新的 agent thread |
| `/status` | 查看当前引擎、模型和 agent thread ID |
| `/stop` | 中断当前任务 |
| `/help`、`/commands` | 显示命令卡片，不调用引擎 |

## 文档

- [飞书配置与排查](docs/zh-CN/feishu.md)
- [钉钉配置与排查](docs/zh-CN/dingtalk.md)
- [后台服务](docs/zh-CN/service.md)
- [安全边界](docs/zh-CN/security.md)
- [路线图与待实现](docs/zh-CN/roadmap.md)
- [Changelog](docs/zh-CN/changelog.md)
- [English docs](docs/en/README.md)

## License

MIT © 2026 zhou.yongguo. See [LICENSE](LICENSE).
