# Changelog

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
