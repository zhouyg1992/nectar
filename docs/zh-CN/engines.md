# 引擎安装与登录验证

Nectar 不要求安装所有引擎。任选一个编程 Agent CLI，安装、登录，并确认它能在非交互模式下正常返回结果即可。

安装器会检测本机已安装的引擎；如果一个都没找到，会提示你先安装。

## 选择引擎

| 引擎 | Nectar 配置段 | 适用场景 |
|---|---|---|
| Codex CLI | `[codex]` | 默认选项，支持模型目录和上下文自动解析 |
| Claude Code | `[claude]` | 已使用 Claude Code 或需要 fallback model |
| Cursor Agent CLI | `[cursor]` | 已使用 Cursor 订阅或 Agent CLI |
| OpenCode | `[opencode]` | 使用 OpenCode 的 provider / model 体系 |
| Pi | `[pi]` | 使用 Pi 的 provider 和 thinking 配置 |

切换引擎时修改 `~/.nectar/config.toml`：

```toml
[bot]
engine = "codex"   # codex | opencode | cursor | claude | pi
```

然后重启服务：

```bash
nectar service restart
```

## Codex CLI

安装 Codex CLI 并完成登录，确认下面的命令可以正常运行：

```bash
codex exec --json --skip-git-repo-check "reply with OK"
```

Nectar 通过 `codex exec --json` 执行任务，并用 `resume` 续接会话。模型、推理强度、sandbox 和工作目录在 `[codex]` 中配置。

如果服务环境找不到 `codex`，把 `[codex].path` 设置为绝对路径。

## Claude Code

安装并登录 Claude Code：

```bash
claude auth login
claude -p --permission-mode plan "Reply with exactly OK"
```

Nectar 通过 `claude -p --output-format stream-json --verbose --include-partial-messages` 执行任务，通过 stdin 传入 prompt，并用 `--resume` 续接会话。

`auto_approve = true` 会映射为 `--dangerously-skip-permissions`，适合无人值守的 bot 场景；关闭时使用 `permission_mode`。

## Cursor Agent CLI

安装 Cursor Agent：

```bash
curl https://cursor.com/install -fsS | bash
```

官方安装脚本会同时提供 `agent`（主命令）和 `cursor-agent`（兼容名）。Nectar 和安装器优先使用 `agent`，找不到时回退 `cursor-agent`，不需要手动创建软链接。

确保 `~/.local/bin` 在 `PATH` 中，然后验证：

```bash
agent login
agent -p --trust --mode ask "Reply with exactly OK"
```

Nectar 使用 `-p --output-format stream-json --stream-partial-output`，并用 `--resume` 续接会话。`auto_approve` 映射为 `-f`，`trust_workspace` 映射为 `--trust`；无头运行两者都需要。

## OpenCode

安装 OpenCode，配置好 provider / model，并确认下面的命令可以正常运行：

```bash
opencode run --format json "Reply with exactly OK"
```

Nectar 通过 `opencode run --format json` 执行任务，并用 `--session` 续接会话。`model` 为空时会尝试从 `~/.config/opencode/opencode.json` 读取默认模型。

## Pi

安装并登录 Pi，确认下面的命令可以正常运行：

```bash
pi auth check --provider anthropic
pi --mode json "Reply with exactly OK"
```

Nectar 通过 `pi --mode json` 执行任务，通过 stdin 传入 prompt，并用 `--session` 续接会话。`provider`、`model` 和 `thinking` 会直接透传为 Pi 参数。

## 常见问题

- **`command not found`**：确认引擎 CLI 在当前 shell 的 `PATH` 中；服务模式下建议在配置里使用绝对路径。
- **登录失效**：先在终端重新登录对应 CLI，再重启 Nectar 服务。
- **一直停在 Working**：用上面的验证命令手动运行一次，确认不是引擎本身在等待审批、模型或网络。
- **权限不足**：Cursor 需要 `--trust`，Claude 需要 `auto_approve` 或合适的 `permission_mode`，Pi 需要 `trust_workspace`。

返回：[中文 README](../../README.zh-CN.md) · [配置参考](config.md) · [命令参考](commands.md)
