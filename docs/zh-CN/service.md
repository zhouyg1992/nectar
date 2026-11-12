# 后台服务

Nectar 可以把自身安装为当前用户的后台服务：

- macOS：LaunchAgent，登录后启动，崩溃自动拉起
- Linux：systemd user service，配合 linger 可开机启动

服务会使用安装时的绝对路径启动 `nectar -config ~/.nectar/config.toml`。

> 后台服务支持需要 v0.1.1 及以上版本。

## 先配置，再安装服务

服务模式从 `~/.nectar/config.toml` 读取飞书凭证，不继承 shell 里的 `FEISHU_APP_ID` / `FEISHU_APP_SECRET`。

先编辑：

```bash
~/.nectar/config.toml
```

确认 `[feishu]` 下的 `app_id` / `app_secret` 已经是真实值，然后：

```bash
nectar service install
```

也可以在一键安装时直接带服务：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --service
```

Linux 上加 `--linger`：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --service --linger
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

卸载只删除服务文件，不会删除配置、数据和日志：

```bash
nectar service uninstall
```

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

如果 `codex` / `opencode` 通过 nvm、asdf 或其他版本管理器安装，确保其目录在安装服务时的 `PATH` 里，或者在 `~/.nectar/config.toml` 中把对应引擎的 `path` 指向可执行文件。

## 排查

| 现象 | 排查 |
|---|---|
| 服务安装失败，提示占位配置 | 先编辑 `~/.nectar/config.toml` |
| 服务启动后立即退出 | `nectar service logs` 查看引擎/飞书错误 |
| 引擎 CLI 找不到 | 检查服务 `PATH`，或在配置里设置 `codex.path` / `opencode.path` |
| 两个实例抢飞书事件 | 停掉手动运行的 `nectar`，只保留服务实例 |
| macOS 睡眠后消息丢失 | 睡眠期间长连接会断，唤醒后自动重连；睡眠中消息不会补发 |
| Linux 开机没启动 | 检查 `loginctl show-user "$USER" -p Linger` 是否为 `yes` |

## 安全

- 服务文件不包含 `app_secret`；凭证仍在 `~/.nectar/config.toml`，权限保持 `0600`。
- 不要用 root 运行个人 Nectar；引擎凭证和工作区都在用户目录下。
- 日志可能包含 chat id、模型信息和错误上下文，注意文件权限和轮转。
