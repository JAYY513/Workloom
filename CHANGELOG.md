# 更新日志

格式参照 Keep a Changelog；pre-release 阶段遵循语义化版本。发版步骤与回退见 [docs/发布流程.md](docs/发布流程.md)。

## Unreleased

### Added

- 开源许可证（MIT）、贡献指南与本更新日志。
- `docs/design.md`：活设计文档（架构原则、总体架构、状态布局、并发与安全要点），随代码演进。

### Changed

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
