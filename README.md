<h1 align="center">🍯 Nectar</h1>

<p align="center">
  <strong>Memory that grows with you.</strong><br>
  Go 编写的飞书 ↔ Codex Agent 桥接器
</p>

<p align="center">
  <a href="README.md">简体中文</a> ·
  <a href="README.en.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/zhouyg1992/nectar/releases"><img src="https://img.shields.io/github/v/release/zhouyg1992/nectar?display_name=tag&sort=semver" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zhouyg1992/nectar" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.22+">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="macOS | Linux">
  <a href="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml"><img src="https://github.com/zhouyg1992/nectar/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

> 首个 Release 版本：**v0.1.0**，推送 `v0.1.0` tag 后由 GitHub Actions 自动构建。

Nectar 把飞书消息交给 Codex CLI 执行，再以交互卡片把结果回传到飞书。v1 支持飞书 + Codex，安装后一条命令即可启动。

## 支持矩阵

**v1 当前仅支持：飞书 / Lark + Codex CLI。**

图例：✅ 已支持 · 🚧 计划中

### 平台

| 平台 | 状态 | 说明 |
|---|---|---|
| 飞书 / Lark | ✅ 已支持 | 长连接、交互卡片、上下文占比 |
| 钉钉 | 🚧 计划中 | Stream 模式与互动卡片适配 |
| Telegram | 🚧 计划中 | Long Polling |
| 企业微信 / 微信 | 🚧 计划中 | 视平台能力适配 |
| Slack | 🚧 计划中 | Socket Mode |
| 更多平台 | 🚧 计划中 | 通过 `PlatformAdapter` 扩展 |

### 引擎

| 引擎 | 状态 | 说明 |
|---|---|---|
| Codex CLI | ✅ 已支持 | `exec --json` / `resume` |
| Claude Code | 🚧 计划中 | CLI / Agent SDK 适配 |
| OpenCode | 🚧 计划中 | CLI 适配 |
| Pi | 🚧 计划中 | CLI 适配 |
| Kimi Code | 🚧 计划中 | 本地 Server API 适配 |
| 更多引擎 | 🚧 计划中 | 通过 `EngineAdapter` 扩展 |

> 计划中的条目是方向，不代表具体排期；实际进度以 [Roadmap](docs/zh-CN/roadmap.md) 和 Releases 为准。

## 安装

macOS / Linux（amd64 / arm64）：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

安装脚本会：

- 自动识别操作系统和 CPU 架构
- 从 GitHub Releases 下载对应版本
- 校验 `checksums.txt` 里的 SHA-256
- 安装到 `~/.local/bin/nectar`
- 如果 `~/.nectar/config.toml` 不存在，从示例配置创建一份

指定版本或安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --version v0.1.0
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --dir /usr/local/bin
```

建议先下载检查再执行：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh -o install.sh
sh install.sh
```

Windows 请使用 WSL，或从 Releases 手动下载。仓库必须是公开的，`curl | sh` 才能匿名访问。

## 前置条件

### Codex CLI

安装 Codex CLI 并完成登录，确认下面的命令可以正常运行：

```bash
codex exec --json --skip-git-repo-check "reply with OK"
```

### 飞书应用

创建企业自建应用，开启机器人能力，订阅消息事件，并使用长连接接收事件。详细步骤和排查方法见 [飞书配置](docs/zh-CN/feishu.md)。

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

[bot]
name = "Nectar"
agent_id = "codex"
admin_users = []              # open_id 白名单；非空时限制单聊
allowed_chats = []            # chat_id 白名单；非空时限制群聊

[storage]
data_dir = "~/.nectar"
```

`context_window` 为 `0` 时，Nectar 会读取 `$CODEX_HOME/config.toml`（默认 `~/.codex/config.toml`）里的 `model` 和 `model_catalog_json`，从模型目录中取 `context_window`。解析不到时卡片底部会省略上下文占比；也可以手动设置，例如 `context_window = 258000`。

页脚的模型名优先使用 `codex.model`，为空时读取 Codex 自身配置；显示时取模型标识的最后一段，例如 `deepseek/deepseek-flash` 显示为 `deepseek-flash`。

也可以用环境变量覆盖：

```bash
export FEISHU_APP_ID=cli_xxx
export FEISHU_APP_SECRET=xxx
export CODEX_PATH=codex
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

如果网络抖动导致首次连接失败，SDK 会自动重连；连接成功前发送的消息不会补发。建议先发 `/help` 验证入站链路，`/help` 不经过 Codex。

- 单聊直接发消息
- 群聊需要 @Bot

### 卡片行为

- 请求开始后立即发送 `🐝 Thinking...` 卡片，不引用原消息
- 工具调用时更新为 `🐝 Working...` 并显示当前命令
- 完成后原地更新为 `🍯 Complete`，失败为 `⚠️ Failed`
- 完成卡片底部显示上下文占比、模型、耗时和 token：

```text
ctx: 17.6k/1.0M (1.7%) · deepseek-flash · 6.0s · ↑17.6k · ↓11
```

### 命令

| 命令 | 作用 |
|---|---|
| `/new`、`/reset` | 清空当前会话，下一条消息启动新的 Codex thread |
| `/status` | 查看当前 Codex thread ID |
| `/stop` | 中断当前任务 |
| `/help` | 显示帮助 |

## 文档

- [飞书配置与排查](docs/zh-CN/feishu.md)
- [架构与扩展点](docs/zh-CN/architecture.md)
- [开发与测试](docs/zh-CN/development.md)
- [安全边界](docs/zh-CN/security.md)
- [发布流程](docs/zh-CN/release.md)
- [路线图与待实现](docs/zh-CN/roadmap.md)
- [Changelog](docs/zh-CN/changelog.md)
- [English docs](docs/en/README.md)（translation in progress）

## License

MIT © 2026 zhou.yongguo. See [LICENSE](LICENSE).
