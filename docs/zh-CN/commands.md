# 命令参考

Nectar 有两类命令：

- CLI 命令：在终端运行 `nectar ...`。
- 聊天命令：在飞书 / 钉钉里发送 `/new`、`/cron` 等。

## CLI 总览

```text
nectar [-config PATH]                 前台运行 bot
nectar -version                       查看版本
nectar help                           查看 CLI 帮助
nectar service <subcommand>           管理后台服务
nectar update                         更新版本
nectar schedule <subcommand>          管理定时任务
nectar skills <subcommand>            管理 Agent Skill
nectar memory <subcommand>            管理本地记忆
nectar uninstall                      卸载 Nectar
```

全局参数：

| 参数 | 说明 |
|---|---|
| `-config PATH` / `--config PATH` | 指定配置文件，默认 `~/.nectar/config.toml` |
| `-version` / `--version` | 打印版本并退出 |
| `-h` / `--help` | 显示帮助 |

> 安装脚本默认已经启动后台服务。手动前台运行时先执行 `nectar service stop`，避免多个实例同时连接平台。

## 后台服务

```bash
nectar service install [--config PATH] [--binary PATH] [--linger] [--no-start]
nectar service start [--config PATH] [--binary PATH]
nectar service stop [--config PATH] [--binary PATH]
nectar service restart [--config PATH] [--binary PATH]
nectar service status [--config PATH] [--binary PATH]
nectar service logs [--config PATH] [--binary PATH] [-n N] [-f]
nectar service uninstall [--config PATH] [--binary PATH]
```

| 参数 | 说明 |
|---|---|
| `--config PATH` | 服务使用的配置文件 |
| `--binary PATH` | 服务使用的 nectar 二进制 |
| `--linger` | Linux 上启用 systemd user linger，开机无需登录即可启动 |
| `--no-start` | 只安装并启用服务，不立即启动 |
| `-n N` | 日志行数，默认 200 |
| `-f` | 持续跟随日志 |

示例：

```bash
nectar service status
nectar service logs -n 500
nectar service logs -f
nectar service restart
```

## 更新

```bash
nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart] [--no-skills]
  [--config PATH] [--binary PATH] [--timeout 10m] [--attempts 2] [--no-progress]
```

| 参数 | 说明 |
|---|---|
| `--check` | 只检查最新版本，不安装 |
| `--force` | 即使已经是目标版本也重新安装 |
| `--version vX.Y.Z` | 安装指定版本，可用于降级 |
| `--no-restart` | 更新二进制和 Skill，但不重启服务 |
| `--no-skills` | 跳过 Agent Skill 刷新 |
| `--config PATH` | 用于检测已安装服务 |
| `--binary PATH` | 更新的二进制路径，默认当前可执行文件 |
| `--timeout 10m` | 每次下载尝试的超时，默认 10 分钟 |
| `--attempts 2` | 元数据和下载请求的最大尝试次数 |
| `--no-progress` | 不显示下载进度条 |

示例：

```bash
nectar update
nectar update --check
nectar update --version v0.8.0
nectar update --no-restart
nectar update --timeout 15m --attempts 3
```

下载资产时，终端会显示百分比进度条；重定向或 TLS 连接短暂失败会自动重试。

## 定时任务

CLI 通过 `~/.nectar/run/api.sock` 与正在运行的 bot 通信，因此需要先启动服务。

### list

```bash
nectar schedule list [--all] [--json] [--socket PATH]
```

| 参数 | 说明 |
|---|---|
| `--all` | 包含已完成和失败的任务 |
| `--json` | 输出 JSON |
| `--socket PATH` | 指定 schedule API socket |

### add

```bash
nectar schedule add \
  (--cron "0 9 * * 1-5" | --every 90m | --in 30m | --at "2026-11-14T15:00:00+08:00") \
  --prompt "任务内容" \
  [--desc "描述"] [--timeout 30m] [--timezone Asia/Shanghai] \
  [--channel feishu] [--chat CHAT_ID] [--thread THREAD_ID] [--agent codex] \
  [--json] [--socket PATH]
```

`--cron`、`--every`、`--in`、`--at` 必须且只能选一个。

目标默认从 `NECTAR_CHANNEL`、`NECTAR_CHAT_ID`、`NECTAR_THREAD_ID`、`NECTAR_AGENT_ID` 读取；Agent 会话里会自动注入这些变量。

### preview

```bash
nectar schedule preview --cron "0 9 * * 1-5" [--timezone Asia/Shanghai] [--json]
```

只计算下一次执行时间，不创建任务。支持与 `add` 相同的时间参数。

### 管理

```bash
nectar schedule pause <id>
nectar schedule resume <id>
nectar schedule run <id>
nectar schedule cancel <id>
nectar schedule delete <id>
```

- `run`：立即执行一次，不改变原计划。
- `cancel`：取消正在执行的那一轮，任务保留。
- `delete`：删除任务，停止未来调度。

别名：`ls` = `list`，`stop` = `cancel`，`del` / `rm` = `delete`。

### update

```bash
nectar schedule update <id> \
  [--prompt "新内容"] [--desc "新描述"] [--timeout 15m] \
  [--cron "0 10 * * 1-5" | --every 2h | --at "..." | --in 30m] \
  [--timezone Asia/Shanghai] [--json] [--socket PATH]
```

`--cron`、`--every`、`--at`、`--in` 最多选一个。

## Agent Skill

```bash
nectar skills install [--config PATH] [--data-dir DIR] [--no-workspace] [--quiet]
nectar skills uninstall [--config PATH] [--data-dir DIR] [--no-workspace] [--dry-run] [--quiet]
```

| 参数 | 说明 |
|---|---|
| `--config PATH` | 配置文件路径 |
| `--data-dir DIR` | Nectar 数据目录 |
| `--no-workspace` | 不修改工作区 `AGENTS.md` |
| `--dry-run` | 预览卸载内容 |
| `--quiet` | 除错误外不输出 |

`remove` 是 `uninstall` 的别名。

安装器会同时安装 `nectar-schedule` 和 `nectar-memory` 两个 Agent Skill，并在工作区 `AGENTS.md` 中维护对应的托管块。

## 记忆

`nectar memory` 直接读写本地 memory store，由 Agent 在 tool call 中调用，也可以手动排查：

```bash
nectar memory add [--kind profile|project|fact|lesson|episode] \
  [--scope chat|project|global] [--status candidate|active] \
  [--tags a,b] [--source agent] <text>
nectar memory search [--scope chat|project|global|all] [--limit N] <query>
nectar memory list [--status active|pending|archived|all] [--json]
nectar memory get <id> [--json]
nectar memory update <id> <new text> [--json]
nectar memory approve|reject|pin|unpin <id>
nectar memory forget <id> --confirm
```

- 默认 `--status candidate`，审核后才注入；用户在聊天中明确要求记住时可传 `--status active`。
- `--scope` 默认使用 `NECTAR_CHAT_ID` 对应的聊天；不在 Agent 会话中时默认当前项目。
- Agent 会话会自动通过 `NECTAR_API_SOCKET` 调用 daemon，避免沙箱直接写 `~/.nectar/memory`；手动 CLI 未设置 socket 时直接读写本地文件。
- `global` 记忆仅管理员可写、审核、固定和删除。
- 群聊中非管理员的写入会变为 `candidate`，审核、固定和删除也需要管理员。
- 删除必须显式传 `--confirm`，避免 Agent 误删。
- 项目/主目录路径会自动转换为 `${PROJECT_ROOT}` / `${HOME}` 模板；其他绝对路径标记为设备本地记忆。

## 卸载

```bash
nectar uninstall [--config PATH] [--binary PATH] [--purge-data] [--keep-binary] [--no-skills] [--dry-run]
```

| 参数 | 说明 |
|---|---|
| `--purge-data` | 同时删除配置、任务数据、工作区和备份 |
| `--keep-binary` | 保留二进制 |
| `--no-skills` | 保留 Agent Skill 和 `AGENTS.md` 托管片段 |
| `--dry-run` | 只预览清理项 |
| `--config PATH` | 配置文件路径 |
| `--binary PATH` | 二进制路径 |

## 聊天命令

| 命令 | 作用 |
|---|---|
| `/new`、`/reset` | 清空当前会话，下一条消息启动新 thread |
| `/status` | 查看当前引擎、模型和 Agent thread ID |
| `/about`、`/whoami` | 查看 Nectar 版本、平台、底层引擎和模型 |
| `/stop` | 中断当前任务 |
| `/cron`、`/schedule` | 定时任务入口 |
| `/memory` | 查看、审核、固定或删除记忆 |
| `/remember` | 显式保存一条记忆 |
| `/help`、`/commands` | 显示命令卡片，不调用引擎 |

`/help`、`/commands`，以及“有哪些命令”这类短问句会直接返回命令卡片，不调用引擎。

### /cron

```text
/cron
/cron list [all]
/cron add <min> <hour> <day> <month> <weekday> <prompt>
/cron every <duration> <prompt>
/cron in <duration> <prompt>
/cron at <RFC3339-time> <prompt>
/cron pause <id>
/cron resume <id>
/cron run <id>
/cron cancel <id>
/cron delete <id>
/cron update <id> prompt|cron|every|at|timezone|timeout ...
/cron help
```

`/cron` 不带子命令时等价于 `/cron list`。别名：`ls` = `list`，`stop` = `cancel`，`del` / `rm` = `delete`，`?` = `help`。

`/cron update` 的字段形式：

```text
/cron update <id> prompt <new prompt>
/cron update <id> cron <min> <hour> <day> <month> <weekday> [prompt]
/cron update <id> every <duration> [prompt]
/cron update <id> at <RFC3339-time> [prompt]
/cron update <id> timezone <IANA-timezone>
/cron update <id> timeout <duration|0s>
```

定时任务默认开启。管理命令仅限 `bot.admin_users`；该列表为空时不限制管理员。详见 [定时任务](schedule.md)。

### /memory

```text
/memory
/memory list [active|pending|archived|all]
/memory search <query>
/memory show <id>
/memory approve <id>
/memory reject <id>
/memory pin <id>
/memory unpin <id>
/memory forget <id>
/remember [project|global] <text>
/forget <id>
```

- `/memory` 不带子命令时等价于 `/memory list`。
- 失败任务会写入 `pending` 候选，审核通过前不会注入。
- `/remember` 默认写入当前会话；`project` 写入当前工作区；`global` 仅管理员可写。
- `global` 记忆的审核、固定和删除也只允许管理员操作。
- 群聊中非管理员 `/remember` 只创建 `pending` 候选，审核/删除需要管理员。

记忆保存在 `~/.nectar/memory/memory.json`，便于查看、备份和后续同步。

## Agent 会话环境变量

Agent 会话中会注入：

| 变量 | 说明 |
|---|---|
| `NECTAR_CHANNEL` | 当前平台 |
| `NECTAR_CHAT_ID` | 当前会话 ID |
| `NECTAR_THREAD_ID` | 当前话题 ID |
| `NECTAR_AGENT_ID` | 当前引擎 |
| `NECTAR_WORKING_DIR` | 当前工作区 |
| `NECTAR_API_SOCKET` | schedule API socket |
| `NECTAR_CLI` | nectar 二进制绝对路径 |
| `NECTAR_USER_ID` | 当前用户 ID |

因此 Agent 调用 `nectar schedule` 时通常不需要手动传目标 chat。

返回：[中文 README](../../README.zh-CN.md) · [配置参考](config.md) · [安装与升级](install.md)
