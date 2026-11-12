# 开发与测试

## 环境

- Go 1.22+
- Codex CLI 或 OpenCode CLI（真实端到端测试需要）
- 飞书自建应用（真实端到端测试需要）

## 常用命令

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
go run . -config ~/.nectar/config.toml
```

## 目录结构

```text
main.go                     # 依赖组装、启动飞书
config/                     # TOML 配置
internal/platform/          # 平台适配器
internal/engine/            # 引擎适配器
internal/core/              # 路由、会话、队列、卡片
internal/memory/comb/       # 教训日志
internal/exec/              # 工作目录、输出限制
internal/service/           # launchd / systemd 服务管理
install.sh                  # 一键安装脚本
```

## 测试

单元测试覆盖：

- Codex JSONL 解析与参数组装
- Codex 适配器真实子进程（用假 shell 脚本）
- OpenCode JSONL 解析与参数组装
- OpenCode 适配器真实子进程（用假 shell 脚本）
- Router 会话复用、FIFO 队列、卡片更新、教训记录
- 飞书消息规范化与 dispatcher 接线
- 教训文件存储、上下文窗口解析
- launchd plist / systemd unit 模板渲染

不需要真实飞书应用或引擎登录即可运行大部分测试。

## 本地模拟 Release

可以本地验证 `install.sh` 的下载、校验和解压流程：

```bash
rm -rf /tmp/nectar-release /tmp/nectar-install-test
mkdir -p /tmp/nectar-release/pkg

CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-X main.version=v0.0.1" \
  -o /tmp/nectar-release/pkg/nectar .

cp config.example.toml README.md README.zh-CN.md LICENSE /tmp/nectar-release/pkg/
cp -R docs /tmp/nectar-release/pkg/
tar -C /tmp/nectar-release/pkg -czf \
  /tmp/nectar-release/nectar_0.0.1_darwin_arm64.tar.gz .

(cd /tmp/nectar-release && \
  shasum -a 256 nectar_0.0.1_darwin_arm64.tar.gz > checksums.txt)

NECTAR_DOWNLOAD_BASE=file:///tmp/nectar-release \
  sh install.sh --version v0.0.1 --dir /tmp/nectar-install-test

/tmp/nectar-install-test/nectar -version
```

Linux 上把资产名和压缩包改成对应的 `linux_amd64` / `linux_arm64`，并用 `sha256sum` 生成校验和。

## 版本注入

发布构建通过 ldflags 注入版本：

```bash
go build -ldflags "-X main.version=v0.1.0" -o nectar .
./nectar -version
```

版本发布流程见 [release.md](release.md)。
