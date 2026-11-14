# Changelog

## v0.7.0 — 2026-11-14

### 新增

- 新增 Telegram Bot API 平台适配器：长轮询、单聊/群聊、论坛话题、附件下载、流式消息编辑和 4096 字符自动分片
- 支持 `[telegram] bot_token`，可选 `proxy` 和自建 `api_base_url`；安装器支持 `--platform telegram`
- Telegram bot token 会从 Agent 子进程环境中移除

### 变更

- README 增加飞书、钉钉和 Telegram 的界面预览

### 修复

- `/help` 卡片现在显示当前实际使用的引擎名称

## v0.6.1 — 2026-11-14

### 新增

- `nectar update` 下载 Release 资产时显示百分比、进度条和已下载/总大小
- 新增 `--timeout`、`--attempts`、`--binary` 和 `--no-progress` 更新参数

### 变更

- 连接、TLS、响应头、版本元数据和资产下载使用独立超时，不再用 60 秒总超时限制整个下载
- 临时连接、TLS、HTTP 5xx 等故障自动重试；Release 页面不可用时回退到 GitHub API 查询最新版本
- 默认每次下载最多等待 10 分钟，元数据和下载请求最多尝试 2 次

### 修复

- 修复慢网络下 Release 资产尚未下载完成就被整体请求超时中断的问题
- 支持使用临时新版 updater 通过 `--binary PATH` 更新指定的 Nectar 安装路径

## v0.6.0 — 2026-11-14

### 新增

- 内置定时任务：支持标准 5 段 Cron、固定间隔和一次性任务，可从聊天、CLI 与本地 Unix Socket API 管理
- 定时任务持久化、启动补偿、时区与执行预览，并提供失败记录和可配置的历史保留上限
- 调度控制：全局并发限制、周期任务 Jitter、单任务超时、运行中取消和 `running` 状态
- `nectar-schedule` Agent Skill：安装器自动安装，`nectar update` 自动刷新，支持独立卸载
- `nectar uninstall` 完整卸载，可选择保留或清除配置、任务数据、工作区和备份

### 变更

- 定时任务默认启用；管理命令仅限 `bot.admin_users`，该列表为空时不限制管理员
- README 重构为快速开始，并拆分引擎、配置、命令、安装、后台服务和定时任务参考文档

### 修复

- 调度任务只有在 Agent 实际执行完成后才更新完成或失败状态，长任务不会重复排队
- 调度状态落盘失败时回滚内存修改，并记录后台持久化错误
- 修正 Cron 通配步长的 day-of-month/day-of-week 组合语义，以及 `startup_grace = "0s"` 的补跑行为
- `schedule run` 不再消耗一次性任务的原始计划

## v0.5.3 — 2026-11-13

### 修复

- systemd unit 的 `WorkingDirectory` 不再加引号，修复 Linux 下服务启动失败的问题

## v0.5.2 — 2026-11-13

### 新增

- 进度卡片实时显示工具调用数量，工具输出保留在卡片正文，最终回答到达后自动隐藏
- `nectar update` 自更新命令：检查 Release、校验 `checksums.txt`、原子替换二进制并重启服务；支持 `--check`、`--force`、`--version` 和 `--no-restart`
- 会话跨重启持久化：聊天/线程到引擎 session 的映射和 last usage 保存到 `~/.nectar/sessions.json`，启动时自动恢复；resume 失败时提示并新建会话

### 变更

- 卡片用户可见文案统一为英文
- 简化卡片底部信息，并在飞书和钉钉的 footer 前增加分隔线

### 修复

- 上下文占比改用最近一次模型调用的 token 数计算，适配 Codex、Claude 和 Cursor；`/status` 保留本轮 token 用量

## v0.5.1 — 2026-11-13

### 变更

- Cursor CLI 对齐官方 `agent` 主命令，配置默认值与安装器检测优先使用 `agent`，同时兼容旧版 `cursor-agent`
- 安装器支持 `--engine agent`、`--engine cursor` 和 `--engine cursor-agent`

### 修复

- Cursor CLI 未登录时，失败卡片会提示执行 `agent login`（或 `cursor-agent login`），或设置 `CURSOR_API_KEY`

## v0.5.0 — 2026-11-13

### 新增

- 钉钉平台适配器：通过 Stream 模式长连接接收单聊和群聊 @ 消息，无需公网回调地址
- 钉钉 A2UI 流式卡片，支持 Markdown、原生表格和代码块；缺少卡片权限时自动降级为普通 Markdown
- 钉钉入站图片、文件与图文混排消息下载，复用平台无关的附件模型交给引擎处理
- 钉钉表情回复与消息去重
- `[dingtalk]` 配置段、`DINGTALK_CLIENT_ID` / `DINGTALK_CLIENT_SECRET` 环境变量与 `bot.platform = "dingtalk"`
- 交互式安装脚本：自动检测已安装的引擎 CLI，选择引擎与平台，生成最小配置并安装、启动后台服务
- `/status` 显示当前引擎和模型
- 文档新增英文版本、钉钉配置说明与流程示意图

### 变更

- 升级到 Go 1.26
- 精简文档结构，只保留使用文档

### 修复

- 钉钉表格改用原生 A2UI table 组件渲染，避免 Markdown 表格错乱

## v0.4.1 — 2026-11-12

### 变更

- 卡片状态 emoji 调整为自然隐喻：`🌼 Thinking`、`🌱 New Session`、`🌿 Session Status`、`✋ Stop`
- `🐝 Working`、`🍯 Complete`、`⚠️ Failed` 和错误提示保持不变

## v0.4.0 — 2026-11-12

### 新增

- Pi 引擎适配器：`--mode json`、`--session` 续接、工具事件与 token usage 映射
- `[pi]` 配置段、`PI_PATH` 环境变量与 `bot.engine = "pi"`
- Pi 的 `provider`、`model`、`thinking` 与 `trust_workspace`（`--approve`）
- Claude Code 引擎适配器：`-p --output-format stream-json`、`--resume` 续接、工具事件与 token usage 映射
- `[claude]` 配置段、`CLAUDE_PATH` 环境变量与 `bot.engine = "claude"`
- Claude Code 的 `auto_approve`（`--dangerously-skip-permissions`）、`permission_mode`、`fallback_model` 与 `max_turns`
- 引擎可在完成事件中返回运行时 `context_window`，卡片上下文占比自动适配
- Cursor Agent CLI 引擎适配器：`-p --output-format stream-json`、`--resume` 续接、工具事件与 token usage 映射
- `[cursor]` 配置段、`CURSOR_AGENT_PATH` 环境变量与 `bot.engine = "cursor"`
- Cursor 的 `auto_approve`（`-f`）与 `trust_workspace`（`--trust`）非交互开关

## v0.3.0 — 2026-11-12

### 新增

- 飞书入站富文本：支持 `post` 图文混合，并保留文字与图片顺序
- 飞书纯图片、文件消息下载后转交 Codex/OpenCode
- Codex 通过 `--image`、OpenCode 通过 `--file` 接收图片附件
- 平台无关的入站附件模型与 `EngineOptions.Attachments` 扩展点

### 修复

- 飞书扁平结构 `post` 消息不再退化为 `[rich text message]`，同时兼容语言包裹结构
- 富文本中的 Markdown 图片和 `img` 元素都会提取为真实附件
- 富文本顶层的 `files` 字段会提取为文件附件，覆盖“文字 + .txt/.log 文件”消息
- 文本类文件内容会直接注入 prompt，避免 Agent 沿用历史上下文而不读取本轮附件

## v0.2.0 — 2026-11-12

### 新增

- OpenCode 引擎适配器：`run --format json`、`--session` 续接、工具事件映射
- `bot.engine` 引擎选择与 `[opencode]` 配置段
- OpenCode 页脚显示真实模型、最后一轮 token 用量和上下文占比
- OpenCode 从模型 `limit.context` 自动解析上下文上限，支持 `opencode.context_window` 覆盖
- OpenCode 工具失败视为可恢复错误，有最终回答时卡片仍显示 Complete
- 飞书卡片迁移到 Card 2.0：标题、引用、行内代码、代码块、列表交给原生 markdown 渲染
- 飞书卡片使用原生 table 元素渲染 Markdown 表格（单卡最多 5 个，超出降级为代码块）
- 飞书卡片将 Markdown 图片转换为链接/文字，避免外链图片导致卡片 PATCH 失败
- 引用式图片（`![alt][ref]`）也会转换为链接/文字

## v0.1.3 — 2026-11-12

### 修复

- 飞书话题消息现在会在同一话题内回复，不再把卡片发到会话主界面
- 会话键纳入 `ThreadID`，同一群的不同话题使用独立 Codex 会话

## v0.1.2 — 2026-11-12

### 新增

- `/help`、`/commands` 和“有哪些命令”类短问句直接返回平台命令卡片，不经过 Codex
- `/new`、`/status`、`/stop`、空消息和命令错误也统一使用平台卡片，文本作为降级
- Codex 中间消息实时更新 Working 卡片；最终卡片只显示最后一条结果
- 飞书卡片正文改用 `markdown` 组件，修复反引号和代码块不渲染
- 新增 `CardInfo` 卡片状态，飞书使用 wathet 头部

## v0.1.1 — 2026-11-12

### 新增

- `nectar service install|uninstall|start|stop|restart|status|logs`
- macOS LaunchAgent 支持
- Linux systemd user service 支持，可选 `--linger`
- `install.sh --service` / `--linger` / `--no-start`

## v0.1.0 — 2026-11-12

首个可用版本（v1）。

### 新增

- 飞书 WebSocket 长连接与消息规范化
- Codex `exec --json` / `resume` 适配器
- Codex JSONL 事件解析
- 三元组会话键与同会话 FIFO 串行队列
- 飞书交互卡片：Thinking / Working / Complete / Failed
- 静态 `🐝` 进行中状态，`🍯 Complete`，`⚠️ Failed`
- 完成卡片上下文占比、模型、耗时和 token
- `/new`、`/status`、`/stop`、`/help`
- 失败教训记录到 `~/.nectar/comb/lessons.json`（pending，不注入）
- 工作目录、超时、输出上限
- `install.sh` 一键安装
- GitHub Actions Release 流水线
- 四平台预编译产物和 SHA-256 校验

### 修复

- 飞书 `channel.OnMessage` 因缺少 `EventDispatcher` 导致消息被静默丢弃
- Codex 子进程自定义取消必须使用 `exec.CommandContext`
- 同一会话队列改为严格 FIFO，避免消息乱序

### 已知限制

- 不持久化会话
- 不转发入站媒体
- 不做逐 token 流式输出
- 教训只记录不注入

详见 [roadmap.md](roadmap.md)。
