---
okf_version: 1
description: Workloom 是独立于 Harness 的 Agent 开发基础设施，状态以文本保存在仓库内 `.devsys/`，通过 Git 在设备间交接。
---

# Workloom 知识库

Workloom 当前实现到 M4：`init`、`config check`、`search`、`workitem`、`doctor`、`recover`、`repair`、`workflow check/start/next/step-complete/pause/resume/cancel`、`approval list/request/approve/reject`、`decision / finding / event / artifact / run` 完整记录族、`context get/workitem/refresh/compact`、`knowledge status`、`session start`、`wire`、`mcp serve`（基于官方 Go SDK、profile 过滤的工具族）；项目内 `.devsys/` 状态布局与严格配置校验，领域模型（WorkItem/事件流/Run/记录/不可变 Artifact 版本链/审批/工作流实例）、工作项九状态机与文件级领取租约、只读对账与摘要确认修复、工作流策略解析与门禁/质量门/last-known-good 缓存、工作流实例操作、审批生命周期与转换事务内的消费、就绪判定与 §7.4 优先级推荐、共享应用服务（CLI 与 MCP 双入口共用）、MCP profile 过滤的 60+ 工具族、`--json` / `--jsonl` 结构化输出与 `devsys wire` 的 AGENTS.md 幂等写入。知识（M5）与执行调度（M6）未实现，`config.yaml` 仍只有 `schema_version`。Go 1.26，依赖 `gopkg.in/yaml.v3` 与 `github.com/modelcontextprotocol/go-sdk v1.8.0`（均 vendored）。

## 模块知识

- [项目接入与配置](knowledge/项目接入与配置/概述.md) — 命令入口、初始化边界、严格配置校验、Schema 与错误契约（含 M3 workflow/approval/next 的 kind 与 code 语义；M4 CLI 渲染与交换协议、AGENTS.md 写入）｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · Schema与错误契约 · CLI渲染与交换协议 · AGENTS.md写入
- [可靠文本存储](knowledge/可靠文本存储/概述.md) — 原子写、JSONL、锁、事务恢复、领域持久化、M2 状态机/领取/对账修复、M3 工作流策略/门禁/质量门/审批/next 与工作流实例/Guard/记录传播/LKG/AppendBatchTx/Staged、M4 读快照与守卫更新（record/update.go + run/ReadSnapshot）与 ErrSuperseded 线性版本链｜卡：概述 · 架构设计 · 领域记录与事件 · 事务与恢复 · 存储错误语义 · 状态机与调度 · 对账与修复 · 编码规范 · 特殊配置与命令 · 读快照与守卫更新
- [共享应用与MCP](knowledge/共享应用与MCP/概述.md) — M4 共享应用服务 `internal/app`（错误分类 + Session/Context/Project/Workitem/Workflow/Approval/Decision/Finding/Artifact/Event/Run/Next/Wire/KnowledgeStatus）与 MCP 服务 `internal/mcp`（官方 Go SDK、profile 过滤的 60+ 工具）｜卡：概述 · 架构设计 · 工具与Profile · 错误语义

## 文章

- [项目总览](content/项目总览.md)
- [快速开始](content/快速开始.md)
- [开发与故障诊断](content/开发与故障诊断.md)
