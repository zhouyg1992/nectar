# 安装与升级

## 前置条件

- macOS 或 Linux，amd64 / arm64
- 至少安装并登录一个引擎 CLI：Codex、Claude Code、Cursor Agent、OpenCode 或 Pi
- 能访问 GitHub Releases
- 需要 `curl` 或 `wget`、`tar`，以及 `sha256sum` 或 `shasum`

Windows 请使用 WSL，或从 Releases 手动下载。仓库必须是公开的，`curl | sh` 才能匿名访问。

## 交互式安装

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

安装器会：

1. 检测已安装的引擎 CLI；一个都没有时提示并退出。
2. 让你选择引擎和平台（默认飞书 / Lark，可选钉钉）。
3. 要求填写所选平台的凭证。
4. 从 GitHub Releases 下载对应版本，并尽量校验 `checksums.txt`。
5. 安装二进制到 `~/.local/bin/nectar`。
6. 写入最小配置 `~/.nectar/config.toml`。
7. 安装 `nectar-schedule` Skill，并向工作区 `AGENTS.md` 写入托管片段。
8. 安装并启动后台服务。

安装器默认保留已存在的 `~/.nectar/config.toml`。交互模式下会询问保留、重新配置还是退出。

> 安装器只写入 `[bot]` 和所选平台凭证。引擎参数、`[schedule]`、`bot.admin_users` 等都使用内置默认值，不会写入配置文件。

## 安装后验证

```bash
nectar service status
nectar service logs -f
```

等待平台连接成功的日志，然后在聊天里发送 `/help`：

- 飞书：`feishu websocket connected; ready to receive messages`
- 钉钉：`dingtalk: stream connected`

单聊直接发消息；群聊需要 @Bot。

## 安装选项

```text
--version vX.Y.Z        安装指定版本（默认最新）
--dir DIR               安装目录（默认 ~/.local/bin）
--config PATH           配置文件路径（默认 ~/.nectar/config.toml）
--engine NAME           codex | opencode | cursor | claude | pi
--platform NAME         feishu | dingtalk
--feishu-app-id ID      飞书 app_id
--feishu-app-secret S   飞书 app_secret
--dingtalk-client-id ID 钉钉 client_id
--dingtalk-client-secret S
                        钉钉 client_secret
--reconfigure           备份并替换现有配置
--no-service            只安装二进制，不安装后台服务
--service               兼容参数；服务本来就是默认行为
--no-skills             跳过 Agent Skill 安装
--linger                Linux 上启用 systemd user linger
--no-start              安装并启用服务，但不立即启动
-h, --help              显示安装帮助
```

对应环境变量：

```text
NECTAR_VERSION          同 --version
NECTAR_BIN_DIR          同 --dir
NECTAR_DOWNLOAD_BASE    覆盖 Release 下载地址
FEISHU_APP_ID           同 --feishu-app-id
FEISHU_APP_SECRET       同 --feishu-app-secret
DINGTALK_CLIENT_ID      同 --dingtalk-client-id
DINGTALK_CLIENT_SECRET  同 --dingtalk-client-secret
```

## 常用安装方式

指定版本或目录：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --version v0.6.0

curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --dir /usr/local/bin
```

先下载再执行：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh -o install.sh
sh install.sh
```

只安装二进制：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --no-service
```

非交互安装：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env FEISHU_APP_ID=cli_xxx FEISHU_APP_SECRET=xxx \
  sh -s -- --engine codex --platform feishu
```

钉钉：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  env DINGTALK_CLIENT_ID=dingxxx DINGTALK_CLIENT_SECRET=xxx \
  sh -s -- --engine codex --platform dingtalk
```

Linux 开机启动：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh |
  sh -s -- --linger
```

如果 `~/.local/bin` 不在 `PATH` 中，按安装器输出添加，例如：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## 更新

```bash
nectar update
```

`nectar update` 会下载最新版本、校验 `checksums.txt`、原子替换二进制、用新二进制刷新 Agent Skill，并重启已安装的后台服务。它不会修改 `~/.nectar/config.toml` 和数据。

```bash
nectar update --check          # 只检查最新版本
nectar update --version v0.6.0 # 指定版本或降级
nectar update --force          # 重装当前版本
nectar update --no-restart     # 更新二进制和 Skill，不重启服务
nectar update --no-skills      # 跳过 Skill 刷新
```

会话映射和最近一次上下文用量保存在 `~/.nectar/sessions.json`。重启后会继续 resume 原引擎会话；旧会话失效时，Nectar 会在最终卡片提示并启动新会话。

## 卸载

```bash
nectar uninstall                    # 服务、二进制、Skill 和 AGENTS.md 托管片段
nectar uninstall --purge-data       # 同时删除配置、任务数据、工作区和备份
nectar skills uninstall             # 只卸载 Agent Skill
```

`nectar uninstall` 默认保留配置和任务数据。常用参数：

```text
--keep-binary     保留二进制
--no-skills       不清理 Agent Skill 和 AGENTS.md 托管片段
--dry-run         预览清理项
--config PATH     指定配置文件路径
--binary PATH     指定二进制路径
```

如果安装时使用了自定义 `--dir` 或 `--config`，卸载时请通过 `--binary`、`--config` 传入相同路径。

## 从源码运行

需要 Go 1.26 或更高版本：

```bash
go build -o nectar .
mkdir -p ~/.nectar
[ -f ~/.nectar/config.toml ] || cp config.example.toml ~/.nectar/config.toml
./nectar
```

从源码运行时需要自己编辑 `~/.nectar/config.toml`，至少填入平台凭证。

返回：[中文 README](../../README.zh-CN.md) · [配置参考](config.md) · [后台服务](service.md)
