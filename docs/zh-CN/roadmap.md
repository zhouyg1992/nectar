# 路线图与待实现

## v0.3.0 已完成

- 飞书入站图片、文件与图文混合消息
- Codex/OpenCode 原生图片附件投递

## v1 已完成

- 飞书 WebSocket 长连接
- 单聊和群聊 @Bot
- Codex `exec --json` / `resume`
- OpenCode `run --format json` / `--session`（下个版本）
- Cursor Agent `-p --output-format stream-json` / `--resume`
- Claude Code `-p --output-format stream-json` / `--resume`
- JSONL 事件解析
- 会话键与同会话 FIFO 队列
- 交互卡片：Thinking / Working / Complete / Failed
- 完成卡片上下文占比、模型、耗时和 token
- `/new`、`/status`、`/stop`、`/help`
- 失败教训记录（pending，不注入）
- 一键安装脚本与 GitHub Release 流水线

## v1 明确不做

- 逐 token 流式输出
- 会话持久化
- 教训注入与审核卡片
- 定时任务
- 云同步
- 多平台实现与 Codex/OpenCode/Cursor/Claude Code 之外的引擎

## 后续

### v1.1

- 定时任务
- 教训日志检索
- 更细的流式输出

### v1.2

- 飞书审核卡片
- 教训 `pending → active` 后才注入

### v1.3

- Pollen 云同步：Comb、`AGENTS.md`、skills
- 多设备冲突处理

### 更远

- 钉钉、Telegram、微信、Slack
- Kimi、Pi
- Jev 决策服务
- Agent Teams / Multi-Bot Relay

历史变更见 [changelog.md](changelog.md)。
