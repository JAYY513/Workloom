# 贡献指南

感谢关注 Workloom。提交改动前请通读本文件；由 Agent 执行的开发工作另见 [AGENTS.md](AGENTS.md)（wire 注入的纪律块与任务流水线）。

## 环境

- Go 1.26 与 Git；Windows / macOS / Linux 均可开发（发布矩阵覆盖 6 平台）。
- `make build`；`go test ./...`；`go vet ./...`；gofmt 格式化。
- 端到端剧本：`scripts/smoke-m*.{sh,ps1}`。

## 本地开发与正式版切换

作者可以直接复用 npm 全局安装目录，避免 Agent 同时看到 `workloom` 与另一个开发命令：

```powershell
npm install -g @kaki317/workloom
$npmRoot = npm root -g
$npmExe = Get-ChildItem -Path $npmRoot -Filter workloom.exe -Recurse -File |
  Where-Object FullName -match "workloom-win32-x64" |
  Select-Object -First 1 -ExpandProperty FullName

cd C:\Source\CodeSource\ai\Workloom
go test ./...
go build -o $npmExe .\cmd\workloom
workloom --version
```

这样开发版仍通过标准 `workloom` 命令和 MCP 使用。修改代码后重新 `go build -o $npmExe` 即可；关闭并重启 MCP 客户端以加载新进程。恢复正式版时重跑 `npm install -g @kaki317/workloom --force`。不要修改 npm wrapper 或 `package.json`，只替换平台二进制；npm 更新会覆盖本地开发版。

此方式仅用于作者本地 dogfood，不是普通用户的安装步骤；正式发布前仍须用干净环境验证 npm 包。

如果 npm 安装后提示平台包未安装，重跑 `npm install -g @kaki317/workloom --force --include=optional`；不要使用 `--omit=optional`。

## 提交前

1. 先阅读[设计文档](docs/design.md)的设计原则与边界约束。
2. 为行为变化补充高价值测试：优先覆盖行为、边界与回归，而不是追求覆盖率数字。
3. 保持 `.devsys/` 为唯一事实来源；不引入绕过应用服务的写路径。
4. 在 Pull Request 中说明：动机、兼容性影响、验证方式、回退路径。

## 提交规范

- Conventional Commits + 中文主题：`type(scope): 描述（#任务号）`，例如 `fix(first-run): …（#345）`。
- `.devsys/` 状态与代码分开提交：状态变更用 `chore(devsys): …` 前缀，便于回滚与审计。
- 提交前全量 `go test ./... -count=1`，`go vet` / gofmt 干净，工作区不残留生成物。

## 文档纪律

- `docs/repowiki/` 由 repowiki 生成并管理（人工内容只写在保护页或标记之外）；涉及代码理解的改动完成后运行 `repowiki status` 自查新鲜度（退出码 10 = 过期，需运行 repowiki 刷新再提交）。
- 面向使用者的文档：README（中/英）、[使用手册](docs/使用手册.md)、[迁移指南](docs/迁移指南.md)、[发布流程](docs/发布流程.md)、[CHANGELOG](CHANGELOG.md)。
- 开发过程性记录（里程碑报告、开发日志等）不进仓库，保留在本地私有目录。

## 行为变化与版本

- 用户可感知的行为变化在 [CHANGELOG](CHANGELOG.md) 的 Unreleased 节登记一条。
- 受管状态文件的 `schema_version` 变化必须走显式迁移（见[迁移指南](docs/迁移指南.md)），禁止隐式改写。

## 发布

tag 触发 `.github/workflows/release.yml`（6 平台矩阵 + 严格 checksums 自检 + `dist/npm/*.tgz` 附到 GitHub Release，并经 trusted publishing / OIDC 自动 `npm publish`，无任何长期凭据）；步骤、校验与回退见[发布流程](docs/发布流程.md)。

## 许可证

MIT，见 [LICENSE](LICENSE)。
