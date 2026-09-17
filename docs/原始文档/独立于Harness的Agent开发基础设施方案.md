# 独立于 Harness 的 Agent 开发基础设施方案

**文档版本：** 1.7  
**文档状态：** 方案设计稿  
**本版要点：** 保留无数据库路线，明确 CLI/MCP 访问纪律、本机文件事务与恢复、跨设备接力边界；同步补充实施验收，不改变里程碑顺序。  
**上一版要点：** 新增配套《实施计划.md》（M0–M9、52 步），§19 增加指向实施计划的引用。  
**目标：** 构建一个独立运行、可被多个 Harness Agent 调用的智能开发系统

**参考项目：**

| 项目 | 定位 | 地址 |
|---|---|---|
| BMAD | Breakthrough Method for Agile AI Driven Development（规划与开发方法论） | <https://github.com/bmad-code-org/BMAD-METHOD> |
| RepoWiki | 面向 Agent 的项目知识 Wiki 生成与保鲜流水线 | <https://github.com/JAYY513/repowiki> |
| Contrabass | AI 编码 Agent 的项目级编排器（Symphony 的 Go 实现） | <https://github.com/junhoyeo/contrabass> |
| Symphony | OpenAI 的自主实现运行编排设计（Elixir 参考实现） | <https://github.com/openai/symphony> |
| agent-tasks | 面向 AI 编码 Agent 的流水线任务管理与实时看板（MCP / REST / WebSocket）；默认状态存于用户级 `~/.agent-tasks/agent-tasks.db`，本系统不采用该存储模式 | <https://github.com/keshrath/agent-tasks> |

**本地克隆（阅读与核对用）：** 根目录 `C:\Source\CodeSource\github-clone\`

| 项目 | 本地目录 | HEAD | 拉取日期 |
|---|---|---|---|
| BMAD | `BMAD-METHOD` | `0a000534` | 2026-09-15 |
| RepoWiki | `repowiki-jayy513` | `5bfe1f3` | 2026-09-12 |
| Contrabass | `contrabass` | `5fc728b` | 2026-07-17 |
| Symphony | `symphony` | `be10a1b` | 2026-09-15 |
| agent-tasks | `agent-tasks` | `2b421ca` | 2026-04-15 |

克隆方式（按需替换代理地址）：

```powershell
$env:HTTP_PROXY  = "http://127.0.0.1:10808"
$env:HTTPS_PROXY = "http://127.0.0.1:10808"
git clone https://github.com/bmad-code-org/BMAD-METHOD.git
git clone https://github.com/JAYY513/repowiki.git repowiki-jayy513
git clone https://github.com/junhoyeo/contrabass.git
git clone https://github.com/openai/symphony.git
git clone https://github.com/keshrath/agent-tasks.git
```

注意：`JAYY513/repowiki` 的本地目录命名为 `repowiki-jayy513`，避免与已存在的 `RepoWiki/`（he-yufeng/RepoWiki）在 Windows 上发生大小写冲突。

---

## 1. 项目概述

### 1.1 项目定位

本项目是一套独立于具体 Harness 的 Agent 开发基础设施。

它不属于 Codex、OpenCode、Claude Code、Gemini CLI 或某一个特定 Agent，也不要求 Agent 必须运行在某个专用编排器内部。

系统通过标准化接口向外部 Agent 提供：

- 项目蓝图和长期状态；
- 项目目标、范围、约束和里程碑；
- 动态任务管理；
- 任务依赖和状态流转；
- 可配置工作流；
- 代码知识和项目上下文；
- 决策、错误、发现和经验记录；
- Agent 执行记录；
- 产物管理；
- 多种 Harness 接入能力。

核心定位可以概括为：

> 一个面向多个 Harness Agent 的独立开发操作系统，为 Agent 提供统一的项目状态、任务编排、知识上下文和执行记录。

---

### 1.2 解决的问题

当前使用多个 AI 编程 Agent 时，通常存在以下问题：

1. 不同 Harness 各自保存上下文，换 Agent 后容易丢失历史。
2. 任务分散在聊天记录、Markdown、Issue、临时清单中。
3. 项目目标、架构决策、任务状态和代码实际情况容易不一致。
4. BMAD 等方法论可以规划，但不一定负责长期任务管理和跨 Agent 执行。
5. Contrabass/Symphony 类工具擅长执行编排，但不一定提供完整的项目知识和产品规划底座。
6. RepoWiki 类工具可以生成代码知识，但通常不是完整的任务和工作流系统。
7. 不同系统之间容易出现重复任务、重复拆分和多套状态真相。
8. 固定流水线无法很好处理真实开发中的调查、返工、架构变化、测试失败和临时决策。
9. 常见工具把任务和运行状态集中存放在用户级公共数据库中，项目之间相互混杂，换电脑或换项目目录后状态无法随仓库同步。

---

### 1.3 设计目标

#### 核心目标

- 独立运行，不依赖任何单一 Harness。
- 任意支持 MCP、CLI 或 HTTP 的 Agent 都可以接入。
- 支持动态创建任务，而不是只能执行固定任务列表。
- 支持长期项目状态和跨会话上下文恢复。
- 项目状态保存在项目仓库内（`.devsys/`），可随 Git 在不同电脑之间同步。
- 项目之间彼此隔离，不依赖跨项目共享的任务数据库。
- 支持可配置、可调整的工作流。
- 支持代码知识按需检索，而不是每次加载完整文档。
- 支持记录成功、失败、错误、发现、决策和后续行动。
- 支持多种 Agent 和 Harness 并行工作。
- 支持人工参与、审批、暂停和恢复。
- 保持核心数据模型稳定，外部工具通过适配器接入。

#### 非目标

第一阶段不追求：

- 自己训练或托管大模型；
- 自己实现完整 IDE；
- 替代所有 Harness 的终端和编辑器；
- 一开始支持所有编程语言；
- 一开始实现复杂的企业级项目管理功能；
- 建立跨项目共享的任务或运行状态数据库（状态必须随项目仓库走）；
- 一开始复制 BMAD、RepoWiki、Contrabass、Symphony、agent-tasks 的全部功能；
- 将所有能力都设计成 Skill；
- 强制所有项目使用同一套开发流程。

---

## 2. 设计原则

### 2.1 Harness 解耦

系统的核心服务不能依赖某个 Harness 的内部机制。

错误方式：

```text
Codex Skill
  -> 直接修改某个 Harness 专用任务文件
  -> 依赖 Codex 会话上下文
```

正确方式：

```text
Codex / OpenCode / Claude Code / 其它 Agent
  -> MCP / CLI / HTTP
  -> 独立开发系统
  -> 统一数据和工作流
```

---

### 2.2 Skill 不是核心系统

Skill 只负责帮助 Agent 理解如何使用系统，不负责保存核心状态。

核心能力必须由独立服务提供：

- 项目查询；
- 任务创建和更新；
- 工作流执行；
- 知识检索；
- 运行记录；
- 产物登记；
- 决策和发现记录。

Skill 是可选的使用说明和行为约束。

---

### 2.3 单一事实来源

不同类型的信息必须有明确的权威来源：

| 信息类型 | 权威来源 |
|---|---|
| 项目目标和范围 | 项目蓝图 |
| 产品需求和验收标准 | 规格文档 |
| 架构和技术决策 | 架构决策记录 |
| 任务状态 | 统一任务系统 |
| Agent 执行状态 | Run 运行记录 |
| 代码实际结构 | 代码知识索引 |
| 错误、发现和经验 | 项目记录系统 |
| 生成的文档和报告 | 产物系统 |

不能让多个系统同时成为同一类信息的“最终真相”。

---

### 2.4 记录与推理分离

Agent 可以提出建议、判断和计划，但系统应区分：

- Agent 的建议；
- 已确认的事实；
- 已批准的决策；
- 已验证的结果；
- 尚未验证的推测。

例如：

```text
Agent 建议：可能需要修改协议抽象层
状态：待确认

架构决策：确认新增协议适配器接口
状态：已批准

实施结果：已完成接口和单元测试
状态：已验证
```

---

### 2.5 动态工作流优先

不把所有项目强制限制为：

```text
需求 -> PRD -> 架构 -> Epic -> Story -> Sprint -> 开发
```

系统应根据任务和项目状态动态决定下一步：

```text
用户目标
  -> 读取项目状态
  -> 判断当前缺失信息
  -> 生成下一步工作
  -> 执行
  -> 验证
  -> 根据结果继续、返工、暂停或创建新任务
```

工作规模决定流程深度（采纳 BMAD 的右尺寸原则，规模不同但实现单元相同）：

| 规模 | 判定依据 | 流程 |
|---|---|---|
| 微小改动 | 低风险、意图明确 | 直接实施，不走完整规划 |
| 单会话 | 一个实现会话能完成 | 规格 → 实施 → 验证 |
| Epic | 多个会话、单一目标 | 规格 → 拆分工作项 → 逐项实施 → 整体验证 → 回顾 |
| 项目 | 多个 Epic 或约 20 个以上会话 | 蓝图与规格 + 架构决策 + 多个 Epic 循环 |

规划产物可以随规模升级，但实现单元不变：任何规模最终都落到「一个工作项 + 一次 Run + 一次验证」。不允许因为走了流程就强制小改动先写 PRD 或 Epic。

---

### 2.6 核心服务与执行器分离

系统负责：

- 决定做什么；
- 记录为什么做；
- 管理任务和依赖；
- 管理工作流；
- 管理上下文；
- 管理执行状态；
- 管理验证结果。

Harness 负责：

- 读取上下文；
- 修改代码；
- 执行命令；
- 运行测试；
- 返回结果。

---

### 2.7 项目内状态（Project-Local State）

项目状态属于项目本身，而不是某个工具的用户目录。

- 所有项目级数据（蓝图、任务、Run、决策、发现、事件、产物元数据）写入项目仓库内的 `.devsys/`；
- 状态文件使用文本格式（YAML / Markdown / JSONL），可 diff、可 review、可合并；
- 通过 Git 提交与拉取在不同电脑之间同步状态，不需要额外的同步服务；
- 不建立跨项目公共数据区：任务、运行和记录不写入用户级数据库（`~/.agent-tasks/agent-tasks.db` 这类模式明确不采用）；
- 用户级目录只允许保存三类内容：项目路径注册表、凭证引用、与项目语义无关的缓存；
- 不使用数据库：查询直接扫描项目内文本文件，任何缓存都必须可删除并自动重建（理由见 §14.1）；
- 换电脑流程即：`git clone` → `devsys init` → 状态、任务与记录完整恢复。

---

## 3. 总体架构

```text
┌─────────────────────────────────────────────────────────┐
│                    外部使用者                           │
│                                                         │
│  Codex   OpenCode   Claude Code   Gemini CLI   自定义Agent │
└───────────────────────┬─────────────────────────────────┘
                        │
          MCP / CLI / HTTP API / 文件协议
                        │
┌───────────────────────▼─────────────────────────────────┐
│                  接入层 Integration                      │
│                                                         │
│  MCP Server                                             │
│  CLI                                                    │
│  HTTP API                                               │
│  通用 Agent 使用说明                                     │
│  Harness Adapter                                        │
└───────────────────────┬─────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────┐
│                  应用服务层 Application                  │
│                                                         │
│  项目服务 Project Service                               │
│  任务服务 WorkItem Service                              │
│  工作流服务 Workflow Service                            │
│  上下文服务 Context Service                             │
│  知识服务 Knowledge Service                             │
│  运行服务 Run Service                                   │
│  产物服务 Artifact Service                              │
│  记录服务 Decision / Finding / Event Service            │
└───────────────────────┬─────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────┐
│                  核心领域层 Domain                       │
│                                                         │
│  Project                                                │
│  WorkItem                                               │
│  Workflow                                               │
│  Run                                                    │
│  Artifact                                               │
│  KnowledgeReference                                     │
│  Decision                                               │
│  Finding                                                │
│  Event                                                  │
│  Approval                                               │
└───────────────────────┬─────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────┐
│                  基础设施层 Infrastructure               │
│                                                         │
│  项目内状态文件（YAML / Markdown / JSONL）               │
│  文本状态优先，无数据库依赖                              │
│  文件存储（产物、日志、本地工作区）                      │
│  Git 集成与跨设备同步                                    │
│  代码索引与知识检索                                      │
│  全文检索：扫描文本（缓存可选，可删除）                  │
│  日志和事件总线                                          │
│  进程执行和工作区管理                                    │
└─────────────────────────────────────────────────────────┘
```

---

## 4. 核心模块

### 4.1 项目管理模块

负责项目长期状态，而不是单次任务状态。

主要内容：

- 项目名称和标识；
- 项目目标；
- 项目背景；
- 用户和使用场景；
- 项目范围；
- 非目标；
- 技术栈；
- 约束条件；
- 当前阶段；
- 里程碑；
- 当前风险；
- 当前阻塞；
- 重要决策；
- 最近变更；
- 项目健康状态。

项目蓝图应能够回答：

- 这个项目为什么存在？
- 目标是什么？
- 当前做到哪里？
- 下一阶段要完成什么？
- 哪些内容明确不做？
- 当前有哪些重要约束？
- 哪些决策已经确定？
- 哪些问题仍未解决？

---

### 4.2 工作项模块

统一使用 `WorkItem` 表示不同类型的工作。

建议支持以下类型：

| 类型 | 用途 |
|---|---|
| initiative | 大方向或长期目标 |
| milestone | 阶段性里程碑 |
| epic | 较大功能或主题 |
| feature | 功能 |
| story | 可交付的用户或技术需求 |
| task | 具体实施任务 |
| bug | 缺陷 |
| research | 调查和研究 |
| decision | 待确认的决策 |
| review | 代码或设计审查 |
| verification | 测试和验证 |
| refactor | 重构 |
| migration | 迁移 |
| release | 发布工作 |
| follow_up | 后续行动 |

WorkItem 不应被限制为传统 Issue。

---

### 4.3 工作流模块

工作流描述“如何完成一类工作”，但不应该把所有流程写死。

工作流可以包含：

- 输入要求；
- 前置条件；
- 可选步骤；
- 任务创建规则；
- 状态转换；
- 条件分支；
- 验证规则；
- 审批点；
- 完成条件；
- 失败处理；
- 后续任务生成规则。

示例工作流：

- 新项目初始化；
- 快速修复；
- 普通功能开发；
- 复杂功能开发；
- Bug 调查；
- 架构变更；
- 重构；
- 代码审查；
- 发布准备；
- 知识库刷新；
- 技术调研；
- 需求变更评估。

---

### 4.4 知识模块

知识模块吸收 RepoWiki 类工具的核心思想，但不以“生成静态 Wiki”为唯一目标。

主要能力：

- 项目扫描；
- 目录结构分析；
- 代码符号索引；
- 模块关系分析；
- 依赖关系分析；
- API 和接口索引；
- 配置和入口识别；
- 文档索引；
- 架构说明；
- 按任务检索相关上下文；
- 增量刷新；
- 知识有效性验证；
- 过期知识标记。

知识系统应该优先回答：

> 当前任务需要读取哪些项目知识？

而不是每次把整个项目知识库全部交给 Agent。

---

### 4.5 执行模块

执行模块记录 Agent 的实际工作过程。

主要内容：

- Agent；
- Harness；
- 模型信息；
- 工作区；
- 分支或 Worktree；
- 执行命令；
- 输入上下文；
- 输出摘要；
- 修改文件；
- 测试结果；
- 日志；
- 错误；
- 重试次数；
- 开始和结束时间；
- 执行状态；
- 产物；
- 后续任务。

执行模块可以直接调用 Harness，也可以只记录外部 Agent 的执行结果。

---

### 4.6 记录模块

系统需要专门记录以下信息：

- Decision：决策；
- Finding：发现；
- Error：错误；
- Risk：风险；
- Event：项目事件；
- Observation：观察；
- Lesson：经验；
- Question：待解决问题。

这些信息不能全部混入普通日志。

例如：

```text
普通日志：
运行测试命令完成。

Finding：
发现协议解析器在异常寄存器长度下会越界。

Decision：
决定将协议长度校验放到统一解析入口。

Risk：
旧设备可能依赖非标准寄存器长度，需要兼容性验证。
```

### 4.7 门禁、记录传播与质量约束

工作项推进必须经过显式门禁，不能依赖 Agent 自觉声明（采纳 agent-tasks 的阶段门禁）。

- **阶段门禁**：每个阶段可配置 `require_artifacts`（必须存在的产物）、`require_min_artifacts`（最少产物数）、`require_comment`（至少一条说明）、`require_approval`（需要审批）；`exempt_stages` 声明豁免阶段；
- **回退需理由**：任何状态回退都必须附带 `reason` 工件，并写入事件流；
- **就绪门**：进入实施前做一次就绪检查，结论只允许 `PASS` / `CONCERNS` / `FAIL`；`FAIL` 必须按严重度列出修复项及应触发的工作流（采纳 BMAD 的实现就绪检查）；
- **领取质量门**：领取前用确定性启发式评分（标题词数、描述长度与结构，不调用模型）拦截信息量过低的任务，避免模糊任务消耗 Agent 上下文；
- **记录传播**：任务完成时，其 `decision` 与 `learning` 记录沿任务图传播到父任务与兄弟任务；只沿已有链接传播，不做全项目广播。

---

### 4.8 运行约束与完成校验

Run 是「一次尝试」，不是「一次成功」，因此需要独立的运行约束与完成校验（采纳 Symphony 的尝试生命周期与 Contrabass 的完成门禁）。

运行阶段（简化自 Symphony 的尝试生命周期）：

```text
preparing_workspace
  -> building_prompt
  -> launching_agent
  -> initializing_session
  -> streaming_turns
  -> finishing
  -> succeeded / failed / timed_out / stalled / canceled
```

工作区不变量（必须在启动 Agent 前校验）：

- Agent 进程的工作目录必须等于该任务的执行工作区；
- 执行工作区路径必须位于配置的工作区根目录内，越界直接拒绝；
- 工作区目录名只允许 `[A-Za-z0-9._-]`；净化后若与原标识不同，必须追加原始标识的哈希后缀以避免碰撞；
- 默认使用 git worktree，一个任务一个工作区，跨 Run 复用但不自动删除。

工作区生命周期钩子（在策略文件中声明，统一超时）：

| 钩子 | 时机 | 失败语义 |
|---|---|---|
| `after_create` | 工作区首次创建后 | 致命：中止创建 |
| `before_run` | 每次尝试启动 Agent 前 | 致命：中止本次尝试 |
| `after_run` | 每次尝试结束后（无论成败） | 仅记录 |
| `before_remove` | 删除工作区前 | 仅记录，继续清理 |

完成校验（防止「声称完成但没有产出」）：

```text
领取任务时：记录工作区 HEAD SHA（claim head）
标记完成时：git rev-parse <branch> 与 claim head 比较
  ↓
分支已推进   -> 允许进入验证
分支未推进   -> 不得标记完成，转人工复核
git 报错     -> 不得标记完成，转人工复核并记录事件
```

语义要求：**没有校验证据就不允许标记为「已验证」**，此处不采纳 Contrabass 对空 SHA 的放行行为。

多轮续跑：

- 首轮使用完整任务提示词；
- 后续轮次只发送续跑指引，不重复完整提示词（线程内已有上下文）；
- 单次会话的轮数上限由策略文件配置；
- 干净退出后安排一次短延迟续跑检查，确认任务是否仍需推进。

---

## 5. 核心数据模型

### 5.1 Project

```yaml
id: temp-monitor-system
name: 温箱集中监控系统
description: ...
status: active
current_phase: implementation

goals:
  - ...
scope:
  in:
    - ...
  out:
    - ...

constraints:
  - ...
tech_stack:
  - ...
milestones:
  - id: m1
    name: ...
    status: planned

current_state:
  summary: ...
  risks: []
  blockers: []
  next_focus: []

blueprint_artifact_id: artifact-001
created_at: ...
updated_at: ...
```

---

### 5.2 WorkItem

```yaml
id: TMS-142
project_id: temp-monitor-system
parent_id: TMS-100

type: feature
title: 增加新的温箱协议
description: ...

status: ready
priority: high

dependencies:
  - TMS-120

acceptance_criteria:
  - 新协议可以被导入
  - 服务端可以建立连接
  - 采集数据可以正常保存
  - 客户端可以显示协议错误

constraints:
  - 不改变现有协议兼容性

context_refs:
  - knowledge://module/modbus
  - artifact://architecture/protocol
  - decision://protocol-adapter

artifact_refs: []

workflow_id: feature-development

assigned_agent: null
assigned_harness: null

created_at: ...
updated_at: ...
```

---

### 5.3 Workflow

```yaml
id: feature-development
name: 功能开发
version: 1

input:
  required:
    - workitem
  optional:
    - project_context
    - architecture_context

steps:
  - id: inspect
    type: inspect
    required: true

  - id: clarify
    type: clarify
    required: false

  - id: plan
    type: plan
    required: true

  - id: implement
    type: execute
    required: true

  - id: verify
    type: verify
    required: true

transitions:
  - from: inspect
    to: clarify
    when: missing_information == true

  - from: inspect
    to: plan
    when: missing_information == false

  - from: verify
    to: implement
    when: verification_failed == true

  - from: verify
    to: done
    when: verification_passed == true

approval_points:
  - architecture_change

completion_rules:
  - acceptance_criteria_verified
  - no_unresolved_blocker
```

策略文件化（采纳 Symphony / Contrabass 的 `WORKFLOW.md` 模式）：

- 每个工作流是一份仓库内文件（`.devsys/workflows/<id>.md`）：YAML front matter 承载配置，正文是该工作流的提示词模板；
- 配置支持 `$VAR` 形式的环境变量引用，密钥不落盘；
- 解析失败时保留上一份有效配置（last-known-good）；配置错误只阻塞新任务派发，不影响对账与只读操作；
- 与 `steps / transitions / approval_points / completion_rules` 并列增加：
  - `gates`：阶段门禁（见 §4.7）；
  - `hooks`：工作区生命周期钩子（见 §4.8）；
  - `concurrency`：全局与按状态的并发上限；
  - `limits`：轮数上限、超时、停滞阈值、退避上限。

---

### 5.4 Run

```yaml
id: run-20260917-001
project_id: temp-monitor-system
workitem_id: TMS-142
workflow_id: feature-development

agent:
  id: codex-local
  harness: codex
  model: ...

workspace:
  path: ...
  branch: feature/tms-142
  worktree: ...

claim:
  head_sha: ...            # 领取时记录的 HEAD，完成校验的基准（见 §4.8）
  claimed_at: ...
  lease_until: ...
  heartbeat_at: ...

verification:
  advanced: null           # true / false；未校验时保持 null
  head_sha_at_complete: ...
  verified_by: null        # 校验未通过时的人工复核者

status: running
attempt: 1
phase: streaming_turns        # 取值见 §4.8

started_at: ...
finished_at: null

input_context_refs:
  - artifact://project/blueprint
  - knowledge://task-context/TMS-142

changed_files: []
commands: []
tests: []
logs: []

result:
  summary: null
  errors: []
  findings: []
  decisions: []
  follow_up_workitems: []

retry_count: 0
```

---

### 5.5 Artifact

```yaml
id: artifact-001
project_id: temp-monitor-system

type: architecture
name: 协议适配器架构说明
path: docs/architecture/protocol-adapter.md

source: agent
created_by_run_id: run-001

status: current
version: 2

related_workitems:
  - TMS-142

created_at: ...
updated_at: ...
```

---

### 5.6 Decision

```yaml
id: decision-001
project_id: temp-monitor-system

title: 使用独立协议适配器扩展新设备协议
context: ...
options:
  - ...
decision: ...
reasoning: ...
consequences:
  - ...
status: approved

created_by: ...
approved_by: ...
related_workitems:
  - TMS-142

created_at: ...
```

---

### 5.7 Finding

```yaml
id: finding-001
project_id: temp-monitor-system

type: bug
title: 异常寄存器长度可能导致解析失败
description: ...
evidence:
  - ...
severity: medium
status: open

discovered_by_run_id: run-001
related_workitems:
  - TMS-142

recommended_actions:
  - 创建修复任务
```

---

## 6. 任务状态设计

### 6.1 基础状态

建议使用有限且稳定的基础状态：

```text
draft
backlog
ready
in_progress
blocked
review
verification
done
cancelled
```

### 6.2 状态含义

| 状态 | 含义 |
|---|---|
| draft | 尚未准备完成 |
| backlog | 已记录但尚未安排 |
| ready | 可以开始 |
| in_progress | 正在执行 |
| blocked | 被依赖、决策或外部条件阻塞 |
| review | 等待审查 |
| verification | 等待测试或验证 |
| done | 已满足完成条件 |
| cancelled | 已取消 |

不要把所有工作类型都强制使用完全不同的状态。类型差异应主要由工作流表达。

---

### 6.3 状态变更规则

状态不能只由 Agent 任意写入。

系统应支持：

- 合法状态转换；
- 状态转换原因；
- 操作人或 Agent；
- 关联 Run；
- 验证结果；
- 审批要求；
- 自动转换规则；
- 强制暂停规则。

例如：

```text
in_progress -> done
```

只有在以下条件满足时才允许：

- 验收标准已检查；
- 必要测试已执行；
- 没有未解决的阻塞；
- 必要审查已完成；
- 需要人工批准时已批准。

---

### 6.4 调度态（与工作项状态分离）

工作项状态描述「事情做到哪一步」，调度态描述「此刻有没有人在做」，两者必须分开，否则客户端会把「正在运行」误当成业务状态（采纳 Symphony 的领取态模型）。

| 调度态 | 含义 |
|---|---|
| `unclaimed` | 未领取，也没有重试计划 |
| `claimed` | 已被领取，用于防止重复派发；实际处于运行或排队重试 |
| `running` | 存在运行中的 Run |
| `retry_queued` | 无运行中的 Run，但存在重试计划 |
| `released` | 领取被释放（任务终态、不再可派发或重试结束） |

约束：

- 只有调度器可以写调度态，Agent 只能写工作项状态与记录；
- `claimed` 与 `running` 检查必须先于任何启动动作；
- 调度态写入依赖租约与心跳（见 §15.3），跨机器冲突按 §14.4 处理。

与工作项状态的映射：

| 工作项状态 | 典型调度态 |
|---|---|
| `draft` / `backlog` | `unclaimed` |
| `ready` | `unclaimed`（可派发）或 `retry_queued` |
| `in_progress` | `claimed` / `running` |
| `blocked` | 保持 `unclaimed`，写入阻塞原因，不进入派发队列 |
| `review` / `verification` | `released`（等待人或其它角色处理） |
| `done` / `cancelled` | `released` |

---

## 7. 动态任务机制

### 7.1 为什么需要动态任务

真实开发过程经常出现：

- 调查后发现需要修改架构；
- 测试失败后需要创建修复任务；
- 发现需求不明确，需要创建澄清任务；
- 发现旧代码存在隐患，需要创建技术债任务；
- 一个功能实施中发现需要先升级依赖；
- 代码审查发现新的安全或兼容性问题；
- 一个任务完成后产生多个后续任务。

因此，任务系统不能只支持预先创建好的静态任务列表。

---

### 7.2 动态任务生成规则

Agent 或工作流可以提出新任务，但系统应记录来源：

```yaml
created_from:
  type: run
  id: run-001

reason: 测试发现旧协议长度校验缺失

proposed_by: codex-local
approval_required: false
```

任务生成来源包括：

- 用户请求；
- 工作流规则；
- Agent 建议；
- 测试失败；
- 代码审查；
- 决策结果；
- 发现记录；
- 依赖阻塞；
- 定期检查；
- 项目状态分析。

---

### 7.3 动态流程示例

```text
用户：增加新的温箱协议
  ↓
系统读取项目蓝图和代码知识
  ↓
判断现有协议抽象是否足够
  ↓
发现架构信息不足
  ↓
创建 research 任务
  ↓
调查完成
  ↓
创建 decision 任务
  ↓
决策确认
  ↓
创建 feature / task 任务
  ↓
调用任意 Harness 实施
  ↓
运行测试
  ↓
测试失败
  ↓
自动创建 bug / follow_up 任务
  ↓
修复并重新验证
  ↓
更新知识和项目状态
```

### 7.4 就绪门与「下一步」优先级

`devsys next` 必须给出确定的推荐顺序，而不是任意挑选（采纳 BMAD 的固定优先级与就绪判定）。

就绪判定结论只有三种：`PASS`、`CONCERNS`、`FAIL`。`FAIL` 必须给出按严重度排序的修复项以及应触发的工作流。

下一步优先级（自上而下，先命中先返回）：

1. 恢复已领取但停滞或中断的任务；
2. 处理等待审查或等待审批的任务；
3. 启动下一个「就绪」任务；
4. 启动最早进入 `backlog` 的任务；
5. 运行未完成的项目级回顾；
6. 报告完成。

派发排序（同优先级内）：`priority` 升序 → `created_at` 最早 → 标识符字典序；同时受全局与按状态并发上限、`blocked_by` 先决条件约束（采纳 Symphony SPEC §8.2/§8.3）。

要求：

- 只输出状态、风险与下一步，**不提供时间估算**；
- 风险信号至少包含：状态文件过期、孤儿任务、长期滞留审查的任务、未解决的阻塞。

---

## 8. 外部接口设计

### 8.1 接口层次

系统建议同时提供：

1. MCP；
2. CLI；
3. HTTP API；
4. 文件交换格式；
5. 可选 SDK。

所有接口必须调用同一套应用服务。

当前优先提供 CLI、MCP 与文件交换格式，不建设远程中心协调服务。Agent 正常通过 CLI/MCP 查询有效状态并提交状态变更；文件交换的导入也必须经过同一套校验、门禁与事务路径，不能绕过应用服务直接改任务状态。

```text
MCP ─────┐
CLI ─────┤
HTTP ────┼──> Application Service ──> Domain ──> Storage
SDK ─────┤
File ────┘
```

---

### 8.2 MCP 工具分类

#### 项目工具

```text
project_list
project_get
project_create
project_update
project_status
project_blueprint_get
project_state_update
```

#### 工作项工具

```text
workitem_list
workitem_get
workitem_create
workitem_update
workitem_next
workitem_claim
workitem_release
workitem_start
workitem_block
workitem_complete
workitem_add_dependency
workitem_remove_dependency
```

#### 工作流工具

```text
workflow_list
workflow_get
workflow_start
workflow_step_next
workflow_step_complete
workflow_pause
workflow_resume
workflow_cancel
```

#### 上下文工具

```text
context_get
context_for_workitem
context_refresh
context_compact
```

#### 知识工具

```text
knowledge_search
knowledge_get_module
knowledge_get_file
knowledge_get_dependencies
knowledge_get_related
knowledge_refresh
knowledge_validate
knowledge_status
```

#### 运行工具

```text
run_create
run_get
run_list
run_update
run_log
run_heartbeat
run_verify
run_complete
run_fail
run_cancel
```

#### 记录工具

```text
decision_list
decision_get
decision_create
decision_approve

finding_list
finding_get
finding_create
finding_resolve

event_list
event_record
```

#### 产物工具

```text
artifact_list
artifact_get
artifact_register
artifact_update
```

---

### 8.3 Agent 会话接口

建议提供一个高层接口，减少 Agent 初次使用时需要调用的工具数量：

```text
agent_session_start
```

输入：

```json
{
  "project_id": "temp-monitor-system",
  "harness": "codex",
  "agent_id": "codex-local",
  "workspace": "D:/Projects/TempMonitorSystem",
  "intent": "继续当前项目开发"
}
```

返回：

```json
{
  "project": {
    "id": "temp-monitor-system",
    "name": "温箱集中监控系统",
    "current_phase": "implementation"
  },
  "state": {
    "summary": "...",
    "blockers": [],
    "risks": [],
    "next_focus": []
  },
  "current_workitems": [],
  "recommended_next_action": {
    "type": "workitem",
    "id": "TMS-142",
    "reason": "任务已准备完成且没有未解决依赖"
  },
  "context": {
    "project_summary": "...",
    "relevant_architecture": [],
    "relevant_decisions": [],
    "recent_findings": []
  }
}
```

该接口可以减少 Agent 需要自行组合多个查询的成本。

---

### 8.4 CLI 示例

```bash
# 项目
devsys project list
devsys project show temp-monitor-system
devsys project status temp-monitor-system

# 状态与同步（状态保存在项目内 .devsys/）
devsys init
devsys status
devsys sync status

# 工作区视图（只读）
devsys workspace serve
devsys workspace build --static

# 工作项
devsys task list
devsys task show TMS-142
devsys task next
devsys task create --project temp-monitor-system --type feature --title "增加新的温箱协议"
devsys task update TMS-142 --status in_progress
devsys task block TMS-142 --reason "等待协议定义确认"
devsys task complete TMS-142

# 上下文
devsys context get --task TMS-142
devsys knowledge search "Modbus 协议解析"
devsys knowledge module "Protocol"

# 记录
devsys decision create
devsys finding create
devsys event record

# 运行
devsys run list
devsys run show run-001
devsys run complete run-001
```

---

### 8.5 HTTP API

HTTP API 用于：

- Web UI；
- 外部自动化；
- 第三方集成；
- 自定义 Agent；
- CI/CD；
- 其它编排器。

建议采用版本化路径：

```text
/api/v1/projects
/api/v1/workitems
/api/v1/workflows
/api/v1/runs
/api/v1/knowledge
/api/v1/artifacts
/api/v1/decisions
/api/v1/findings
/api/v1/events
```

实时状态可通过：

- Server-Sent Events；
- WebSocket；
- 长轮询；
- 事件订阅；

实现。

---

## 9. Harness 适配设计

### 9.1 适配器原则

Harness Adapter 不是核心领域模型，而是执行层插件。

适配器负责：

- 检查 Harness 是否安装；
- 检查版本和能力；
- 生成执行命令；
- 注入任务上下文；
- 设置工作目录；
- 设置环境变量；
- 启动进程；
- 读取标准输出和错误输出；
- 处理退出码；
- 收集结果；
- 将结果转换为统一 Run 格式。

---

### 9.2 统一执行接口

```text
HarnessAdapter
  ├── get_name()
  ├── check_available()
  ├── get_capabilities()
  ├── prepare_workspace()
  ├── build_command()
  ├── start()
  ├── stream_output()
  ├── stop()
  └── collect_result()
```

建议支持的能力：

```text
supports_non_interactive
supports_json_output
supports_session_resume
supports_mcp
supports_worktree
supports_streaming
supports_model_selection
supports_approval
```

---

### 9.3 初始适配器

第一阶段建议优先支持：

1. 通用 Shell 执行器；
2. Codex；
3. OpenCode；
4. Claude Code；
5. 外部 MCP Agent；
6. Contrabass 作为可选执行编排器。

不要一开始为每个 Harness 开发大量专用逻辑。

优先使用：

- 标准输入输出；
- 命令行参数；
- MCP；
- JSON 或 JSONL；
- 文件结果协议；
- 统一退出码。

---

## 10. BMAD、RepoWiki、Contrabass、agent-tasks 的整合方式

### 10.1 BMAD 的定位

BMAD 作为“开发方法和规划能力”（参考实现：<https://github.com/bmad-code-org/BMAD-METHOD>）。

可以吸收：

- 项目启动分析；
- 产品需求；
- PRD；
- 架构设计；
- Epic；
- Story；
- 验收标准；
- 开发准备检查；
- 代码审查；
- 回顾总结。

映射关系：

| BMAD 概念 | 本系统概念 |
|---|---|
| Brief | Project Blueprint / Discovery Artifact |
| PRD | Specification Artifact |
| Architecture | Architecture Artifact / Decision |
| Epic | Epic WorkItem |
| Story | Story WorkItem |
| Sprint Status | 统一 WorkItem 状态 |
| Code Review | Review WorkItem |
| Retrospective | Event / Lesson / Finding |

本系统不应直接把 BMAD 的文件结构作为内部数据模型。

---

### 10.2 RepoWiki 的定位

RepoWiki 作为“代码知识和上下文能力”（参考实现：<https://github.com/JAYY513/repowiki>）。

可以吸收：

- 项目扫描；
- 代码结构分析；
- 模块索引；
- 依赖关系；
- 文档生成；
- 知识验证；
- 增量更新；
- 按任务获取上下文。

但知识层应进一步支持：

- 查询结果与代码版本绑定；
- 知识新鲜度；
- 过期检测；
- 任务相关性排序；
- 决策和任务关联；
- 多工作区或 Worktree 的差异；
- 只返回当前任务所需的最小上下文。

---

### 10.3 Contrabass / Symphony 的定位

Contrabass/Symphony 类设计作为“执行编排能力”的参考或可选后端。

两个项目的关系：Symphony 是 OpenAI 的原始设计（Elixir 参考实现，<https://github.com/openai/symphony>），Contrabass 是其 Go 实现并扩展了 TUI、内部看板和多 Agent 团队模式（<https://github.com/junhoyeo/contrabass>）。本系统按能力吸收其编排模型，不绑定任一实现。

可以吸收：

- 任务领取和释放；
- 依赖门控；
- 工作区和 Worktree；
- Agent Runner；
- 并发执行；
- 失败重试；
- 孤儿任务恢复；
- 停滞检测；
- 心跳和运行状态；
- 日志流；
- 任务执行监控；
- TUI / Web Dashboard；
- 事件记录。

但本系统不应被限制为：

```text Issue -> Run
```

而应使用更通用的模型：

```text Project -> Initiative -> WorkItem -> Workflow -> Run -> Artifact
```

---

### 10.4 agent-tasks 的定位

agent-tasks 类设计作为“任务流水线与看板能力”的参考（<https://github.com/keshrath/agent-tasks>）。

可以吸收：

- 可配置阶段（backlog → spec → plan → implement → test → review → done）；
- 依赖 DAG 与阶段推进门禁；
- 审批与拒绝后自动回退；
- 工件版本化与评论线程；
- 只读看板视图（对应 §17 的工作区视图）。

明确不采纳：

- 默认把状态集中存放在用户级数据库（`~/.agent-tasks/agent-tasks.db`），项目状态无法随仓库跨设备同步；
- 任务状态与项目代码分离，换电脑时需要单独迁移数据库；
- 以嵌入式 SQLite 作为任务事实来源（Contrabass、RepoWiki 均以项目内文件为事实来源，见 §14.1）。

本系统采用 §2.7 的项目内状态模型：保留同类流水线能力，但状态文件保存在项目内。

---

### 10.5 推荐整合链路

```text
项目蓝图
  ↓
需求 / 规格
  ↓
架构和决策
  ↓
WorkItem
  ↓
动态工作流
  ↓
Harness Agent 执行
  ↓
测试 / 审查 / 验证
  ↓
Run 结果
  ↓
更新任务状态
  ↓
记录错误、发现、决策和产物
  ↓
刷新代码知识
  ↓
更新项目状态
```

---

### 10.6 参考实现对比与最优组合

按八个坐标轴对比五个参考实现（证据来自各仓库源码与文档）：

| 坐标轴 | BMAD | RepoWiki | Symphony | Contrabass | agent-tasks |
|---|---|---|---|---|---|
| 事实来源 | `_bmad/` 与 `_bmad-output/` 文件 | `.repowiki/*.json` 与 `docs/repowiki/` | 调度状态仅在内存，恢复靠 tracker + 文件系统 | `.contrabass/board/` 的 JSON/JSONL | 用户级 SQLite 与第二个用户级知识库 |
| 工作项 | Epic/Story、`stories.yaml`、`sprint-status.yaml` | 模块/页面（不是任务系统） | Issue + 5 领取态 + 11 尝试阶段 | issue 4 态与领取态映射 | task + 阶段流水线 + 依赖 DAG |
| 策略定义 | 技能与文档，人工会话驱动 | 技能固定 Step 0–7 | 仓库内 `WORKFLOW.md`（front matter + 模板） | 同左，含热载入与 last-known-good | 数据库表 `pipeline_config` |
| 完成校验 | 就绪门 PASS/CONCERNS/FAIL | validate 报告 | 允许在移交态结束 | claim SHA 与 `git rev-parse` 分支门禁 | 阶段门禁 + 审批 |
| 工作区 | 无 | 无 | 每 issue 独立目录 + 路径不变量 | git worktree | 无 |
| 并发与重试 | 人工节奏 | 单跑 + pid 互斥 | 全局与按状态上限、确定性退避、停滞检测 | 同左 + 确定性抖动、孤儿恢复 | 心跳清理、孤儿回收、保留期 |
| 知识上下文 | `project-context.md`、spec、架构脊柱 | triggers 路由 + commit 基线 + protected | 无（仅提示词） | 无（仅提示词与 diff 快照） | 桥接到独立知识服务 |
| 跨设备 | 无 | Git 仅作外部手段 | 无 | 无 | 无 |

结论：五个参考各自只覆盖了本方案的一部分，**没有一个解决「状态随项目仓库跨设备同步」**，也都不提供完整的工作项 + 知识 + 记录三位一体。

#### 采纳清单

| # | 机制 | 来源 | 落点 |
|---|---|---|---|
| 1 | 领取时记录 HEAD SHA，完成时用 `git rev-parse <branch>` 校验分支推进 | Contrabass | §4.8 |
| 2 | 策略文件化：front matter + 模板正文 + 严格校验 + 热载入 + last-known-good | Symphony / Contrabass | §5.3 |
| 3 | 领取态与工作项状态分离 | Symphony SPEC §7.1 | §6.4 |
| 4 | 多轮续跑：首轮全量提示词、后续仅续跑指引、轮数上限、干净退出后短延迟续跑 | Symphony SPEC §7.1 | §4.8 |
| 5 | 确定性退避与可复现抖动（以任务与尝试次数为种子） | Contrabass | §15.4 |
| 6 | 停滞检测（末次事件超时即终止并重试） | Symphony SPEC §8.5 | §15.4 |
| 7 | 工作区安全不变量：cwd 等于工作区、路径不越根、目录名净化加哈希后缀 | Symphony SPEC §9.5 | §4.8 |
| 8 | 工作区生命周期钩子与失败语义（致命 / 仅记录） | Symphony SPEC §5.3.4 | §4.8 |
| 9 | 派发确定性：优先级 → 创建时间 → 标识符；全局与按状态并发上限 | Symphony SPEC §8.2/§8.3 | §7.4 |
| 10 | 阶段门禁与回退理由工件 | agent-tasks | §4.7 |
| 11 | 领取质量门：确定性启发式评分拦截信息量过低的任务 | agent-tasks `domain/confidence.ts` | §4.7 |
| 12 | 决策与学习按任务图传播到父与兄弟任务 | agent-tasks | §4.7 |
| 13 | 知识新鲜度：`source_commit`、基线 commit、`affected_pages`、退出码 0/10/11、`content_hash` 与 `protected`、`run.json` 断点 | RepoWiki | §12.5 |
| 14 | 就绪门与固定的「下一步」优先级，且不提供时间估算 | BMAD | §7.4 |
| 15 | 右尺寸流程：微小改动 / 单会话 / Epic / 项目共用同一实现单元 | BMAD | §2.5 |
| 16 | 管理块幂等注入、标记区间、手写内容保留 | RepoWiki | §12.5 |
| 17 | 凭据隔离：配置只写引用，宿主密钥不下传子进程 | Symphony SPEC §1、§3.3 | §16.3 |
| 18 | 单写者 + 原子 rename + 追加 JSONL + 文件锁 | Contrabass | §15.2 |
| 19 | 状态修复：先推断真实状态，人工确认后重写并校验；只有修复路径允许降低完成度 | BMAD | §15.4 |

#### 明确拒绝

| # | 反模式 | 来源 | 拒绝理由 |
|---|---|---|---|
| 1 | 用户级集中数据库，以及第二个用户级知识库 | agent-tasks | 违背 §2.7：状态不随仓库走 |
| 2 | 流水线配置存入数据库表 | agent-tasks | 不可 review、不可 diff、不可离线迁移 |
| 3 | 完成校验 fail-open（空 SHA 或 git 错误静默通过） | Contrabass | 门禁失效比误报更危险，见 §4.8 |
| 4 | 运行态文件与锁多套并存（team / events / heartbeat / dispatch / mailbox） | Contrabass | 冲突面扩大；本系统只保留 claim、run、event 三处 |
| 5 | 编排状态只存内存 | Symphony | 与跨设备恢复目标冲突；状态必须已持久且可重建 |
| 6 | 通知与状态写入一律 fail-open | agent-tasks | 通知可降级，状态写入必须失败可见 |
| 7 | 单文件千行 CLI、git 失败静默降级为空基线 | RepoWiki | 维护性与正确性；git 故障必须显式报错 |
| 8 | 小改动也强制走完整 PRD/Epic 流程 | BMAD 的反面 | 采纳其右尺寸原则，拒绝一刀切 |

结论：

> 最优组合 = BMAD 的右尺寸流程与就绪门 + RepoWiki 的知识新鲜度与人工保护 + Symphony 的调度状态机与工作区不变量 + Contrabass 的完成校验与确定性退避 + agent-tasks 的阶段门禁与记录传播；存储与同步层五个参考都未解决，采用 §14 的项目内文本 + Git 方案。

---

### 10.7 选型结论：执行层对齐 Symphony SPEC

决策：

> 执行层以 Symphony SPEC（`openai/symphony`，规范 18 节 + 附录 A）作为对齐规范；项目状态、知识、记录层自研；Contrabass 仅作为可选执行后端；BMAD 与 agent-tasks 只取方法与机制。

评估过的选项：

| 选项 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| A. 对齐 Symphony SPEC，核心自研 | 语言无关契约；含参考算法（SPEC §16）、一致性测试矩阵（§17）、实现清单（§18）；扩展点显式（HTTP 面板、SSH worker 均为可选扩展，不影响核心正确性）；安全与路径不变量明确 | 只覆盖执行层；调度状态默认只在内存（SPEC §14.3），与项目内持久状态相反，需要显式替换 | **采纳** |
| B. 直接以 Contrabass 为核心执行器 | 开箱即用（TUI、看板、dashboard、team、worktree、claim 校验），且与规范一致 | 看板状态写入 `.contrabass/`，与 `.devsys/` 构成两套事实来源；无 MCP；完成校验对空 SHA 放行；依赖 Go 与 tmux | 仅作可选后端（§10.3） |
| C. 以 BMAD 为骨架 | 方法成熟、生态大、产物为文本 | 没有运行时、状态机、并发与工作区；`_bmad-output/` 与本系统状态目录重复 | 仅取方法（§2.5、§7.4） |
| D. 以 agent-tasks 为任务层 | MCP + REST + WebSocket + 看板开箱即用；门禁与传播完整 | 用户级集中库（违背 §2.7）；配置存库不可 diff；跨机器无解 | 仅取机制（§4.7） |
| E. 完全自研，不参照规范 | 完全贴合本项目需求 | 缺少外部一致性清单，容易漏掉停滞检测、路径安全、双写竞态等边界 | 不取 |

对齐边界（照搬 / 替换 / 扩展）：

| Symphony SPEC 的内容 | 处理 |
|---|---|
| `WORKFLOW.md` 契约、校验与热载入、失败语义（SPEC §5、§6） | 照搬 |
| 领取态、尝试阶段、续跑与重试语义（SPEC §7） | 照搬 |
| 派发、对账、清理与排序（SPEC §8、§16） | 照搬 |
| 工作区不变量与 hooks（SPEC §9） | 照搬 |
| 密钥处理与安全要求（SPEC §15.3、§15.2） | 照搬 |
| 面板只做观测、不得成为正确性依赖（SPEC §13.7） | 照搬（对应 §17 只读工作区视图） |
| 调度状态仅在内存，重启靠 tracker 恢复（SPEC §14.3） | **替换**：状态持久化到 `.devsys/`，恢复改为打开项目时对账（§15.4） |
| 一律以外部 tracker 为事实来源（SPEC §11） | **替换**：`.devsys/` 工作项是事实来源，外部 Issue 只做映射（§20.2） |
| 单机部署假设 | **扩展**：跨设备同步（§14.4）；远端执行参考附录 A 的 SSH worker 扩展，列为后续选项 |

说明：Symphony 自述为 low-key engineering preview，因此本方案把它当作**规范与验收清单**使用，而不是当作生产系统依赖；任何偏离 SPEC 的行为都必须在本文档中显式记录。

对齐基准：`openai/symphony` 的 `SPEC.md`，本地克隆于 `C:\Source\CodeSource\github-clone\symphony`（2026-09-17，commit `be10a1b`）；升级规范前先比对该文件的新旧差异。

---

## 11. 上下文管理设计

### 11.1 上下文分层

建议将上下文分为以下层级：

#### 第一层：项目摘要

始终可以快速获取：

- 项目目标；
- 当前阶段；
- 当前重点；
- 当前阻塞；
- 关键约束；
- 最近变更；
- 重要决策。

#### 第二层：任务上下文

与当前 WorkItem 直接相关：

- 任务描述；
- 验收标准；
- 依赖；
- 相关任务；
- 相关决策；
- 相关发现；
- 相关产物；
- 相关代码模块。

#### 第三层：领域上下文

按任务需要加载：

- 架构说明；
- 模块说明；
- API；
- 数据模型；
- 协议；
- 配置；
- 测试策略。

#### 第四层：历史上下文

仅在需要时读取：

- 旧 Run；
- 历史错误；
- 过去的失败尝试；
- 已关闭决策；
- 旧版本文档；
- 相关提交。

---

### 11.2 上下文获取原则

不要每次把所有信息交给 Agent。

会话先调用 `agent_session_start` 获取摘要，再通过项目、工作项、决策和上下文接口按需查询。接口应提供来源与版本信息；长篇规格、蓝图和决策正文可按返回的路径读取。知识检索用于定位相关模块，源码阅读用于确认实际实现，不以搜索代码目录或聊天记录推断任务进度。直接检查 `.devsys/` 可用于诊断，但冲突或未恢复的数据不得当作有效状态；人工编辑的文档仍须经过对应校验与新鲜度检查。

上下文服务应根据以下因素筛选：

- 当前项目；
- 当前任务；
- 任务类型；
- 相关模块；
- 依赖；
- 最近变更；
- 当前工作流步骤；
- Agent 的上下文预算；
- 信息的新鲜度；
- 信息的重要性。

---

### 11.3 上下文快照

每次 Run 应记录实际使用的上下文快照：

```yaml
context_snapshot:
  project_state_version: 12
  workitem_version: 4
  artifact_versions:
    - artifact-001:v2
  knowledge_revision: 20260917-03
  decision_ids:
    - decision-001
```

这样可以追溯：

- Agent 当时看到了什么；
- 为什么做出某个决定；
- 某次错误是否由过期上下文造成；
- 后续结果是否依赖旧版本知识。

---

## 12. 代码知识系统设计

### 12.1 知识对象

建议至少支持：

- Project；
- Repository；
- Directory；
- File；
- Symbol；
- Module；
- Interface；
- Class；
- Function；
- API；
- Dependency；
- Configuration；
- Test；
- Document；
- Architecture Node；
- Knowledge Note。

---

### 12.2 知识索引流程

```text
扫描项目
  ↓
识别语言和构建系统
  ↓
分析目录和文件
  ↓
解析符号和依赖
  ↓
识别模块边界
  ↓
关联文档和测试
  ↓
生成知识记录
  ↓
写入文本知识文件与可选检索缓存
  ↓
执行一致性验证
  ↓
发布知识版本
```

---

### 12.3 增量更新

不应每次全量扫描。

触发方式：

- Git 提交后；
- 文件变更后；
- Agent Run 完成后；
- 手动刷新；
- 定时刷新；
- 知识过期；
- 结构变化检测。

---

### 12.4 知识可信度

每条知识应有：

```yaml
source:
  type: code
  path: ...
  revision: ...

confidence: high
freshness: current
verified_at: ...
```

知识状态可以包括：

```textcurrent
stale
invalid
unverified
conflicted
```

代码事实优先级应高于旧文档中的描述。

---

### 12.5 新鲜度机制与人工保护

知识层必须能证明「它描述的是哪个版本的代码」（采纳 RepoWiki 的基线机制）。

- 每个知识页面带 `source_commit` 与 `sources`（该页覆盖的路径模式）；
- `state.json` 保存整体基线 commit 与页面映射；
- `devsys knowledge status` 将基线与 `HEAD` 比较，并用 `sources` 计算 `affected_pages`；
- 退出码约定：`0` 新鲜、`10` 过期、`11` 缺失，便于 hooks 与 CI 直接使用；
- 增量再生成只重写 `affected_pages`；
- `content_hash` 记录人工编辑：默认跳过被手工修改的页面，`protected: true` 永久锁定，只有 `--force` 才能覆盖；
- 生成过程写入 `run.json` 检查点（含 pid 互斥），中断后可续跑而不是重来；
- 管理块幂等：向 `AGENTS.md` 注入的说明必须位于标记区间内，重复执行不重复写入，手写内容原样保留；
- git 不可用或命令失败时必须显式报错，**不允许静默降级为「未知基线」**。

---

## 13. 工作流设计

### 13.1 快速修复流程

适用于：

- 简单 Bug；
- 小范围修改；
- 文档修正；
- 小型配置调整。

流程：

```text
读取任务
  ↓
获取相关上下文
  ↓
实施修改
  ↓
运行必要验证
  ↓
记录结果
  ↓
完成任务
```

---

### 13.2 普通功能开发流程

```text
读取任务
  ↓
检查需求完整性
  ↓
检查相关架构和代码
  ↓
必要时创建调查任务
  ↓
生成实施计划
  ↓
实施
  ↓
测试
  ↓
代码审查
  ↓
修复问题
  ↓
更新知识
  ↓
完成任务
```

---

### 13.3 复杂功能流程

```text
项目状态分析
  ↓
需求澄清
  ↓
规格设计
  ↓
架构评估
  ↓
架构决策
  ↓
拆分 WorkItem
  ↓
依赖排序
  ↓
逐项实施
  ↓
集成验证
  ↓
代码审查
  ↓
文档和知识更新
  ↓
里程碑更新
```

---

### 13.4 Bug 调查流程

```text
收集现象
  ↓
复现问题
  ↓
建立假设
  ↓
检查代码和日志
  ↓
记录 Finding
  ↓
确定根因
  ↓
创建修复任务
  ↓
实施修复
  ↓
回归测试
  ↓
关闭 Finding
```

---

### 13.5 架构变更流程

```text
提出变更
  ↓
影响范围分析
  ↓
读取架构和代码知识
  ↓
比较候选方案
  ↓
创建 Decision
  ↓
人工或规则审批
  ↓
更新架构产物
  ↓
创建实施任务
  ↓
实施和验证
  ↓
刷新知识
```

---

## 14. 存储设计

### 14.1 存储原则

- 项目内状态文件是唯一权威来源：蓝图、任务、Run、决策、发现、事件和产物元数据全部写入项目仓库内的 `.devsys/`；
- 文本优先：YAML / Markdown / JSONL，可 diff、可 review、可合并；
- Git 即同步机制：状态随仓库在不同电脑之间同步，不引入额外的状态服务；
- 无公共数据区：不把任务或运行状态集中写入用户级数据库；用户级目录只保存项目路径注册表、凭证引用和无项目语义的缓存；
- 不使用数据库：查询与检索直接扫描项目内文本文件；任何缓存都必须是 `.cache/` 下的普通文件，可删除、可自动重建。

当前不使用数据库的取舍：

- 目标是本机多 Agent 协作与不同设备前后接力，优先保留状态随 Git 审查、迁移和备份的使用方式；
- 数据库能提供事务与查询便利，但不是任务管理的必要条件；本系统通过统一命令入口和 §15.2 的文件一致性协议实现本机协调，Git 仅负责版本传递，不替代事务或锁；
- 不以固定记录数量作为数据库是否有价值的判断标准；文本检索性能在 M1.6 实测，当前不引入 SQLite（包括索引数据库）或中心服务；
- 若未来明确要求跨设备同时协调执行，或实测无法满足需求，再单独评估架构变更，不提前实现第二套存储。

可选加速（都不改变事实来源）：

- 进程内索引：一次扫描后在内存中构建，进程结束即释放；
- 缓存文件：`.cache/` 下的 JSON 或二进制快照，缺失或损坏时自动重建；
- 外部检索服务 / 对象存储：仅在多用户或大规模场景按需引入，不得成为项目状态的唯一存放位置。

---

### 14.2 数据与文件分工

| 内容 | 存储位置 | 是否提交 Git | 说明 |
|---|---|---|---|
| 项目元数据、蓝图 | `.devsys/project.yaml` | 提交 | 权威来源 |
| 任务（WorkItem） | `.devsys/workitems/<id>.yaml` | 提交 | 一任务一文件，降低合并冲突 |
| 任务规格 | `.devsys/specs/<id>.md` | 提交 | Markdown 正文 |
| 运行记录 | `.devsys/runs/<run-id>.yaml` | 提交 | 摘要与结果 |
| 运行事件流 | `.devsys/runs/<run-id>.jsonl` | 提交（可裁剪） | 追加写 |
| 事件 | `.devsys/events/<YYYY-MM>.jsonl` | 提交（可裁剪） | 追加写、按月分片 |
| 决策 / 发现 | `.devsys/decisions/`、`.devsys/findings/` | 提交 | 每条一文件 |
| 代码知识 | `.devsys/knowledge/` | 提交 | Markdown + 元数据 |
| 检索缓存（可选） | `.devsys/.cache/` | 不提交 | 普通缓存文件，可删除重建，不是数据库 |
| 日志、大型产物、临时工作区 | `.devsys/local/` | 不提交 | 由 `.gitignore` 排除 |
| 凭证、密钥 | 环境变量 / 系统密钥链 | 不提交 | 状态文件只保存引用名 |

---

### 14.3 项目目录建议

```text
project-root/
├── .devsys/
│   ├── project.yaml
│   ├── config.yaml
│   ├── state/
│   │   ├── current.yaml
│   │   └── milestones.yaml
│   ├── workitems/
│   ├── specs/
│   ├── workflows/
│   ├── runs/
│   ├── decisions/
│   ├── findings/
│   ├── events/
│   ├── artifacts/
│   ├── context/
│   ├── knowledge/
│   ├── .cache/          # 可选检索缓存，不提交
│   └── local/           # 日志与临时文件，不提交
├── docs/
├── src/
├── tests/
└── ...
```

注意：

- `.devsys/` 是唯一的项目状态来源，随仓库提交；
- 不在用户级目录保存任何项目数据；用户级配置只保存项目路径注册表和凭证引用；
- 可选缓存（`.cache/`）可删除重建；本地文件（`local/`）不提交，但清理前必须检查未完成事务、活动执行与未转移产物，不能把恢复材料当作缓存删除（见 §15.2）；
- 结构化数据用 YAML，正文知识用 Markdown，追加型数据用 JSONL；
- 敏感信息和模型密钥不进入状态文件（见 §16.3）。

---

### 14.4 跨电脑同步

同步通道就是 Git：

当前仅支持不同设备前后接力，不支持跨设备同时领取或写入同一项目状态。本机文件锁不能跨 Git 副本协调，Git 也不是分布式锁；`sync status` 不能证明另一台设备已经停止执行。

```text
电脑 A（提交状态）
  ↓ git push
远端仓库
  ↓ git pull
电脑 B（恢复状态）
```

约定：

- 一任务一文件、一决策一文件，事件按月分片追加，避免单一巨型文件造成冲突；
- 每个状态文件带 `updated_at`、`updated_by`（机器或 Agent 标识）与版本号；
- 任务领取写入独立租约文件；本机按 §15.2 串行校验和更新。跨设备出现租约分叉时保留双方并阻止相关任务继续派发，禁止依据时间戳较新者自动覆盖；
- 状态提交与代码提交分开：状态使用独立提交前缀（如 `chore(devsys): ...`），便于回滚与审计；
- 提供 `devsys sync status`：检测本地与远端分叉、未提交的状态变更和潜在冲突；
- 不允许静默自动合并两个分叉的任务状态；必须显式选择保留或合并，并记录事件；
- 冲突无法自动解决时，保留双方文件并标注 `conflict`，由人或 Agent 决策，或走 §15.4 的状态修复流程（推断 → 确认 → 重写）。

换电脑流程（先完成交接，再恢复）：

1. 旧设备停止派发并结束或明确中断当前执行，处理未完成事务，确认无写者后释放领取、保存结果；尚未提交的代码、分支和需要转移的本地产物须一并处理。
2. 提交并推送代码与 `.devsys/` 状态；锁和未完成事务恢复材料不作为可移植运行状态提交，存在待恢复事务时不能宣告交接完成。
3. 新设备 clone/pull 已交接版本，运行初始化与状态检查；出现冲突或恢复失败时先修复，不开始派发。完成显式交接前旧设备不得继续写入。

```text
git clone <repo>
  ↓
devsys init      # 校验 .devsys/（没有数据库需要迁移）
  ↓
devsys status    # 蓝图、任务、Run、记录全部就绪
```

---

## 15. 并发与一致性

### 15.1 并发场景

系统需要考虑：

- 多个 Agent 同时读取同一任务；
- 多个 Agent 试图领取同一任务；
- 一个 Agent 更新任务时另一个 Agent 同时更新；
- 多个 Run 同时写入日志；
- 知识索引正在刷新时 Agent 查询知识；
- 工作流自动创建任务时用户手动修改任务。

---

### 15.2 必要机制

以下机制必须由 CLI/MCP 共用的存储与应用服务实施；执行 Agent 和外部命令期间不持有写锁：

- 乐观并发控制；
- 版本号；
- 原子状态转换；
- 任务 Claim（文件级领取，见 §14.4）；
- Lease 租约；
- 心跳；
- 超时释放；
- 幂等操作；
- 事件日志；
- 变更审计；
- 冲突检测；
- 跨机器分叉检测与人工确认合并（见 §14.4）；
- 项目级短时写锁：同一项目内受管状态变更串行执行，锁内重新检查版本、依赖与租约；旧版本更新必须拒绝，不能覆盖新状态；
- 单文件原子写：写临时文件后替换，禁止就地截断；验证 Windows/macOS/Linux 上的替换、锁与持久化行为，不能仅凭 rename 宣称跨文件事务成立；
- 跨文件事务：任务、Run 与事件等关联变更使用可恢复日志，记录事务标识、预期版本、变更内容与提交状态。协议须明确持久化顺序和幂等恢复规则；JSONL 追加须检测不完整尾部并防止恢复时重复登记事件；
- 有效状态读取须与写入、恢复协调，不能返回半提交的任务/Run/事件组合。中断后先按日志完成确定性恢复；无法恢复时明确拒绝有效状态查询与变更，仅允许诊断，不伪装为已提交；
- 锁和事务恢复材料存于本机 `.devsys/local/`，不随 Git 提交；未完成事务日志不可当作普通可丢弃缓存清理，须先恢复或显式处置。单文件原子性、进程中断恢复与断电持久性分别验收，未验证的保证不得宣称；
- 派生进度和视图尽量由权威记录计算，不维护第二套任务状态；
- 打开项目时先检查事务恢复，再对账租约过期、孤儿领取与产物不一致；只报告命令不自动改写，发现待恢复事务须明确提示先恢复（见 §15.4）。

---

### 15.3 任务领取

```yaml
claim:
  owner: codex-local
  run_id: run-001
  claimed_at: ...
  lease_until: ...
  heartbeat_at: ...
```

如果 Agent 崩溃或超时：

```text
检测租约过期
  ↓
标记为 orphaned
  ↓
释放任务
  ↓
记录恢复事件
  ↓
允许重新领取
```

### 15.4 退避、停滞与恢复

- **退避**：失败重试使用指数退避，`delay = min(base × 2^(attempt-1), max)`；抖动必须**可复现**，以「任务标识 + 尝试次数」为种子的确定性偏移，保证重启后重试计划一致（采纳 Contrabass）；
- **续跑**：干净退出后的续跑检查使用短固定延迟，而不是指数退避；
- **停滞检测**：以「最后一次事件时间」为基准，超过阈值即终止本次尝试并按失败处理；阈值配置为 0 表示关闭检测；
- **孤儿恢复**：先完成事务恢复，再识别「已领取但没有运行记录」的业务孤儿；按恢复策略释放回 `unclaimed`，并记录恢复事件。`doctor` 和 `repair --dry-run` 只报告，不执行恢复写入；
- **对账**：每次打开项目或执行 `devsys status` 时，先检查未完成事务，再比对工作项、运行记录与产物；事务日志的确定性恢复不同于业务状态推断，后者不得静默修正；
- **状态修复**：状态文件与实际漂移（丢失、手工改动、合并后不一致）时，走「先推断、再确认、后重写」：从代码、Git 历史、产物与运行记录推断真实状态，列出一张提议表供确认，确认后才重写并校验；这是唯一允许把任务标记为「比原来更不完整」的路径（采纳 BMAD）。

---

## 16. 安全设计

### 16.1 权限边界

至少区分：

- 只读查询；
- 创建任务；
- 修改任务；
- 执行任务；
- 修改项目蓝图；
- 批准决策；
- 删除数据；
- 执行危险命令；
- 访问敏感文件。

---

### 16.2 危险操作

以下操作建议支持审批或策略控制：

- 删除文件；
- 修改生产配置；
- 迁移或重建项目状态存储与检索缓存；
- 执行网络或系统级命令；
- 修改安全策略；
- 发布版本；
- 覆盖重要架构决策；
- 批量关闭任务；
- 删除知识或历史记录。

---

### 16.3 敏感数据

不得将以下内容默认写入 Run 日志或上下文：

- API Key；
- 访问令牌；
- 密码；
- 私钥；
- 数据库连接密码；
- 用户隐私数据；
- 生产环境敏感配置；
- 写入 `.devsys/` 状态文件的任何密钥或令牌（只允许保存引用名）。

需要支持日志脱敏和敏感路径排除。

凭据隔离（采纳 Symphony 的做法）：

- 配置文件只写 `$VAR` 形式的引用，不写明文；
- 宿主侧的密钥环境变量不下传给 Agent 子进程；
- 需要外部凭据的操作由系统侧执行，Agent 通过工具调用间接完成。

---

## 17. UI 设计方向

UI 不是系统的唯一入口，但应提供可视化管理。

建议页面：

### 项目总览

显示：

- 项目目标；
- 当前阶段；
- 里程碑；
- 当前进度；
- 当前阻塞；
- 风险；
- 最近活动；
- 推荐下一步。

### 任务视图

支持：

- 列表；
- 看板；
- 树形层级；
- 依赖图；
- 状态筛选；
- 类型筛选；
- Agent 筛选；
- 时间筛选；
- 任务详情；
- 任务历史。

### 工作流视图

显示：

- 当前工作流；
- 当前步骤；
- 已完成步骤；
- 等待条件；
- 阻塞原因；
- 审批点；
- 下一步候选动作。

### Run 视图

显示：

- Agent；
- Harness；
- 模型；
- 工作区；
- 执行状态；
- 实时日志；
- 修改文件；
- 测试结果；
- 错误；
- 产物；
- 后续任务。

### 知识视图

支持：

- 项目结构；
- 模块；
- 文件；
- 符号；
- 依赖；
- 架构图；
- 知识来源；
- 新鲜度；
- 相关任务。

### 工作区视图（扩展功能）

工作区视图（Workspace）是对项目内状态的只读呈现，与执行工作区（worktree）不是同一概念。

数据来源：

- 只读取项目内 `.devsys/` 状态文件与 Git 状态，不引入第二套数据库，也不写回状态；
- 每个视图标注数据来源文件与基线提交，数据过期时明确提示（沿用 §10.2 的新鲜度思路）。

视图内容：

- 蓝图与目标：项目定位、范围、约束、里程碑；
- 进度：任务看板、阶段分布、依赖关系、阻塞与风险；
- 执行：Run 时间线与产物、失败与重试记录；
- 记录：决策、发现、错误与后续行动；
- 知识：模块索引、新鲜度、与任务的关联。

两种形态：

```bash
devsys workspace serve            # 本地只读服务，默认仅监听 127.0.0.1
devsys workspace build --static   # 生成静态站点，可提交或部署
```

边界：

- 只读：所有写入仍通过 MCP / CLI / HTTP 应用服务，避免出现第二套状态真相；
- 不属于 MVP（见 §18.3），属于阶段 7 扩展（见 §19）；
- 参考：agent-tasks 的实时看板是同类能力（见 §10.4），但其默认状态存放于用户级数据库，本系统的工作区视图必须读取项目内状态。

---

## 18. MVP 范围

### 18.1 MVP 目标

MVP 只解决一个核心问题：

> 多个 Harness Agent 在同一个项目中持续工作时，项目状态、任务、上下文和执行结果不会丢失，并且可以动态推进工作。

---

### 18.2 MVP 必须包含

#### 核心服务

- 项目创建和查询；
- 项目内状态目录（`.devsys/`）与 `devsys init` 初始化；
- 项目蓝图；
- 项目当前状态；
- WorkItem CRUD；
- 任务依赖；
- 基础状态机；
- 动态创建任务；
- 任务历史；
- Run 记录；
- 决策记录；
- Finding 记录；
- 产物登记。

#### 接入方式

- CLI；
- MCP；
- 文件交换格式。

#### Agent 使用能力

- 开始会话；
- 获取项目摘要；
- 获取当前任务；
- 获取任务上下文；
- 创建后续任务；
- 报告执行结果；
- 更新任务状态；
- 记录错误和发现。

#### 知识能力

MVP 可以先实现：

- 项目扫描；
- 文件和目录索引；
- 文档检索；
- 简单全文搜索；
- 按任务关联文件。

复杂语法树、向量检索和高级架构分析可以后置。

---

### 18.3 MVP 暂不包含

- 完整 Web Dashboard（可写、在线编辑型；只读的工作区视图作为扩展，见 §17）；
- 分布式调度；
- 多租户；
- 复杂权限系统；
- 全部 Harness 原生适配；
- 高级向量检索服务；
- 自动修改生产环境；
- 复杂模型路由；
- 完整企业级审批系统；
- 自动化发布平台；
- 全量复制 BMAD 工作流。

---

## 19. 推荐开发顺序

> 详细拆分（步骤、依赖、验收、回退）见 `docs/原始文档/实施计划.md`；本章只给宏观阶段与阶段目标。

### 阶段 1：核心数据和服务

实现：

- Project；
- WorkItem；
- Run；
- Artifact；
- Decision；
- Finding；
- Event；
- 项目内状态存储（`.devsys/` 文本文件）与文本检索；
- 调度态与文件级领取（claim / lease / heartbeat）；
- 基础状态机；
- CLI。

目标：

> 不依赖任何 Agent，也可以完整管理项目和任务。

---

### 阶段 2：MCP 接入

实现：

- MCP Server；
- 项目查询工具；
- 任务工具；
- 上下文工具；
- 运行记录工具；
- 决策和发现工具。

目标：

> Codex、OpenCode 等外部 Agent 可以直接查询和更新系统。

---

### 阶段 3：通用 Agent 工作协议

实现：

- `agent_session_start`；
- `workitem.next`；
- `context.for_workitem`；
- `run.create`；
- `run.complete`；
- `finding.create`；
- `decision.create`；
- `workitem.follow_up_create`。

目标：

> Agent 不需要理解内部存储格式，只需要遵循统一协议。

---

### 阶段 4：动态工作流

实现：

- Workflow 定义；
- 工作流实例；
- 步骤状态；
- 条件分支；
- 动态任务创建；
- 验证规则；
- 暂停和恢复；
- 审批点。

目标：

> 系统可以根据结果动态决定下一步，而不是执行固定清单。

---

### 阶段 5：代码知识层

实现：

- 项目扫描；
- 文件索引；
- 文档索引；
- 模块和依赖分析；
- 任务相关上下文；
- 增量刷新；
- 知识版本。

目标：

> Agent 能够按任务精准获取代码上下文。

---

### 阶段 6：Harness 执行适配

实现：

- Shell Adapter；
- Codex Adapter；
- OpenCode Adapter；
- Claude Code Adapter；
- 工作区管理；
- Run 进程管理；
- 日志流；
- 重试和恢复；
- 按 Symphony SPEC 核心一致性清单（SPEC §17.1–§17.7）验收执行层。

目标：

> 系统不仅能被 Agent 调用，还可以主动编排 Agent 执行。

---

### 阶段 7：Web UI 和高级能力

实现：

- 项目总览；
- 任务看板；
- 依赖图；
- Run 监控；
- 知识浏览；
- 工作区视图（只读：`devsys workspace serve` / 静态构建）；
- 跨设备状态同步检查（`devsys sync status`）；
- 工作流编辑器；
- 多项目；
- 远程执行；
- 权限和审批。

---

## 20. 关键风险与应对

### 20.1 过度复制现有项目

风险：

- 同时复制 BMAD、RepoWiki、Contrabass 的全部功能；
- 项目范围快速膨胀；
- 核心模型不稳定。

应对：

- 先定义自己的核心数据模型；
- 只吸收能力，不复制全部实现；
- 将外部项目视为参考模块或适配器；
- 先完成最小闭环。

---

### 20.2 多套状态真相

风险：

- BMAD 有一套状态；
- Contrabass 有一套状态；
- GitHub Issue 有一套状态；
- 工具的用户级数据库（如 `~/.agent-tasks/agent-tasks.db`）里还有一套状态；
- 自己的系统又有一套状态。

应对：

- 统一 WorkItem 作为内部任务真相；
- 外部 Issue 只做映射；
- BMAD Story 作为规格或工作项来源；
- 外部状态同步必须有明确优先级和冲突策略；
- 工具自身的用户级状态不纳入权威来源，只允许显式导入导出。

---

### 20.3 Skill 过多

风险：

- Agent 需要记住大量命令；
- 不同 Harness 使用方式不同；
- Skill 之间重复和冲突。

应对：

- 提供一个通用入口 Skill；
- 通过 `agent_session_start` 自动返回下一步；
- 将复杂操作封装为高层接口；
- Skill 只说明使用规则，不保存核心数据。

---

### 20.4 上下文过大

风险：

- 每次把完整项目 Wiki、全部任务和所有历史日志交给 Agent；
- 消耗上下文；
- 反而降低准确性。

应对：

- 分层上下文；
- 按任务检索；
- 记录上下文快照；
- 提供摘要和详情两级接口；
- 使用新鲜度和相关性过滤。

---

### 20.5 自动化过度

风险：

- Agent 自动创建大量无效任务；
- 自动修改关键架构；
- 自动关闭未完成任务；
- 自动执行危险命令。

应对：

- 动态任务创建支持策略；
- 高风险动作需要审批；
- 完成状态需要验证；
- 重要决策保留人工确认；
- 所有自动动作写入事件记录。

---

### 20.6 与 Harness 深度耦合

风险：

- 核心逻辑依赖某个 Harness 的命令格式；
- Harness 升级后系统失效；
- 无法接入新 Agent。

应对：

- 统一 Harness Adapter 接口；
- 核心只依赖标准输入输出、MCP、HTTP 和文件协议；
- 将具体命令放在适配器中；
- 使用能力探测而不是假定所有 Harness 相同。

---

### 20.7 项目内状态的仓库噪声与合并冲突

风险：

- 状态文件提交频繁，污染代码历史；
- 多台电脑或多个 Agent 同时写入，产生合并冲突；
- 大日志或二进制文件进入仓库，导致仓库膨胀；
- 误把密钥写入状态文件。

应对：

- 状态提交与代码提交分离（`chore(devsys):` 前缀），必要时使用独立分支；
- 一任务一文件、追加写 JSONL、事件按月分片，减少冲突面；
- `.cache/`、`local/`、大产物与日志不提交，只提交摘要与引用；
- `devsys sync status` 显式检测分叉，禁止静默自动合并状态；
- 密钥只保存引用名，提交前提供敏感内容检查。

---

### 20.8 对齐外部规范的漂移风险

风险：

- 对齐的 Symphony SPEC 仍标注为 preview，字段与语义可能变化；
- 照搬部分形成隐式依赖，升级时出现不兼容；
- 「替换」项（内存状态、以 tracker 为事实来源）被误读为漏实现。

应对：

- 固定基准：记录对齐 commit（见 §10.7），升级规范前先比对该文件新旧差异；
- 维护差异清单：所有「替换 / 扩展」项集中在 §10.7 表格中，偏离处必须写明原因；
- 验收只用 SPEC §17.1–§17.7 核心一致性清单，扩展项按需启用；
- 自研实现，不把该规范的参考实现作为运行时依赖或直接复制其代码。

---

## 21. 成功标准

系统达到以下条件，可以认为 MVP 成功：

1. 不启动任何特定 Harness，也可以创建和管理项目、任务和记录。
2. Codex、OpenCode 或其它支持 MCP 的 Agent 可以连接系统。
3. Agent 换一个 Harness 后仍能恢复项目上下文。
4. 任务可以动态创建、关联、阻塞、恢复和完成。
5. 每次执行都有独立 Run 记录。
6. 错误、发现和决策不会只存在于聊天记录中。
7. Agent 可以按任务获取相关代码上下文。
8. 任务状态、运行状态和项目状态相互关联。
9. 系统不会要求每个 Harness 安装大量专用 Skill 才能使用。
10. 核心数据模型不依赖 BMAD、Contrabass 或 RepoWiki 的内部格式。
11. 项目状态完全保存在项目仓库内（`.devsys/`），在另一台电脑上 clone 后即可恢复任务、蓝图与记录。
12. 系统不依赖任何跨项目共享数据库，单个项目可以独立迁移和备份。
13. 工作区视图可以只读展示项目内状态（进度、蓝图、Run、决策），并且不构成第二套事实来源。
14. 完成校验可以拦截「声称完成但分支未推进」的 Run，且不存在 fail-open 通过路径。
15. 知识新鲜度可计算（基线 commit + affected_pages + 退出码），人工编辑与锁定页面受到保护。

---

## 22. 最终建议

本项目最合理的方向不是开发：

- Contrabass 的 BMAD 插件；
- BMAD 的任务管理增强版；
- RepoWiki 的任务系统；
- 以用户级公共数据库承载多个项目任务状态的模式；
- 某一个 Harness 专用的 Skill 集合。

而是开发：

> 一个独立运行、与 Harness 解耦、通过 MCP、CLI、HTTP 和文件协议向任意 Agent 提供项目状态、动态任务、工作流、知识上下文和执行记录的开发基础设施。

各项目的能力边界建议如下：

| 项目或方法 | 在本系统中的角色 |
|---|---|
| [BMAD](https://github.com/bmad-code-org/BMAD-METHOD) | 需求、规划、架构、验收和开发方法 |
| [RepoWiki](https://github.com/JAYY513/repowiki) | 代码扫描、知识索引、上下文检索 |
| [Contrabass](https://github.com/junhoyeo/contrabass) | 可选执行后端（见 §10.3）；看板与工作区实现参考 |
| [Symphony](https://github.com/openai/symphony) | 执行层对齐规范（见 §10.7）：编排状态机、工作区不变量、重试与对账 |
| [agent-tasks](https://github.com/keshrath/agent-tasks) | 流水线任务管理、阶段门禁、工件版本化、看板视图（参考；其公共数据库存储模式不采纳） |
| 本系统 | 统一项目状态、动态工作流、跨 Harness 接入、长期上下文和记录 |

最终架构应遵循：

```text
任意 Harness Agent
  -> 标准接口
  -> 独立开发系统
  -> 统一项目与任务状态
  -> 动态工作流
  -> 知识上下文
  -> Agent 执行与验证
  -> 记录和持续更新
```

这才可以真正形成跨 Harness、跨会话、跨工具的长期开发基础设施。

最终选型（见 §10.7）：

> 执行层对齐 Symphony SPEC；项目状态、知识、记录层自研；Contrabass 仅作可选执行后端；BMAD 与 agent-tasks 只取方法与机制。
