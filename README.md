<div align="center">
  <img src="docs/assets/workloom-mark.svg" width="104" alt="Workloom 图标">
  <h1>Workloom</h1>
  <p><strong>把 AI 编码 Agent 的项目状态，织回你的 Git 仓库。</strong></p>
  <p>独立于 Harness、Git 原生、面向多 Agent 的开发基础设施。</p>

  <p>
    <a href="#方式-a让-agent-帮你接入推荐"><strong>Agent 快速开始</strong></a>
    ·
    <a href="docs/README.en.md">English</a>
    ·
    <a href="docs/使用手册.md">使用手册</a>
    ·
    <a href="docs/design.md">设计文档</a>
  </p>

  <p>
    <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white">
    <img alt="MCP" src="https://img.shields.io/badge/MCP-native-8B5CF6?style=flat-square">
    <img alt="Git native" src="https://img.shields.io/badge/state-Git--native-2563EB?style=flat-square&logo=git&logoColor=white">
    <img alt="Status" src="https://img.shields.io/badge/status-pre--release-F59E0B?style=flat-square">
  </p>
</div>

---

Workloom 是一个独立于具体编码 Harness 的 Agent 开发基础设施。它把任务、工作流、审批、运行记录、决策、发现和知识上下文保存为项目内 `.devsys/` 下的可读文本，并通过同一套 CLI 与 MCP 应用服务实施校验、并发控制和恢复。

这意味着你可以更换 Codex、Claude Code、OpenCode 或其他 Agent，而不丢失项目状态，也不需要维护跨项目数据库或常驻服务。

## 为什么是 Workloom

| 常见问题 | Workloom 的方式 |
|---|---|
| Agent 状态被锁在某个 Harness 中 | 状态模型独立，Agent 通过 MCP 或 CLI 接入 |
| 换设备后上下文难以恢复 | `.devsys/` 随 Git clone、pull 和 checkout 一起迁移 |
| 自动化直接改文件，容易绕过规则 | 写操作统一经过应用服务、版本守卫、门禁和事务 |
| Agent 显示“完成”，但没有代码证据 | 完成前校验工作区 HEAD，证据不足时转入人工复核 |
| 项目知识很快过期 | 可计算知识新鲜度，支持受保护页面与增量刷新 |
| 看板成为第二事实来源 | 看板与静态站都是投影，`.devsys/` 始终是唯一事实来源 |

## 核心能力

- **Git 原生状态**：YAML、JSONL 与 Markdown 可读、可审计、可 diff。
- **Harness 解耦**：内置 Codex、Claude Code、OpenCode 与 Shell 适配器。
- **项目规划上下文**：统一保存项目蓝图引用、目标、范围、约束、里程碑和当前状态。
- **动态任务系统**：Agent 可按蓝图创建任务与子任务，维护优先级和依赖关系。
- **完整工作流**：步骤、条件、门禁、审批、领取、租约、重试与恢复。
- **MCP 原生接入**：按 `session`、`executor`、`reviewer`、`admin` profile 暴露能力。
- **可靠写入**：原子替换、乐观并发、文件锁、跨文件事务和崩溃恢复。
- **上下文与知识**：任务上下文装配、知识扫描、校验、新鲜度与增量刷新。
- **可验证执行**：隔离 worktree、运行事件流、完成校验与显式人工放行。
- **只读工作区视图**：终端摘要、离线静态站与本地只读服务。
- **跨设备接力**：分叉检查、租约审计、保守归档与确定性修复。

## 从项目蓝图到交付

Workloom 管理的不只是 Agent 的一次运行，而是项目从规划到交付的完整上下文：

| 层级 | Workloom 管理的内容 | Agent 可以做什么 |
|---|---|---|
| **项目蓝图** | 项目目标、范围、约束、技术栈、蓝图 Artifact 引用 | 读取项目方向，在蓝图缺失时先向用户澄清 |
| **里程碑与状态** | 当前阶段、里程碑、风险、阻塞项、下一步重点 | 汇总进度，判断应该继续交付还是回到规划 |
| **动态任务** | 任务、子任务、优先级、验收条件、依赖关系 | 根据目标拆分工作，在执行中发现并创建后续任务 |
| **工作流程** | 步骤、条件、阶段门禁、质量门和审批策略 | 选择匹配的流程，按规则推进而不是自由跳步 |
| **执行与验证** | Agent 领取、worktree、Run、日志、重试和完成证据 | 执行代码变更，并用 Git 与测试证据证明完成 |
| **项目记忆** | 决策、发现、产物、事件和知识上下文 | 为下一位 Agent 留下可追踪、可恢复的上下文 |

```text
项目蓝图
   ↓
目标 / 范围 / 里程碑
   ↓
动态任务 ── 子任务 ── 依赖
   ↓
工作流 ── 门禁 ── 审批
   ↓
Agent 执行 ── 验证 ── 人工复核
   ↓
项目状态与知识沉淀 ──→ 下一轮规划
```

## 架构

<div align="center">
  <img src="docs/assets/workloom-architecture.svg" width="100%" alt="Workloom 架构：编码 Agent 通过 MCP、CLI 或文件交换接入统一核心，状态保存于项目内 .devsys 并由 Git 同步">
</div>

所有入口共享 `internal/app` 中的业务规则。CLI 和 MCP 不是两套实现，因此 Agent 无法通过切换入口绕过门禁、审批、版本守卫或事务恢复。

```text
Coding Agents
    │
    ├── MCP ──┐
    ├── CLI ──┼── Workloom Core ── .devsys/ ── Git
    └── Files ┘      │
                     ├── Workflows & approvals
                     ├── Dispatch & worktrees
                     ├── Context & knowledge
                     └── Transactions & recovery
```

## 快速开始

### 方式 A：让 Agent 帮你接入（推荐）

你不需要先学会 Workloom 的命令。打开 Codex、Claude Code、OpenCode 或其他可执行终端命令的编码 Agent，把下面这段话直接发给它：

```text
帮我在当前项目接入 Workloom（https://github.com/JAYY513/Workloom）：

1. 若 workloom 未安装（workloom --version 无输出）：优先
   `npm install -g @kaki317/workloom`（Node.js ≥ 18）；平台不支持或失败时
   按该仓库 INSTALL.md 的 §1 安装，装完自证版本。
2. 在目标项目目录执行 init → `workloom setup`（该目录已是 git 仓则建议在仓库根；
   非 Git 原型可直接 init）。`setup` 一步完成接入并自检（init → 工作流模板 →
   wire → wire --check → prime → 蓝图检查 → doctor），失败即停。

任一步失败就停下报告；走脚本安装路径时不要跳过哈希校验；不要直接修改 .devsys/ 内的
受管文件（所有写入走 workloom CLI 或 MCP）；若某一步必须我手动操作，
只告诉我那一步，然后继续完成剩余工作。
```

接入完成后，你可以直接这样和 Agent 沟通：

```text
请先用 Workloom 读取项目蓝图、目标、范围、里程碑和当前状态。
然后规划并实现用户登录功能：把工作拆成有验收条件的任务与子任务，
维护依赖关系，选择合适的工作流推进，并在需要我决策或审批时暂停。
```

```text
继续上次的工作。先读取 Workloom 中的当前里程碑、未完成任务、
相关决策和风险；执行过程中发现的新工作请创建为任务并建立依赖。
完成后记录验证证据，更新项目状态，并告诉我结果和剩余风险。
```

Agent 会从仓库中的 `AGENTS.md` 和 `.agents/skills/devsys/` 获得操作规范，通过 `workloom prime` 恢复上下文；你仍然可以在 Git 中审查所有状态变化。

### 方式 B：手动接入

### 1. 安装

完整安装与接入流程（含校验、回退与排障）见 [INSTALL.md](INSTALL.md)；下面是最短路径。

```bash
npm install -g @kaki317/workloom   # 需要 Node.js ≥ 18
workloom --version                 # 自证
```

就这么一条：升级就是重跑同一条命令，平台二进制与完整性由 npm 处理。当前平台包只发布了
Windows x64；macOS / Linux / Windows ARM64，以及 Release 脚本、`go install`、源码构建等
其余安装方式见 [INSTALL.md](INSTALL.md) §1。

> 改名兼容：旧版主命令 `devsys` 仍可用（同一程序的兼容别名）；状态目录仍是 `.devsys/`。
> npm 包只装 `workloom` 一个 bin；装安装脚本才同时装入 `devsys`。

### 2. 初始化项目

在任意目录运行一条命令（Git 仓库根目录最佳；非 Git 目录也可用）：

```bash
workloom setup
```

它按顺序完成 `init`（幂等）→ 安装 `quick-fix` 起手工作流（已有文件不动）→
`wire`（AGENTS.md 纪律块 + Agent Skill）→ 配置校验 → 环境检查 → `prime` →
蓝图检查（未声明只报告，不猜）→ `doctor`，任一步硬失败即停。重跑安全：
已存在的文件一律保留。

装好二进制后，`workloom mcp install` 把 devsys 注册进检测到的 MCP 客户端（服务器名仍为 `devsys`，兼容既有客户端配置）
（Codex / Claude Code / OpenCode；显式 `--client` 可强制）。默认只预览不写；`--apply` 才写入，
已有条目不覆盖，`--force` 才替换。无法安全合并时拒绝并指向 `wire --print-mcp`。
`--apply` 之后会现场起一次 `mcp serve` 做启动检查（`probe: ok (N tools)`；起不来则 `probe: failed` 并以退出码 3 结束，配置已写入但没接上；沙箱里不能起子进程时用 `DEVSYS_MCP_PROBE=0` 跳过）。

如果客户端没有被自动检测，或你想手动接入其他支持 stdio MCP 的 Agent，可先打印对应客户端片段：

```bash
workloom wire --print-mcp codex
workloom wire --print-mcp claude
workloom wire --print-mcp opencode
```

把输出粘贴到该 Agent 的 MCP 配置中。通用 MCP 语义是启动本地命令
`workloom mcp serve --profile session,executor --tier core`，服务器名使用 `devsys`；不要把
`npx` 作为长期 MCP 命令。客户端找不到 `workloom` 时，把 `command` 换成 `workloom.exe` 的绝对路径，或使用生成片段中的 npm wrapper 命令。

### 通用 stdio MCP 配置模板

其他支持 `mcpServers` JSON 格式的 Agent 可复制下面的模板，只替换三个占位值：`<node.exe>`、`<workloom.js>` 和 `<project-root>`：

```json
{
  "mcpServers": {
    "devsys": {
      "type": "stdio",
      "command": "<node.exe>",
      "args": [
        "<workloom.js>",
        "mcp", "serve",
        "--profile", "session,executor",
        "--tier", "core"
      ],
      "cwd": "<project-root>"
    }
  }
}
```

Windows npm 安装的典型路径是：

```text
command: C:\\Program Files\\nodejs\\node.exe
workloom.js: C:\\Users\\<user>\\AppData\\Roaming\\npm\\node_modules\\@kaki317\\workloom\\bin\\workloom.js
```

如果 Agent 支持直接执行命令，也可以把 `command` 改成 `workloom.exe`，并把 `args` 从 `mcp` 开始。`cwd` 必须是实际项目根目录；不同 Agent 对字段名和配置文件位置可能不同，无法识别该模板时使用其文档要求的等价 stdio 字段。

想逐步执行或换模板时，底层命令仍然都在：

```bash
workloom init
workloom config check
workloom wire
workloom session start
workloom setup --template reference-template   # 或 workflow init --template <id> 单装工作流
workloom workflow check                        # 校验全部策略文件
```

模板清单单源化：`setup --template` 与 `workflow init --template` 的
完整模板列表见该命令的用法输出（内嵌于二进制，随版本更新）。

`workloom init` 幂等创建 `.devsys/`；`wire` 将精简的协作纪律写入 `AGENTS.md`，且保留文件中的手写内容。
新项目的 `.devsys/workflows/` 是空的：没有工作流时任务照常推进，但派发给 Agent 的轮次提示词里不会有流程约束。

### 3. 接着用它

装好之后的日常使用——创建并推进工作、质量门与阶段门、连接 MCP 客户端、
查看项目状态——见《使用手册》（`docs/使用手册.md`）。最常用的三个命令：

```bash
workloom next            # 现在该做什么（只读）
workloom prime           # 会话起手：项目事实 + 在飞工作 + 推荐下一步
workloom workitem create # 新任务入口
```

## 一个典型闭环

```text
定义工作流 → 创建工作项 → 就绪检查 → Agent 领取
     ↑                                  ↓
人工审批 ← 复核与验证 ← 记录证据 ← 隔离 worktree 执行
```

1. 工作流定义步骤、条件、门禁、审批与执行限制。
2. `next` 只读评估风险，并给出确定性的下一步建议。
3. `dispatch` 恢复异常状态、规划容量、领取任务并启动指定 Harness。
4. Agent 在隔离 worktree 中执行，日志、命令和结果进入 Run 事件流。
5. `run complete` 校验 Git HEAD 是否真实推进；证据不足则 fail closed。
6. 决策与发现以引用传播，供后续 Agent 恢复上下文。

## 状态模型

```text
.devsys/
├── project.yaml          # 项目标识与元数据
├── config.yaml           # 调度、知识、工作区与默认策略配置
├── workitems/            # 工作项与工作流实例
├── workflows/            # Markdown 工作流策略
├── approvals/            # 审批请求与消费状态
├── runs/                 # Run 摘要与 JSONL 事件流
├── decisions/            # 架构与产品决策
├── findings/             # 调研、风险与发现
├── artifacts/            # 产物及不可变版本链
├── events/               # 按月分片的项目事件
├── knowledge/            # 知识快照与新鲜度状态
└── archive/              # 可审计的保守归档
```

`.devsys/` 是唯一事实来源。只读工具可以直接消费这些文本；所有运行时写入都应经过 CLI 或 MCP，以保留并发与一致性保证。

## 设计原则

1. **项目自治**：状态属于项目，而不是某台机器、某个 Agent 或某个 SaaS。
2. **先证据，后状态**：没有可验证证据，就不能把任务标记为完成。
3. **默认拒绝**：版本冲突、策略损坏、审批缺失和恢复未完成时 fail closed。
4. **文本优先**：数据可以被人阅读、Git 审查，并由简单工具交换。
5. **投影不是事实**：Dashboard、静态站和 Contrabass 只是执行或观察后端。
6. **单写者接力**：当前跨设备模型是明确交接，不支持多设备同时写入。

## 文档

| 文档 | 用途 |
|---|---|
| [使用手册](docs/使用手册.md) | 15 分钟上手、日常操作与完整命令路径 |
| [发布流程](docs/发布流程.md) | 发版 runbook：tag/Actions 触发、产物校验、回退与故障处理 |
| [迁移指南](docs/迁移指南.md) | Schema 升级、二进制替换与回退 |
| [设计文档](docs/design.md) | 架构原则、数据模型与设计取舍（活文档） |
| [贡献指南](CONTRIBUTING.md) | 环境要求、提交规范与文档纪律 |
| [更新日志](CHANGELOG.md) | 版本历史与用户可感知的变更 |
| [RepoWiki](docs/repowiki/index.md) | 自动生成的模块级项目知识 |

## 开发

要求 Go 1.26 与 Git：

```bash
make build
go test ./...
go vet ./...
```

项目包含分层单元测试、CLI/MCP 集成测试，以及 `scripts/smoke-m*.{sh,ps1}` 端到端剧本。测试策略优先覆盖行为、边界与回归，而不是单纯追求覆盖率数字。

## 当前状态与边界

Workloom 已完成 M0–M9 实施与 17 项成功标准验收，当前仍处于 **pre-release** 阶段。

- 核心路径以本地 CLI、stdio MCP 与 Git 为主，不提供远程中心协调服务。
- 跨设备协作采用单写者接力；同时写入由 Git 合并流程人工处理。
- Contrabass 集成已通过 fixture 闭环，真实环境字段仍标记为 `[UNVERIFIED]`。
- 采用 MIT 许可证（[LICENSE](LICENSE)）；贡献约定见 [CONTRIBUTING.md](CONTRIBUTING.md)，版本历史见 [CHANGELOG.md](CHANGELOG.md)。

## 参与贡献

提交改动前，请阅读以下步骤（完整约定见 [CONTRIBUTING.md](CONTRIBUTING.md)）：

1. 先阅读 [设计文档](docs/design.md) 中的设计原则。
2. 为行为变化补充高价值测试，并运行 `go test ./...` 与 `go vet ./...`。
3. 保持 `.devsys/` 为唯一事实来源，不引入绕过应用服务的写路径。
4. 在 Pull Request 中说明动机、兼容性影响、验证方式和回退路径。

---

<div align="center">
  <sub>Workloom keeps the project memory with the project.</sub>
</div>
