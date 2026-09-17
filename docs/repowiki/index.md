---
okf_version: 1
description: Workloom 是独立于 Harness 的 Agent 开发基础设施，状态以文本保存在仓库内 `.devsys/`，通过 Git 在设备间交接。
---

# Workloom 知识库

Workloom 当前实现到 M0.4：`devsys` 命令行（`init`、`config check`）、项目内 `.devsys/` 状态布局、严格配置校验，以及可恢复的文本存储基元。语言 Go 1.26，唯一外部依赖 `gopkg.in/yaml.v3` 已 vendored，支持离线构建。后续里程碑（领域类型、工作流、执行等）尚未实现，Wiki 内容以当前源码为准。

## 模块知识

- [项目接入与配置](knowledge/项目接入与配置/概述.md) — 命令入口、初始化边界与严格配置校验｜卡：概述 · 架构设计 · 技术栈 · 编码规范 · 特殊配置与命令 · Schema与错误契约
- [可靠文本存储](knowledge/可靠文本存储/概述.md) — 原子写、JSONL、锁、事务与恢复协议｜卡：概述 · 架构设计 · 事务与恢复 · 存储错误语义 · 编码规范

## 文章

- [项目总览](content/项目总览.md)
- [快速开始](content/快速开始.md)
- [开发与故障诊断](content/开发与故障诊断.md)
