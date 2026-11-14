# 后台服务

Nectar 可以把自身安装为当前用户的后台服务：

- macOS：LaunchAgent，登录后启动，崩溃自动拉起
- Linux：systemd user service，配合 linger 可开机启动

服务会使用安装时的绝对路径启动 `nectar -config ~/.nectar/config.toml`。

## 手动安装服务

一键安装脚本默认会写入配置并安装、启动后台服务，通常不需要本节。只有手动安装服务，或修改配置后需要重新安装/重启时，才需要按下面的步骤操作。

服务模式从 `~/.nectar/config.toml` 读取所选平台（飞书或钉钉）的凭证，不继承 shell 里的 `FEISHU_APP_ID` / `FEISHU_APP_SECRET`、`DINGTALK_CLIENT_ID` / `DINGTALK_CLIENT_SECRET`。

先编辑：

```bash
~/.nectar/config.toml
```

确认所选平台的配置已经是真实值：

- 飞书：`[feishu]` 下的 `app_id` / `app_secret`
- 钉钉：`[dingtalk]` 下的 `client_id` / `client_secret`（也兼容旧字段 `app_key` / `app_secret`）
- Telegram：`[telegram]` 下的 `bot_token`

如果同时配置了多个平台，需要在 `[bot]` 里显式设置 `platform = "feishu"`、`"dingtalk"` 或 `"telegram"`。然后：

```bash
nectar service install
```

一键安装脚本默认会写入配置并安装、启动后台服务。只安装二进制、不装服务：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --no-service
```

Linux 上加 `--linger`：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --linger
```

如果配置仍是示例占位符，服务安装会直接失败并提示你先编辑配置。

## 常用命令

```bash
nectar service status
nectar service logs
nectar service logs -n 500
nectar service logs -f
nectar service restart
nectar service stop
nectar service start
nectar service uninstall
```

`nectar service install --no-start` 会写入并启用服务，但不立即启动。

## 更新

`nectar update` 会下载最新版本、校验 `checksums.txt`、原子替换二进制、用新二进制刷新 Agent skill，并重启已安装的后台服务；不会修改 `~/.nectar/config.toml` 和数据。

```bash
nectar update
nectar update --check
nectar update --version v0.6.1
nectar update --no-restart
```

用 `--force` 可重装当前版本。更新支持 macOS 和 Linux；Release 不提供 Windows 资产。

会话映射和 last usage 会保存到 `~/.nectar/sessions.json`。重启后 Nectar 会 resume 原来的引擎会话；如果该 session 已失效，会提示并启动新会话。

## macOS

服务文件：

```text
~/Library/LaunchAgents/com.zhouyg1992.nectar.plist
```

日志：

```text
~/Library/Logs/nectar.out.log
~/Library/Logs/nectar.err.log
```

LaunchAgent 在**用户登录后**启动。如果 Mac 重启后没有人登录，Nectar 不会运行。要真正 7×24，建议放在常开的 Linux 服务器、NAS 或 Mac mini 上。

`nectar service uninstall` 只删除服务文件，不会删除二进制、Skill、配置、数据和日志。
完整卸载请使用：

```bash
nectar uninstall
nectar uninstall --purge-data
```

第一条会删除服务、二进制、Agent Skill 和工作区托管片段，但保留配置、任务数据与备份；
第二条会同时清除配置、数据和备份。只想移除 Agent Skill 时使用 `nectar skills uninstall`。

## Linux

服务文件：

```text
~/.config/systemd/user/nectar.service
```

日志：

```bash
journalctl --user -u nectar.service -f
```

默认 systemd user service 在用户登录后启动。要开机启动且无需登录：

```bash
nectar service install --linger
```

如果权限不足，手动执行：

```bash
sudo loginctl enable-linger "$USER"
```

## 路径和环境

服务环境不会加载 `.zshrc` / `.bashrc`。以下内容会写入服务定义：

- `HOME`
- `PATH`（包含 Nectar 所在目录和常见系统路径）
- `CODEX_HOME`（如果当前 shell 设置了）

如果 `codex` / `opencode` / `agent`（或旧版 `cursor-agent`）通过 nvm、asdf 或其他版本管理器安装，确保其目录在安装服务时的 `PATH` 里，或者在 `~/.nectar/config.toml` 中把对应引擎的 `path` 指向可执行文件。

## 排查

| 现象 | 排查 |
|---|---|
| 服务安装失败，提示占位配置 | 先编辑 `~/.nectar/config.toml` |
| 服务启动后立即退出 | `nectar service logs` 查看引擎/平台错误（飞书或钉钉） |
| 引擎 CLI 找不到 | 检查服务 `PATH`，或在配置里设置 `codex.path` / `opencode.path` / `cursor.path` |
| Cursor 提示未登录 | 用运行服务的同一用户执行 `agent login`（旧版 `cursor-agent login`），或设置 `CURSOR_API_KEY` |
| 两个实例抢飞书/钉钉事件 | 停掉手动运行的 `nectar`，只保留服务实例 |
| macOS 睡眠后消息丢失 | 睡眠期间长连接会断，唤醒后自动重连；睡眠中消息不会补发 |
| Linux 开机没启动 | 检查 `loginctl show-user "$USER" -p Linger` 是否为 `yes` |

## 安全

- 服务文件不包含 `app_secret` / `client_secret`；凭证仍在 `~/.nectar/config.toml`，权限保持 `0600`。
- 不要用 root 运行个人 Nectar；引擎凭证和工作区都在用户目录下。
- 日志可能包含 chat id、模型信息和错误上下文，注意文件权限和轮转。
