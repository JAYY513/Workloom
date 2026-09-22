# 更新日志

格式参照 Keep a Changelog；pre-release 阶段遵循语义化版本。发版步骤与回退见 [docs/发布流程.md](docs/发布流程.md)。

## Unreleased

### Fixed

- `workloom --help` 顶层命令表补回 `init` 与 `sync status`：两条命令一直可路由（`init` 是 `setup` 的第一步、`sync status` 是 M8.1 只读接力判定），但 e4f1a9e 重写命令表时漏列，按 INSTALL.md 分步接入的用户在 `--help` 里找不到入口。纯文案，无行为变更。

## v0.1.9 — 2026-09-22

### Added

- `workloom setup`：一条命令完成项目接入（init → 起手工作流 → wire → config check → wire --check → prime → 蓝图检查 → doctor），幂等、失败即停；已有文件一律保留。底层命令不变。
- `workloom mcp install`：把 devsys 注册进 MCP 客户端（Codex / Claude Code / OpenCode，user/project 两种 scope）。默认只预览；`--apply` 才写；已有 `devsys` 条目不覆盖，`--force` 才替换该条目。写入前打印目标路径。严格 JSON / TOML 外科式写入，无法安全合并时拒绝并指向 `wire --print-mcp`。
- npm 分发层：`@jayy513/workloom` 包装现有 Go 二进制（平台包走 optionalDependencies，无 postinstall 下载）。尚未 `npm publish`。npm bin 只有 `workloom`。Release 附带 `dist/npm/*.tgz`。

### Changed

- 主命令从 `devsys` 改名为 `workloom`：`go install github.com/JAYY513/Workloom/cmd/workloom@v0.1.9` 安装，用法、帮助、文档中的命令示例全部改为 `workloom …`。`devsys` 保留为同一程序的兼容别名（安装脚本同时装入两个名字），旧脚本与习惯用法不受影响。状态目录 `.devsys/`、MCP 注册名 `devsys`、skill 路径 `.agents/skills/devsys/`、`DEVSYS_*` 环境变量均不改名，既有接入项目无需迁移。

### Fixed

- 安装文档与安装脚本不再把本仓库写成私有仓库。默认 `go install` 不设 `GOPRIVATE`（那会跳过公共校验和数据库）；`gh auth login` 与 `GOPRIVATE` 只留给私有 fork。脚本下载顺序不变。Release 资产名改为 `workloom-*`（v0.1.8 及更早仍是 `devsys-*`）。

## v0.1.8 — 2026-09-21

### Added

- 开源许可证（MIT）、贡献指南与本更新日志。
- `docs/design.md`：活设计文档（架构原则、总体架构、状态布局、并发与安全要点），随代码演进。

### Changed

- Git 从入场券改为可选能力：项目身份是含 `.devsys/` 的目录，不是 git 根。`devsys init` 允许非 Git 目录与 git 子目录，禁止自动 `git init`；祖先已有 `.devsys/` 仍拒绝嵌套。
- 非 Git 项目使用目录工作区（非 worktree）；`run complete` 对该路径跳过 Git 证据（不把 `Advanced` 标 true）。Git + 已绑定 worktree 的完成门禁不变。
- `wire --check`：git 不在 PATH 时记为能力缺失，不把整次检查打成失败。
- 仓库裁剪：里程碑验收报告、一致性自检、开发记录与原始设计稿移出 HEAD（保留在 Git 历史中可溯源）；公开文档中的引用同步清理。
- `devsys config check` 对未知 `schema_version` 的错误文案不再内指内部实施计划文档。

### Fixed

- 生成的技能参考（`devsys wire --skill`）与 MCP core 档现状同步：计数 19→20，排除清单补 `run_cancel`。
- `workitem transition` 租约 fencing 报错的 token 出路改指本机侧车 `.devsys/local/leases/<id>.token`（scheduling yaml 自 v0.1.5 起不含 token）。


## v0.1.7 — 2026-09-21

### Added

- `go install` 直装支持：模块路径规范化 + 版本兜底。

### Fixed

- v0.1.6 复测观察两项。

## v0.1.6 — 2026-09-21

### Added

- `install.sh` / `install.ps1` 随 GitHub Release 资产发布；README 直装指引同步刷新。

## v0.1.5 — 2026-09-21

### Fixed

- v0.1.4 复测遗留修复两项。

## v0.1.4 — 2026-09-21

### Fixed

- 首次使用打磨批次 D（#338、#342）；MCP core 档 20 项工具。

## v0.1.3 — 2026-09-20

### Fixed

- 首启失败可见性与接入闭环（#336 P0、#337 P1）：claim 前预检、拒绝统一落账、dead / in-flight 尝试分类、蓝图工件校验、不可读受管状态 fail closed。

## v0.1.2 — 2026-09-20

### Fixed

- MCP core 档严格 19 项（run 终态工具拆分注册）。

## v0.1.1 — 2026-09-20

### Added

- GitHub Actions 发布工作流：tag / workflow_dispatch 触发 + 严格 `checksums.txt` 自检。
- `devsys wire --check|--skill|--print-mcp`、`devsys prime`（= `session start --compact`）、`mcp serve --tier`。
- `workitem create/update --acceptance`；审批列表显示有效状态（consumed / invalidated）。
- `config.yaml` 可选 `default_policy`：未绑定工作流的工作项按它过质量门 / 阶段门。

### Fixed

- `next` / `project status` 与 `claim` 共用同一组质量门判定（`quality_blocked`、`retry_pending` 风险可见并给补救命令）。
- 阶段门审批失效原因可见（因离开状态失效 + 重新申请出路）。
- 并发版本冲突报错补出路；安装链修复（回退构建进入克隆目录、校验和格式兼容）。
- 轮次提示词协议补 `--by`；README / 手册补「工作流从哪来」。

## v0.1.0 — 2026-09-20

### Added

- 首个 Release：6 平台矩阵构建 + GNU SHA-256 `checksums.txt` + `install.sh` / `install.ps1` + Makefile。
- `devsys prime`、`--latest` 便捷参数（与 `--expect` 互斥）、MCP core 档过滤。
- 子目录内运行命令：`DEVSYS_PROJECT_ROOT` 优先、向上回溯 `.devsys/`。
- `devsys wire` 增强：`--check` 内容比对、`--skill` 技能注入、`--print-mcp` 客户端片段。
