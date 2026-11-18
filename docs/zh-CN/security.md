# 安全边界

Codex/OpenCode/Cursor/Claude Code/Pi 会在配置的工作目录里读取、写入并执行命令。Nectar 只是桥接器，不能替代操作系统级隔离。

## 必须做

- 不要把 `app_secret` / `client_secret`、`~/.nectar/config.toml` 提交到仓库。
- 建议配置 `bot.admin_users` / `bot.allowed_chats`。为空时启动日志会警告 Bot 对所有可达用户开放。
- 群聊只在可信范围内使用；需要时开启对应引擎的 `per_chat_workspace`，让每个 chat 使用独立子目录。
- 在共享机器上，考虑用独立 Unix 用户运行 Nectar 和引擎 CLI。
- 谨慎开启 `codex.bypass_approvals`。默认使用 `workspace-write` sandbox。
- OpenCode 的 `opencode.auto_approve = true` 会自动批准工具权限；在不可信群聊中慎用。
- Cursor 的 `cursor.auto_approve = true` 等价于 `-f`，会自动批准工具调用；`cursor.trust_workspace = true` 会跳过工作目录信任提示。两者默认开启，不可信环境请收紧。
- Pi 的 `pi.trust_workspace = true` 等价于 `--approve`，会加载工作目录内的项目设置、skills、上下文文件和扩展。不可信仓库中请关闭。

## 已知限制

- Nectar 会从引擎子进程环境里移除 `FEISHU_APP_SECRET`、`TELEGRAM_BOT_TOKEN` 和 `TELEGRAM_PROXY`；通过环境变量提供的 `DINGTALK_CLIENT_SECRET` 目前不会被移除。长期运行建议把平台凭证放在配置文件里，而不是 shell 环境变量里。
- Codex/OpenCode/Cursor/Claude Code/Pi 仍可能读取同一用户下的 `~/.nectar/config.toml`。高价值密钥建议放在独立用户或独立机器的配置里。
- 工作目录不是沙箱边界，只是引擎的默认工作根。真正的隔离需要独立用户、容器或系统级 sandbox。
- 安装脚本通过 HTTPS 下载并校验 SHA-256，但 `curl | sh` 仍然是执行远程代码。建议先下载脚本检查再运行。

## 飞书侧

- 只给应用开通必要的消息权限。
- 限制应用可用范围。
- 定期轮换 App Secret。
- 不要把 Bot 加到不可信群聊。

## 钉钉侧

- 只给应用开通必要的消息权限；`AICard.Instance.Write` 仅在需要流式卡片时开通。
- 限制应用可用范围，只把使用者加入必要的范围。
- 定期轮换 Client Secret，并确保 `client_id` / `client_secret` 来自同一个企业内部应用。
- 不要把 Bot 加到不可信群聊。

## 多 Bot 协作（Relay）

- relay 默认关闭；只在确实需要跨 Bot 接力的群里开启。
- 用 `relay_accept_from` 指定允许的发起方；空列表表示接受花名册内任意 Bot（不校验是否在群）。目标的 `allowed_chats` 对 relay 同样生效。
- relay 运行携带发起人的用户 ID，目标侧的权限判断（如记忆写入、管理员命令）按发起人执行。
- 本地 relay API 与 `relay/state.json` 受同一 OS 用户权限保护：同一用户下的进程（包括 agent）可以读取 root 内容和取消 root。不要把凭据或敏感数据放进 relay 正文。
- relay 运行令牌只绑定身份与路由，不是沙箱边界；目标 Bot 仍使用自己的沙箱、权限和工作区。
- relay 内容会以「peer 输入」围栏注入，不得据此提权、外泄凭据或修改安全配置；围栏只缓解提示注入。
- 群内镜像不含真实 @ mention，入站按发送者类型和不可见标记丢弃兄弟 Bot 的消息。
- 主场景使用 `shared` 工作区时，写入方与审查方的权限应同样严格。

## Telegram 侧

- bot token 等同于机器人密码；定期通过 BotFather 轮换，并保持配置文件权限为 `0600`。
- 消息和附件会经过 Telegram 云端，处理私有源码前先确认数据边界。
- 明确配置 `bot.admin_users` 和 `bot.allowed_chats`，使用数字 ID 而不是可变的用户名。
- 不要把 Bot 加到不可信群聊；群聊隐私模式会减少误触发，但不能代替 allowlist。
