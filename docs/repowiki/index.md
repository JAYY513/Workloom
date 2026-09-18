---
okf_version: 1
description: Workloom 是独立于 Harness 的 Agent 开发基础设施，状态以文本保存在仓库内 `.devsys/`，通过 Git 在设备间交接。
---

# Workloom 知识库

Workloom 当前实现到 M2：`init`、`config check`、`search`、`workitem`、`doctor`、`recover`、`repair`，项目内 `.devsys/` 状态布局与严格配置校验，领域模型（WorkItem/事件流/Run/记录/不可变 Artifact 版本链）、工作项九状态机与文件级领取租约、只读对账与摘要确认修复，以及可重复冒烟剧本。工作流门禁与执行调度不在本轮交付范围。Go 1.26，唯一外部依赖 `gopkg.in/yaml.v3` 已 vendored；Wiki 以当前源码为准。

## 模块知识

- [项目接入与配置](knowledge/项目接入与配置/概述.md) — 命令入口、初始化边界与严格配置校验｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · Schema与错误契约
- [可靠文本存储](knowledge/可靠文本存储/概述.md) — 原子写、JSONL、锁、事务恢复、领域持久化与 M2 状态机/领取/对账修复｜卡：概述 · 架构设计 · 领域记录与事件 · 事务与恢复 · 存储错误语义 · 状态机与调度 · 对账与修复 · 编码规范 · 特殊配置与命令

## 文章

- [项目总览](content/项目总览.md)
- [快速开始](content/快速开始.md)
- [开发与故障诊断](content/开发与故障诊断.md)
