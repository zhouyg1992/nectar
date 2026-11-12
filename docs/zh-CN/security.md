# 安全边界

Codex/OpenCode 会在配置的工作目录里读取、写入并执行命令。Nectar 只是桥接器，不能替代操作系统级隔离。

## 必须做

- 不要把 `app_secret`、`~/.nectar/config.toml` 提交到仓库。
- 建议配置 `bot.admin_users` / `bot.allowed_chats`。为空时启动日志会警告 Bot 对所有可达用户开放。
- 群聊只在可信范围内使用；需要时开启对应引擎的 `per_chat_workspace`，让每个 chat 使用独立子目录。
- 在共享机器上，考虑用独立 Unix 用户运行 Nectar 和引擎 CLI。
- 谨慎开启 `codex.bypass_approvals`。默认使用 `workspace-write` sandbox。
- OpenCode 的 `opencode.auto_approve = true` 会自动批准工具权限；在不可信群聊中慎用。

## 已知限制

- Nectar 会从引擎子进程环境里移除 `FEISHU_APP_SECRET`。
- Codex/OpenCode 仍可能读取同一用户下的 `~/.nectar/config.toml`。高价值密钥建议放在独立用户或独立机器的配置里。
- 工作目录不是沙箱边界，只是引擎的默认工作根。真正的隔离需要独立用户、容器或系统级 sandbox。
- 安装脚本通过 HTTPS 下载并校验 SHA-256，但 `curl | sh` 仍然是执行远程代码。建议先下载脚本检查再运行。

## 飞书侧

- 只给应用开通必要的消息权限。
- 限制应用可用范围。
- 定期轮换 App Secret。
- 不要把 Bot 加到不可信群聊。
