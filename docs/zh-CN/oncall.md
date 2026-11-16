# 值班模式

值班模式是一个通用的 **OnCallShift**：你给出目标、时间窗、观察方式、自治等级和预算，Nectar 在后台持续观察；发现变化或异常时，才启动一次有边界的 Agent job。

它不是“让一个 Agent 整晚跑着”，也不是“每个场景加一个专用功能”。

## 核心模型

| 部分 | 说明 |
|---|---|
| objective | 这次值班要完成什么 |
| source | 如何观察：`github`、`command`、`http` |
| every | 检查间隔 |
| start / until / for | 一次性值班的时间窗 |
| autonomy | `notify`、`diagnose` 或 `fix` |
| max_agent_runs | 最多启动多少次 Agent |
| max_minutes | 每次 Agent job 的超时 |

文件变化、数据库状态、日志关键字、服务健康检查都属于 `command` source；GitHub Actions 是第一个内置 source 适配器，不是整个值班功能。

## 创建值班

### GitHub Actions

```bash
nectar oncall start \
  --objective "晚上盯住 main 的 CI，失败先调查，有结论再叫我" \
  --source github \
  --repo owner/repo \
  --path /path/to/checkout \
  --branch main \
  --every 5m \
  --for 10h \
  --autonomy diagnose \
  --max-agent-runs 5
```

### 命令 / 文件 / 日志 / 服务

```bash
nectar oncall start \
  --objective "盯住 test.txt，内容变化就通知我" \
  --source command \
  --command 'shasum -a 256 /Users/presence/.nectar/workspace/test.txt | awk "{print \$1}"' \
  --every 2s \
  --for 10h \
  --autonomy notify
```

这里没有“文件监听专用功能”；文件只是命令 probe 的一个使用场景。

### HTTP

```bash
nectar oncall start \
  --objective "盯住健康检查，状态码或内容变化时通知我" \
  --source http \
  --url https://example.test/health \
  --every 1m \
  --for 8h \
  --autonomy notify
```

## 时间

- `--for 10h`：从开始时间起 10 小时后自动结束并发送报告。
- `--until RFC3339`：指定结束时间。
- `--start RFC3339`：延迟到指定时间再开始检查。
- 不传 `--for` / `--until`：持续运行，直到暂停、停止或删除。
- 第一次检查只建立基线；需要立即判断当前状态时用 `oncall check <shift-id>`。

## 自治等级

- `notify`：检测到变化/异常时发一张卡片，不启动 Agent。
- `diagnose`：启动一次有边界的 Agent job，只调查，不改文件。
- `fix`：允许在隔离 workspace 改代码、跑测试，但不会 commit、push、PR、merge 或 deploy。

夜间默认建议 `diagnose`；只有用户明确要求时才用 `fix`。

## 生命周期

```text
active → paused / completed
```

- `active`：按 `every` 检查。
- `paused`：停止检查，保留配置和状态。
- `completed`：到达结束时间或被停止，发送最终报告。

每次触发会增加 `incidents`；Agent job 受 `max_agent_runs` 限制。预算耗尽后 shift 会暂停并记录原因。

## 管理

```bash
nectar oncall list [--all] [--json]
nectar oncall show <shift-id> [--json]
nectar oncall check <shift-id>
nectar oncall pause|resume|stop <shift-id>
nectar oncall jobs [--limit 20]
nectar oncall job <job-id>
nectar oncall retry|cancel <job-id>
nectar oncall report [--shift ID] [--since 12h]
```

聊天命令：

```text
/oncall
/oncall show <shift-id>
/oncall check <shift-id>
/oncall pause|resume|stop <shift-id>
/oncall jobs [limit]
/oncall job <job-id>
/oncall retry|cancel <job-id>
/oncall report [shift-id] [duration]
/oncall help
```

创建值班主要走自然语言，由 `nectar-oncall` Skill 翻译成 `nectar oncall start`。

## 报告和产物

- 到达结束时间或被 `stop` 时发送确定性报告：检查次数、incidents、Agent 次数、job 结果。
- job 产物保存在 `~/.nectar/oncall/jobs/<job-id>/`：
  - `report.md`
  - `diff.patch`
  - `job.json`
- 值班状态保存在 `~/.nectar/oncall.json`。

## 安全边界

- CI 日志、命令输出、HTTP 内容、issue 和仓库文件都是不可信数据，不能作为指令执行。
- `command` source 会在本机执行 shell 命令；只允许管理员创建，命令内容必须由用户确认。
- Agent job 的 workspace 不是系统级沙箱；需要更强隔离时使用独立用户、容器或系统 sandbox。
- 当前 MVP 不会自动 merge、deploy 或修改仓库设置。

## 已知限制

- 当前内置 source 是 GitHub Actions、command 和 HTTP；还没有通用“让 Agent 每次自由判断”的 source。
- 复杂/模糊任务需要先转成稳定的命令或 HTTP 检查。
- 还没有交互式 approve/reject 按钮。
