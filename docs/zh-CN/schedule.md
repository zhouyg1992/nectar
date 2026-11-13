# 定时任务

Nectar 内置一个进程内调度器。任务持久化到 `~/.nectar/schedules.json`，由后台 bot 进程触发，不依赖系统 crontab。

## 配置

```toml
[schedule]
enabled = true
timezone = "Local"      # 或 "Asia/Shanghai"
startup_grace = "5m"
```

- `timezone` 是 cron 的默认时区；也可以在单条任务里覆盖。
- `startup_grace` 控制重启后错过多久以内的任务仍然补跑。更早的一次性任务会标记为 missed，周期任务直接跳到下一次。

## 聊天命令

```text
/cron
/cron list [all]
/cron add <min> <hour> <day> <month> <weekday> <prompt>
/cron every <duration> <prompt>
/cron in <duration> <prompt>
/cron at <RFC3339-time> <prompt>
/cron pause|resume|run|delete <id>
/cron update <id> prompt|cron|every|at|timezone ...
```

例子：

```text
/cron add 0 9 * * 1-5 检查 CI 并汇报失败项
/cron every 90m 检查服务健康状态
/cron in 30m 提醒我检查刚才的部署
```

管理命令只允许 `bot.admin_users` 使用；该列表为空时沿用 Nectar 的“不限制用户”行为。

## CLI

```bash
nectar schedule list [--all] [--json]
nectar schedule add --cron "0 9 * * 1-5" --prompt "检查 CI"
nectar schedule add --every 90m --prompt "检查健康状态"
nectar schedule add --in 30m --prompt "提醒我检查部署"
nectar schedule add --at "2026-11-14T15:00:00+08:00" --prompt "开会"
nectar schedule preview --cron "0 9 * * 1-5"
nectar schedule pause|resume|run|delete <id>
nectar schedule update <id> --prompt "新内容" --cron "0 10 * * 1-5"
```

CLI 通过 `~/.nectar/run/api.sock` 与正在运行的 bot 通信。Agent 会话里会自动注入 `NECTAR_CHANNEL`、`NECTAR_CHAT_ID`、`NECTAR_THREAD_ID`、`NECTAR_AGENT_ID` 和 `NECTAR_CLI`（Nectar 二进制绝对路径），所以 Agent 不需要手动传聊天 ID，也不依赖服务进程的 `PATH` 是否包含 `~/.local/bin`。

## 自然语言

自然语言由 Agent 翻译，Nectar 只做确定性校验。建议把下面的规则加入工作区的 `AGENTS.md` / `CLAUDE.md` 或对应 Skill：

```text
你可以用本地命令 nectar schedule 管理定时任务。
- 日历周期（每天/每周/工作日/每月）用 --cron。
- 固定间隔（每 90 分钟、每两周）用 --every。
- 一次性任务用 --in 或 --at。
- 创建前先运行 nectar schedule preview，把 next_runs 展示给用户。
- 任务 prompt 必须自包含，不能依赖当前对话里的“刚才/上面”。
- 时间、星期或时区有歧义时先问用户。
- 创建成功后返回任务 ID 和下次执行时间。
```

`install.sh` 默认会做好这件事：

- 把 `nectar-schedule` skill 安装到 `~/.nectar/skills`、`~/.codex/skills`、
  `~/.claude/skills` 和 `~/.agents/skills`；
- 向 Nectar 工作区（以及配置里出现的 `working_dir`）的 `AGENTS.md` 写入一个
  带 `<!-- BEGIN NECTAR SCHEDULE -->` 标记的托管片段，供 OpenCode、Pi、Cursor
  等主要依赖 `AGENTS.md` 的引擎使用；
- 升级时会备份旧 skill 到 `~/.nectar/skill-backups`，并原地替换托管片段。

不想修改全局 Agent 配置时，安装时加 `--no-skills`。

`nectar update` 在替换二进制后会用新二进制执行 `nectar skills install`，
自动刷新全局 skill 和工作区 `AGENTS.md` 托管片段。需要单独刷新时也可以手动运行
`nectar skills install`。

## 语义与边界

- cron 使用标准 5 段格式：`minute hour day month weekday`，支持 `*`、`*/n`、`a-b`、`a-b/n`、逗号列表和 `jan-dec` / `sun-sat` 名称。
- 同时限制 day-of-month 和 weekday 时，按标准 cron 语义取 OR。
- 周期任务错过多轮不会补跑，只会在恢复后计算下一次；启动时最多补跑 `startup_grace` 内的一次。
- 一次性任务如果在执行过程中进程重启，`startup_grace` 内可能再执行一次（至少一次语义）。
- 同一任务上一轮未结束时，下一次触发会跳过并记录 `last_error`，不会叠加执行。
- 删除任务只停止未来调度，默认不打断正在执行的那一轮。
- 目前所有任务复用原聊天会话；需要隔离上下文的长任务请等待后续 `new-per-run` 支持。
