# 路线图与待实现

## v0.10.5 已完成

- 钉钉卡片布局改进：普通段落撑满卡片宽度，8 列 50 行以内的小表格改用 Markdown 渲染，修复表头灰底和宽度错位

## v0.10.4 已完成

- 任务完成后给最终消息添加完成表情（飞书 `DONE`、Telegram 👍）；`/about` 与 Agent 身份提示修复版本号重复 `v` 前缀

## v0.10.3 已完成

- `nectar update` 支持 GitHub 镜像前缀和 HTTP Range 断点续传，弱网或无法直连 GitHub 时更新更稳

## v0.10.2 已完成

- Dashboard 记忆审核支持批准/拒绝/恢复、批量操作、状态和标签筛选、编辑 scope，记忆与定时任务列表按每页 50 条分页

## v0.10.1 已完成

- Codex app-server 的 `max_output_bytes` 改为按单条事件限制，避免长任务因累计输出被误判超限

## v0.10.0 已完成

- 多 Bot：独立平台身份、引擎、工作区、会话、记忆和权限，支持按 Bot 查看服务日志、定时任务和记忆
- 会话级 git worktree，以及 `nectar bots`、`nectar worktrees list|remove` 等清理命令
- 本地 Dashboard：Bot 状态总览、记忆审核、定时任务管理和只读配置，默认关闭且只监听回环地址

## v0.9.0 已完成

- 运行中插话：`/steer`、`/append`、`/ps`、`/btw` 向进行中的任务追加补充信息，普通消息仍按队列排队
- Codex app-server、Claude stream-json、Pi rpc 和 OpenCode 共享 server 插话后端，并默认启用
- 出站媒体：Agent 将图片和文件写入工作区的 `.nectar/outbox/`，本轮结束后自动发送到飞书、钉钉和 Telegram

## v0.8.1 已完成

- 会话管理 v2：稳定的 Nectar 会话 ID、名称、当前会话指针、每会话用量和消息数，以及 version 1 会话文件自动迁移
- 新增 `/list`、`/switch`、`/current`、`/rename`、`/archive`、`/unarchive` 聊天命令；`/new [name]` 保留旧会话
- 按会话的 FIFO 队列，并对共享同一 workspace 的运行做串行化
- 排队消息复用一张卡片，从 Queued 原地更新为 Working 和 Complete
- 钉钉卡片保留单换行，会话卡片改用 Markdown 表格对齐，并支持 Complete/Finalizing 两阶段收尾

## v0.8.0 已完成

- 本地优先的 memory 层：profile / project / chat 作用域，显式 `/remember` 与 `/memory` 管理
- 失败教训候选审核、按 hash 去重的检索注入、类型配额与敏感信息落盘前脱敏
- `nectar memory` CLI、`nectar-memory` Agent Skill，以及通过 daemon socket 完成的沙箱内记忆写入
- Nectar runtime 身份注入与 `/about` / `/whoami`

## v0.7.0 已完成

- Telegram Bot API 平台适配器：长轮询、单聊/群聊、论坛话题、附件下载、流式消息编辑与 4096 字符自动分片
- `[telegram]` 配置支持可选代理和自建 API 地址，安装器支持 `--platform telegram`
- Telegram bot token 不再传递给 Agent 子进程

## v0.6.0 已完成

- 定时任务：Cron、固定间隔、一次性任务、持久化、启动补偿与执行预览
- 调度控制：并发限制、Jitter、超时、取消、历史保留与运行状态
- `nectar-schedule` Agent Skill 的安装、升级与卸载
- `nectar uninstall` 完整卸载流程
- README 与安装、配置、命令、引擎、服务文档重构

## v0.5.0 已完成

- 钉钉 Stream 模式长连接、A2UI 流式卡片与图片/文件附件
- 交互式安装脚本：自动检测引擎、生成配置并安装后台服务
- `/status` 显示当前引擎与模型

## v0.4.0 已完成

- Cursor Agent CLI 引擎适配
- Claude Code 引擎适配
- Pi 引擎适配
- 引擎可在完成事件中返回运行时 `context_window`，卡片上下文占比自动适配

## v0.3.0 已完成

- 飞书入站图片、文件与图文混合消息
- Codex/OpenCode 原生图片附件投递

## v1 已完成

- 飞书 WebSocket 长连接
- 单聊和群聊 @Bot
- Codex `exec --json` / `resume`
- OpenCode `run --format json` / `--session`
- Cursor Agent `-p --output-format stream-json` / `--resume`
- Claude Code `-p --output-format stream-json` / `--resume`
- Pi `--mode json` / `--session`
- JSONL 事件解析
- 会话键与同会话 FIFO 队列
- 交互卡片：Thinking / Working / Complete / Failed
- 完成卡片上下文占比、模型、耗时和 token
- `/new`、`/status`、`/stop`、`/help`
- 失败教训记录（pending，不注入）
- 一键安装脚本与 GitHub Release 流水线

## v1 明确不做

- 逐 token 流式输出
- 教训注入与审核卡片
- 云同步
- 飞书 / 钉钉 / Telegram 之外的平台，以及 Codex/OpenCode/Cursor/Claude Code/Pi 之外的引擎

## 后续

### v1.1

- 教训日志检索
- 更细的流式输出

### v1.2

- 飞书审核卡片
- 教训 `pending → active` 后才注入

### v1.3

- Pollen 云同步：Comb、`AGENTS.md`、skills
- 多设备冲突处理

### 更远

- 微信、Slack
- Kimi
- Jev 决策服务
- Agent Teams / Multi-Bot Relay

历史变更见 [changelog.md](changelog.md)。
