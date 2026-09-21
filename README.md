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
    <a href="docs/原始文档/独立于Harness的Agent开发基础设施方案.md">设计方案</a>
    ·
    <a href="docs/M9-验收报告.md">验收报告</a>
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
请帮我在当前 Git 项目中接入 Workloom（https://github.com/JAYY513/Workloom）。

请你自行完成：
1. 检查 devsys 是否已安装（devsys --version 能输出版本号即算）。如果没有：
   a. 打开 https://github.com/JAYY513/Workloom/releases/latest ，下载 install.sh
      （Windows PowerShell 用 install.ps1）；
   b. 用同页的 checksums.txt 校验脚本哈希（Linux/macOS/Git Bash：
      sha256sum install.sh；Windows PowerShell：
      (Get-FileHash install.ps1 -Algorithm SHA256).Hash.ToLower()），不匹配就停下报告；
   c. 执行安装：bash install.sh --tag <该版本>（PowerShell：
      powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1 -Tag <该版本> -AddToPath）；
   d. 确认 devsys --version 输出与所装版本一致。私有仓库下载需要 gh auth login，
      未登录时会自动回退源码构建（需本机有 Go + Git）。
2. 在项目根目录运行 devsys init。
3. 运行 devsys workflow init --template quick-fix 安装一份起步工作流
   （另有 feature-development / architecture-change / reference-template 可选，
   装好后按本项目实际改写；没有工作流任务也能推进，但提示词里没有流程约束）。
4. 运行 devsys wire（默认写入 AGENTS.md 纪律块与 .agents 技能文件），
   再按你的客户端运行 devsys wire --print-mcp codex|claude|opencode 完成 MCP 配置。
5. 运行 devsys wire --check 检查接入结果，再运行 devsys prime 读取项目状态。
6. 检查项目蓝图、目标、范围、约束、里程碑、现有任务和工作流。
   如果蓝图尚未配置，请明确告诉我并先询问项目目标，不要直接假设。
7. 最后用自然语言告诉我：接入是否成功、当前项目状态、建议的下一步。

不要直接修改 .devsys 内的受管状态文件；所有写入都通过 devsys CLI 或 MCP 完成。
如果某一步必须由我手动操作，只告诉我那一个明确步骤，然后继续完成剩余工作。
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

Agent 会从仓库中的 `AGENTS.md` 和 `.agents/skills/devsys/` 获得操作规范，通过 `devsys prime` 恢复上下文；你仍然可以在 Git 中审查所有状态变化。

### 方式 B：手动接入

### 1. 安装

从源码构建，仓库已提交 `vendor/`，可离线完成：

```bash
git clone https://github.com/JAYY513/Workloom.git
cd Workloom
# Linux / macOS
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/devsys ./cmd/devsys
# Windows（产物带 .exe 扩展名，Git Bash / PowerShell / cmd 均可直接执行）
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/devsys.exe ./cmd/devsys
```

> Windows 注意：`install.sh` 与上面的构建命令请在 **Git Bash** 中运行；若
> `bash.exe` 解析到 WSL，脚本会按 Linux 分支处理（WSL 内通常没有 Go/Git，
> 报 “go is not installed” 之类错误），不是脚本故障。

已装 Go 的用户也可以跳过下载直装（私有仓库需先让 Go 直连仓库，跳过校验和数据库）：

```bash
go env -w GOPRIVATE=github.com/JAYY513/Workloom
go install github.com/JAYY513/Workloom/cmd/devsys@v0.1.7
```

这样装的二进制同样能自报版本（`devsys --version` → `devsys v0.1.7`；
zip 模块无 VCS stamping，故无 commit 后缀）。

> 说明：`v0.1.0` 起提供 Release 二进制（Linux/macOS/Windows × amd64/arm64，SHA-256 校验）。
> 本仓库为私有仓库：下载 Release 产物需要先 `gh auth login`（或在浏览器登录后下载）。
> 未登录时 `install.sh` 会自动回退到 `git clone --branch <tag> + go build`（需本机有 Go + Git，且 clone 同样需要仓库访问权限）。
> `v0.1.1` 起二进制与 `checksums.txt` 均由 CI 发布（GNU 校验和格式）。`v0.1.2` 起默认 MCP `--tier core`
> 严格为 19 项日常子集（`run_fail` / `run_cancel` 不再随 `run_complete` 泄漏）；`v0.1.4` 起
> core 档补入 `workitem_release`（与 claim 配对），严格为 20 项。`v0.1.4` 还带来：租约 token
> 侧车脱敏（scheduling/ 随 git 但无凭证）、质量门中日韩文字加权、run complete 成功即释租约、
> `workitem update` / `artifact register` 强制 `--actor/--reason` 审计、族级 `--help` exit 0、
> `wire` 默认含 skill。`v0.1.3` 起补齐首启接入闭环
> （蓝图写入 `project update --blueprint-artifact`、`mcp serve` 空工具集报错、损坏受管 YAML exit 4、
> `workflow init --template` 内嵌模板）。`v0.1.5` 修复复测遗留：run complete 拒绝文案与实际
> 状态一致（被 review 门拦时提示先补 comment）、`init` 生成项目级 .gitattributes（.devsys/
> .agents 固定 LF，消除 Windows 换行噪音）。`v0.1.6` 起 install 脚本本身随 release 发布，`v0.1.7` 起支持 `go install`（模块路径规范化），
> 「复制给 Agent」提示词可直接从 release 页下载脚本，无需克隆仓库。想要与本文档一致的行为
> （`prime`、`--latest`、`--tier core` 20 项、`next` 与 `claim` 同源的质量门判定、`default_policy`、
> 安装链修复），请用 `v0.1.7` 或更新版本，或从源码构建。

```bash
# Linux / macOS
bash scripts/install.sh --tag v0.1.7

# Windows PowerShell
powershell -NoProfile -ExecutionPolicy Bypass \
  -File scripts/install.ps1 -Tag v0.1.7 -AddToPath
```

Release 构建覆盖 Linux、macOS 与 Windows 的 `amd64` / `arm64`。

### 2. 初始化项目

在任意 Git 仓库根目录运行：

```bash
devsys init
devsys config check
devsys wire
devsys session start
```

`devsys init` 幂等创建 `.devsys/`；`wire` 将精简的协作纪律写入 `AGENTS.md`，且保留文件中的手写内容。

新项目的 `.devsys/workflows/` 是空的：没有工作流时任务照常推进，但派发给 Agent 的轮次提示词里不会有流程约束。用内嵌模板装一份起手并按项目改写：

```bash
devsys workflow init --template quick-fix            # 最简；另有 feature-development / architecture-change
devsys workflow init --template reference-template   # 参考级模板：姿态、步骤、证据、停止条件
devsys workflow check                                # 校验全部策略文件
```

### 3. 创建并推进工作

```bash
devsys workitem create \
  --title "Add OAuth login" \
  --actor alice \
  --reason "Q4 roadmap" \
  --description "- 背景：… / - 范围：… / - 验收：…" \
  --acceptance "登录成功回跳,失败可重试"

devsys workitem transition \
  --id WLM-1 \
  --to ready \
  --actor alice \
  --reason "spec approved" \
  --latest

devsys workflow start --id WLM-1 --policy quick-fix --actor alice --reason "begin" --latest
devsys next
devsys dispatch --once --actor alice --reason "run ready work"
```

质量门与阶段门在**工作项有生效策略时**才参与判定：绑定实例（上面的 `workflow start`）或项目级 `default_policy`（见 `.devsys/config.yaml`）。`quick-fix` 示例要求描述 ≥40 字、有验收标准等；`--acceptance` 可在 `create` 直接给，也可事后用 `workitem update --acceptance a,b` 补。两者都没有时 `claim` 会打印一行 `warning:` 说明门禁未生效（项目里有策略文件才提示）；`devsys next` 用与 `claim` 相同的质量门判定，被拦的 ready 任务会进 `quality_blocked` 风险并给出补救命令。

`--latest` 适合操作者明确要求基于最新版本执行的场景；自动化集成应先读版本哈希，再用 `--expect <hash>` 写入。

### 4. 连接 MCP 客户端

让 Workloom 为你的客户端生成 stdio 配置：

```bash
devsys wire --print-mcp codex
devsys wire --print-mcp claude
devsys wire --print-mcp opencode
```

底层服务命令为：

```bash
devsys mcp serve --profile session,executor --tier core
```

默认 `--tier core` 是 20 项日常子集（含 `workitem_release`，与 claim 配对）。`run_update` / `run_fail` / `workitem_block` 等进度、失败与受阻工具在 `--tier standard`；MCP 优先的 Agent 对这些步骤请用 CLI，或把 serve 改成 `--tier standard`。

### 5. 查看项目状态

```bash
devsys workspace view
devsys workspace serve --port 8080
```

本地服务默认只监听 `127.0.0.1`，页面与 `/api/view` 均为只读。

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
| [设计方案](docs/原始文档/独立于Harness的Agent开发基础设施方案.md) | 架构原则、数据模型与设计取舍 |
| [实施计划](docs/原始文档/实施计划.md) | M0–M9 的实施步骤与验收条件 |
| [M9 验收报告](docs/M9-验收报告.md) | 17 项成功标准及复现证据 |
| [M6 一致性自检](docs/M6-一致性自检.md) | 与 Symphony SPEC 的逐项对照 |
| [开发记录](docs/开发记录.md) | 关键决策、偏差、问题与演进历史 |
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
- 仓库尚未加入开源许可证。在正式公开发布前必须选择并提交 `LICENSE`；在此之前，默认版权规则仍然适用。

## 参与贡献

项目正在为公开开源做准备。提交改动前，请：

1. 先阅读 [设计方案](docs/原始文档/独立于Harness的Agent开发基础设施方案.md) 中的设计原则。
2. 为行为变化补充高价值测试，并运行 `go test ./...` 与 `go vet ./...`。
3. 保持 `.devsys/` 为唯一事实来源，不引入绕过应用服务的写路径。
4. 在 Pull Request 中说明动机、兼容性影响、验证方式和回退路径。

---

<div align="center">
  <sub>Workloom keeps the project memory with the project.</sub>
</div>
