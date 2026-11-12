# 发布流程

发布由 [.github/workflows/release.yml](../../.github/workflows/release.yml) 完成。

## 首次发布

1. 确认仓库是 public。
2. 确认 GitHub Actions 已启用。
3. 如果 Release 创建失败，检查 Settings → Actions → General → Workflow permissions，选择 `Read and write permissions`。
4. 提交代码并推送 `main`。
5. 打 tag 并推送：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 工作流做什么

- 运行 `go test ./...` 和 `go vet ./...`
- 交叉编译：
  - `darwin/arm64`
  - `darwin/amd64`
  - `linux/amd64`
  - `linux/arm64`
- 生成 `nectar_<version>_<os>_<arch>.tar.gz`
- 生成 `checksums.txt`
- 创建 GitHub Release，并上传 tarball、checksums 和 `install.sh`

## 重跑发布

工作流检测到同名 Release 时会使用 `gh release upload --clobber` 覆盖资产。重新运行失败的 workflow 即可。

## 用户安装

默认安装最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh
```

固定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/zhouyg1992/nectar/main/install.sh | sh -s -- --version v0.1.0
```

也可以使用 Release 自带的安装脚本：

```bash
curl -fsSL https://github.com/zhouyg1992/nectar/releases/latest/download/install.sh | sh
```

## 版本

二进制版本通过 ldflags 注入：

```bash
-X main.version=v0.1.0
```

用户可以用 `nectar -version` 检查。
