# 值班模式

值班模式让 Nectar 在指定时间窗内轮询 GitHub Actions。分支出现新的失败后，Nectar 会创建一个隔离 job：从失败 commit 创建独立 git worktree，让当前引擎调查（或修复），保存报告和 diff，并把结果卡片发回聊天。

它不是另一种 cron。定时任务执行的是固定 prompt；值班 job 有独立状态、证据、产物、重试和可恢复记录。

## 前置条件

- 本机安装并登录 `gh` CLI，且对目标仓库有读取 Actions 的权限。
- 目标仓库有本地 git checkout，并且 `origin` 指向 GitHub；也可以在创建时显式传 `--repo`。
- 至少安装并登录一个 Nectar 支持的引擎。

## 配置

值班模式默认开启。以下均为可选覆盖：

```toml
[oncall]
enabled = true
default_every = "5m"
window = ""                    # 例如 "22:00-08:00"
autonomy = "diagnose"          # diagnose | fix
max_minutes = 20
max_attempts = 2
max_concurrent = 1
```

- `default_every` 是新建 watch 的默认轮询间隔。
- `window` 是默认值班时间窗，支持跨午夜，例如 `22:00-08:00`。
- `autonomy = "diagnose"` 只调查，不改文件；`"fix"` 允许在隔离 worktree 改代码和跑测试，但不会 commit、push、开 PR、merge 或 deploy。
- `max_minutes` 是单个 job 的超时。
- `max_attempts` 限制手动重试次数。
- `max_concurrent` 限制同时执行的 job 数。

## 创建 watch

在聊天里：

```text
/oncall watch
/oncall watch owner/repo main
```

当前聊天的工作区必须是一个 git checkout。也可以先让 Agent 用自然语言创建，`nectar-oncall` Skill 会把意图翻译成 CLI。

在终端里：

```bash
nectar oncall watch add \
  --repo owner/repo \
  --path /path/to/checkout \
  --branch main \
  --every 5m \
  --window 22:00-08:00 \
  --autonomy diagnose \
  --channel feishu \
  --chat oc_xxx
```

参数：

| 参数 | 说明 |
|---|---|
| `--repo` | `owner/name`；为空时从 `origin` 推断 |
| `--path` | 本地 checkout；默认 `NECTAR_WORKING_DIR` 或当前目录 |
| `--branch` | 要监控的分支；为空时从 git 推断 |
| `--every` | 轮询间隔，默认 `5m` |
| `--window` | 值班时间窗，例如 `22:00-08:00` |
| `--autonomy` | `diagnose` 或 `fix` |
| `--max-minutes` | 单 job 超时 |
| `--max-attempts` | 最大手动重试次数 |
| `--chat` | 结果目标 chat；Agent 会话中自动读取 `NECTAR_CHAT_ID` |

第一次检查会忽略创建 watch 之前的历史失败，不会把它当成新事故。要立即调查当前已经失败的 run，执行：

```bash
nectar oncall watch check <watch-id>
```

## 命令

```bash
nectar oncall watch list [--all] [--json]
nectar oncall watch pause|resume|check|delete <watch-id>
nectar oncall jobs [--limit 20] [--json]
nectar oncall job <job-id> [--json]
nectar oncall retry <job-id>
nectar oncall cancel <job-id>
nectar oncall report [--since 12h] [--json]
```

聊天命令：

```text
/oncall
/oncall list
/oncall watch [repo] [branch]
/oncall check|pause|resume|delete <watch-id>
/oncall jobs [limit]
/oncall job <job-id>
/oncall retry|cancel <job-id>
/oncall report [duration]
/oncall help
```

管理命令仅限 `bot.admin_users`；该列表为空时不限制管理员。

## Job 生命周期

```text
queued → running → completed / failed / canceled
```

- 每个 job 从失败 commit 创建独立 `git worktree`，不污染当前聊天工作区。
- `diagnose` job 的 prompt 明确要求只调查、不改文件。
- `fix` job 可以在 worktree 修改文件、运行测试，但仍然不能提交、推送、合并或部署。
- job 结束后保存 `report.md`、`diff.patch` 和 `job.json` 到 `~/.nectar/oncall/jobs/<job-id>/`。
- Nectar 重启时，未完成的 job 会标记为失败，避免把已消失的引擎进程当成仍在运行。

## 安全边界

- CI 日志、issue 正文、commit message 和仓库内容都按不可信数据处理；其中的指令不能覆盖 Nectar 的 job prompt。
- 工作目录不是沙箱边界。需要更强隔离时，使用独立 Unix 用户、容器或系统级 sandbox。
- `fix` 模式仍受引擎自身的 sandbox 和配置约束。
- 当前 MVP 不自动 merge、deploy 或修改仓库设置。
- `gh` 和仓库权限决定 Nectar 能读取什么；不要给 bot 超出值班需要的权限。

## 已知限制

- 只支持 GitHub Actions 失败轮询；暂不支持 Sentry、Prometheus 或 GitHub webhook。
- 结果目前以最终卡片的文字和 diff 为主，尚未接入交互式 approve/reject 按钮。
- 不自动判断 flaky test；需要在报告或后续迭代中人工确认。
