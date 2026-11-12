# Changelog

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
