---
okf_version: 1
description: Workloom 是独立于 Harness 的 Agent 开发基础设施，状态以文本保存在仓库内 `.devsys/`，通过 Git 在设备间交接。
---

# Workloom 知识库

Workloom 当前实现到 M7（含 M7.4）：`init`、`config check`、`search`、`workitem`、`doctor`、`recover`、`repair`、`workflow check/start/next/step-complete/pause/resume/cancel`、`approval list/request/approve/reject`、`decision / finding / event / artifact / run` 完整记录族、`context get/workitem/refresh/compact`、`context get --task <id>`（M5 叠加 `KnowledgeContext`）、`knowledge status/scan/validate/refresh`、`session start`、`wire`、`mcp serve`（基于官方 Go SDK、profile 过滤的 60+ 工具，含 `knowledge_status/validate/refresh`）、`worktree prepare/remove/list`、`dispatch --once|--watch|--dry-run`、`run exec|prompt|verify|complete|fail|cancel`、`workspace view [--json] [--limit N]` + `workspace build --static [--out DIR]`（M7.2 离线站）+ `workspace serve [--host 127.0.0.1] [--port N]`（M7.3 只读服务）+ 新鲜度提示（M7.4，stale/missing/unavailable/pending 各给跳转命令）；项目内 `.devsys/` 状态布局与严格配置校验，领域模型（WorkItem/事件流/Run/记录/不可变 Artifact 版本链/审批/工作流实例）、工作项九状态机与文件级领取租约、只读对账与摘要确认…

## 模块知识

- [项目接入与配置](knowledge/项目接入与配置/概述.md) — 命令入口、初始化边界、严格配置校验、Schema 与错误契约（含 M3 workflow/approval/next 的 kind 与 code 语义；M4 CLI 渲染与交换协议、AGENTS.md 写入；M5 `CodeStale=10` / `CodeMissing=11` reserved exit code、`codedExit{code}`、`knowledge_pages` / `knowledge_generator` 配置键、`Problem.Severity = "warning"`、`validate.go` 拆 `kindStrings`/`kindMilestones`；M6 执行命令族）｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · Schema与错误契约 · CLI渲染与交换协议 · AGENTS.md写入 · 执行命令与DEVSYS_PROJECT_ROOT
- [可靠文本存储](knowledge/可靠文本存储/概述.md) — 原子写、JSONL、锁、事务恢复、领域持久化、M2 状态机/领取/对账修复、M3 工作流策略/门禁/质量门/审批/next 与工作流实例/Guard/记录传播/LKG/AppendBatchTx/Staged、M4 读快照与守卫更新（record/update.go + run/ReadSnapshot）与 ErrSuperseded 线性版本链；**M5** `internal/storage.LockFile`（OS 独占锁原语，被知识层 `LockRefresh` 复用，方案 §15.2 同源）；M6 运行事件流｜卡：概述 · 架构设计 · 领域记录与事件 · 事务与恢复 · 存储错误语义 · 状态机与调度 · 对账与修复 · 编码规范 · 特殊配置与命令 · 读快照与守卫更新 · 运行事件流
- [共享应用与MCP](knowledge/共享应用与MCP/概述.md) — M4 共享应用服务 `internal/app`（错误分类 + Session/Context/Project/Workitem/Workflow/Approval/Decision/Finding/Artifact/Event/Run/Next/Wire/KnowledgeStatus）与 MCP 服务 `internal/mcp`（官方 Go SDK、profile 过滤的 60+ 工具）；**M5** `KnowledgeStatus/Scan/Validate/Refresh` 四方法 + `KindKnowledge` 错误类 + `knowledge_*` 三工具 + `context_for_workitem` paths 参数 + `context get --task` 三阶段选页；M6 执行命令族 + 完成校验与人工复核｜卡：概述 · 架构设计 · 工具与Profile · 错误语义 · 执行命令族 · 完成校验与人工复核
- [执行层](knowledge/执行层/概述.md) — M6 执行层（harness/workspace/dispatch/retry/prompt）五子包 + 七族 CLI 命令 + MCP 五工具 + `DEVSYS_PROJECT_ROOT` 注入 + §15.4 退避 + §4.8 阶段事件 + 适配器协议契约（Shell/Codex/OpenCode/Claude）｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · 适配器协议契约
- [知识层](knowledge/知识层/概述.md) — **M5** 知识层（page 契约与 front matter 校验 / 索引快照与 `.gitignore` 匹配器 / 新鲜度与三层基线 + 0/10/11 退出码 / 人工保护 + `--force` / 生成器契约 argv + 不走 shell / 断点续跑与 `run.json` + OS 锁 / `internal/app.Service.Knowledge*` + CLI/MCP 双入口 / `context get --task` 装配）｜卡：概述 · 架构设计 · 页面契约与校验 · 索引快照与匹配器 · 新鲜度与基线 · 人工保护 · 生成器契约与适配器 · 断点续跑与锁 · 知识服务接线 · 上下文装配 · 上下文快照
- [视图层](knowledge/视图层/概述.md) — **M7** 视图层（方案 §17）：`internal/view` 只读聚合（`Build` 装配 `Model`）+ `internal/sitestatic` 渲染（`Build` 落离线站 / `RenderPage` 供 serve）+ `workspace view/build/serve` + 新鲜度提示（`freshnessHint` 纯渲染）；`storage.Inspect`（既有锁文件上的共享锁，绝不创建）→ 缺锁文件时 `InspectUnlocked` 降级并标 `advisory_unlocked`；pending 事务按 §15.4 不渲染业务事实；无墙钟字段、零写入、每节带来源｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · 视图数据模型 · 读取纪律与信任语义

## 文章

- [项目总览](content/项目总览.md)
- [快速开始](content/快速开始.md)
- [开发与故障诊断](content/开发与故障诊断.md)
