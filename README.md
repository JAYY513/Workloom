# Workloom

独立于 Harness 的 Agent 开发基础设施。状态保存在项目仓库内（`.devsys/`），
通过 Git 在设备之间同步，不依赖跨项目共享数据库。

## 文档

| 文档 | 说明 |
|---|---|
| [方案](docs/原始文档/独立于Harness的Agent开发基础设施方案.md) | 规格与选型依据，冻结；只有故意偏离设计时才修改 |
| [实施计划](docs/原始文档/实施计划.md) | M0–M9 共 54 步，每步含产出、依赖与验收命令 |

## 当前进度

| 里程碑 | 状态 |
|---|---|
| M0.1 语言与工程骨架 | 已完成（Go 1.26，`devsys --version` 可跑） |
| M0.2 CLI 骨架与 `devsys init` | 已完成（2026-09-17；退出码 0/1/2/3、`--json`/`--quiet`、`init` 幂等、用户级注册表、非 Git 拒绝） |
| M0.3 文本存储基元 | 已完成（2026-09-17；原子写、JSONL 追加与断尾检测、稳定键序序列化、项目级写锁、乐观并发版本校验、可恢复跨文件事务；`gopkg.in/yaml.v3` 已 vendored） |
| M0.4 严格配置解析 | 已完成（2026-09-17；`project.yaml`/`config.yaml` 严格解析：未知键、类型错误、缺必填项逐条报 `文件:行号: 字段: 原因`；受管文件 `schema_version` 校验，未知版本拒绝写入、只读诊断可用；新增 `devsys config check` 与退出码 4） |
| M1.1 领域类型与序列化 | 已完成：八类模型、版本化 YAML/JSON 往返、稳定字节、config/init 统一 Project 类型 |
| M1.2 WorkItem 读写与 ID 分配 | 已完成：internal/workitem，`<前缀>-<序号>` CAS 分配、并发唯一、稳定排序、乐观更新 |
| M1.3 事件流（JSONL 按月分片） | 已完成：internal/events，`events/<YYYY-MM>.jsonl` 追加/过滤/评论线程，断尾检测阻断追加 |
| M1.4 Run 记录与上下文快照 | 已完成：internal/run，`run-<日期>-<序号>`，§11.3 ContextSnapshot 随 Run 往返 |
| M1.5 记录（Decision/Finding/Artifact） | 已完成：internal/record，一记录一文件；Artifact 版本链不可变追加 |
| M1.6 检索（文本扫描） | 已完成：internal/search + `devsys search`，无索引扫描，排除 `.cache/`、`local/` |
| M1.7 里程碑剧本与快照 | 已完成：PowerShell / Bash 剧本、真实领域层回读验收、13 文件示例快照 |
| M2.1 工作项状态机 | 已完成：`internal/domain/state.go` 九状态保守图；`workitem.Transition/ApplyRepair` 带原因/操作者/快照校验，状态与事件同事务 |
| M2.2 调度态与文件级领取 | 已完成：`unclaimed/claimed/running/retry_queued/released` + `.devsys/scheduling/<id>.yaml` 租约；`workitem.Claim` 原子创建 Run 与事件（CLI `workitem claim`），token fencing，并发唯一 |
| M2.3 孤儿恢复与只读对账 | 已完成：`devsys doctor`（只读，不建锁、不恢复）与 `devsys recover`（先确定性事务恢复，再按策略释放过期/孤儿领取，幂等） |
| M2.4 状态修复（推断→确认→重写） | 已完成：`devsys repair --dry-run` 提议表 + 摘要；`--apply --confirm <digest>` 在写事务内逐项复核证据后才重写，唯一允许降低完成度的路径 |
| M2.5 里程碑剧本与快照 | 已完成：`scripts/smoke-m2.*` 领取 → 模拟崩溃 → doctor → recover → 确认修复；真实进程中断回归测试覆盖提交点两侧 |
| M3.1 策略文件格式与解析 | 已完成：`internal/workflow` 解析 `.devsys/workflows/<id>.md`（front matter + 提示词正文），错误逐条定位 `文件:行号:字段:原因`；未知键记录为 warning 不崩；`devsys workflow check`（exit 0/3/4，`--json`）；示例策略三份 |
| M3.2 模板渲染与变量引用 | 已完成：`{{name}}` 模板严格渲染（缺失变量=分类错误并列名，绝不静默空串）；`RenderError` 三类（unknown_variable/template_syntax/env_undefined）；`ExpandEnv` 解析 `$VAR`/`${VAR}`（未定义报错、`$$` 转义、加载期不入库）；`workflow check` 加载期校验模板语法并报 body 绝对行 |
| M3.3 门禁、钩子与限额字段 | 已完成：进入阶段门禁（产物/数量/评论/审批/豁免；审批在 M3.7 前 fail-closed）、领取质量门（确定性评分，无模型调用，拦截返回改进项）、完成时同事务记录传播（decision/finding → 父任务与直接兄弟 `context_refs` + `record_propagated` 事件，不改状态、幂等）；`transition`/`claim` 拒绝 exit 4 并逐条渲染；`scripts/smoke-m3.sh` |
| M3.4 就绪门与下一步优先级 | 已完成：`internal/next` 纯函数 `Evaluate`（PASS/CONCERNS/FAIL；风险：租约过期/孤儿领取/不可读调度文件/滞留审查/阻塞/元数据非法/检查受限）+ §7.4 六级 next（recover_claim→review→start→start_backlog→milestone_review→report_done）；`devsys next` 只读（pending 事务时 FAIL 且不输出业务事实），`--json`，不含时间估算 |
| M3.5 策略热载入与 last-known-good | 已完成：`workflow.Resolve` 每次读取重新校验；成功刷新 `.devsys/.cache/workflows/<id>.md` 快照，失败回退 LKG 并把当前失败定位交给调用方；坏策略阻塞 `claim`（exit 4 + 原因），`transition` 门禁按 LKG 评估并打印 warning，`next` 增 `invalid_policy` 风险且仍可用 |
| M3.6 工作流实例与步骤推进 | 已完成：实例存于工作项 `workflow` 字段（含 `paused`）、步骤历史走事件流；`when` 加载期解析+校验（7 字段白名单、`== != < > in exists`、类型规则；未知字段/操作符=策略错误）；`devsys workflow start\|next\|step-complete\|pause\|resume\|cancel`（start 拒绝坏策略；推进允许 LKG+warning；跳步/条件不满足 exit 4 + 允许候选）；推进不写状态与调度态 |
| M3.7 审批服务 | 已完成：`internal/approval`（一审批一文件 `approval-<N>`、Request/Decide/ConsumeTx/List/Get，全部走事务+CAS）+ CLI `approval list\|request\|approve\|reject`；`require_approval` 门禁联动（批准后在推进事务内消费并写 `consumed_at`+`approval_consumed`）；`requested_status` 实现「离开阶段即作废」；拒绝默认 `block`（理由与引用进 `status_changed`）、`on_reject: regress:<status>` 可回退；`next` 增 `pending_approval` 风险与优先级 2 |
| M4.1 MCP Server 骨架 | 已完成：`internal/mcp` stdio MCP 服务 + `devsys mcp serve [--profile ...]`；工具按 profile 分级暴露（默认 session+executor）；`health` 只读工具；真实 MCP 客户端 codex 0.144.1 连接并调用通过 |
| M4.1a 采用官方 MCP Go SDK | 已完成（2026-09-18）：传输层替换为 `github.com/modelcontextprotocol/go-sdk`（已 vendored，离线构建保持）；自研 JSON-RPC/schema 校验层删除，协议协商、`tools/list`/`tools/call`、输入校验与错误包装由 SDK 负责；错误分类与 profile 过滤保留在本地；codex 实连复验通过 |
| M4.2 工具集：项目/工作项/工作流/审批 | 已完成：新增共享应用服务 `internal/app`（CLI 业务逻辑迁入，CLI 变薄渲染层，MCP 工具调用同一实现——门禁/质量门/审批消费/版本守卫/事务恢复只有一份）；工具面 `project_*`/`workitem_*`/`workflow_*`/`approval_*`（写工具要求 expect）；CLI 补齐 `project` 与 `workitem list\|update\|release\|start\|block\|complete\|comment\|dep`、`workflow list\|get`、`approval get`；错误分类统一（usage/precondition/invalid/internal）双面同形 |
| M4.3 工具集：运行/记录/产物/知识 | 已完成：`decision_*`/`finding_*`/`event_*`/`artifact_*`/`run_*`（读+证据累积+续租）/`context_*`/`knowledge_status`（共 58 个工具）；CLI 增 `decision`/`finding`/`event`/`artifact`/`run`/`context`/`knowledge status`；记录查询带版本哈希、CLI 与 MCP 读取一致（验收测试钉死）；artifact 版本链线性（同父二次追加被拒 `ErrSuperseded`，快照校验在写事务内）；上下文在检查不可信时不输出计数并给 notice；Run 生命周期推进（`run_verify/complete/fail/cancel`）归 M6，知识索引层查询（`knowledge_search/get_*`）归 M5（偏差已记录） |
| M4.4 会话接口 agent_session_start | 已完成：MCP `agent_session_start` + CLI `session start [--compact]`——一次调用返回会话身份（回显不落盘）、项目事实、当前状态、在飞工作项、§7.4 推荐动作（含可执行命令）与上下文引用；检查不可信时不输出计数/工作项内容并给 notice |
| M4.5 文件交换协议与退出码 | 已完成：退出码表固化（0/1/2/3/4 + 知识 10/11 预留）并由测试钉住；`--json` 信封与 `--jsonl`（列表一行一条记录，7 个命令）双格式，互斥；README 协议节 + `scripts/exchange-demo.sh` 可运行解析示例（文件读取 / jsonl / json / 退出码分支） |
| M4.6 AGENTS.md 管理块（devsys wire） | 已完成：`devsys wire [--dry-run]` 幂等注入标记区间（块外内容与其他工具的管理块逐字节保留；重复/残缺标记 exit 4 拒绝；CRLF 一致、权限保留、原子写）；`--dry-run` 输出变化区域预览，`--json` 给结构化结果 |
| M4 收尾：里程碑剧本与提交 | 已完成：`scripts/smoke-m4.{sh,ps1}` 双平台实跑通过（MCP 面 / CLI-MCP 读写一致 / 会话接口 / 退出码分支 / jsonl / wire 幂等 / 知识降级）；repowiki 增量刷新至 M4 基线；`feat(m4)` + `docs(repowiki)` 两个提交 |
| M6.2 工作区管理与不变量 | 已完成：`internal/workspace`（目录名净化 + 哈希后缀、工作区根 `workspace_root`、路径不变量含符号链接逃逸拒绝、git worktree 创建/复用/回收、四个生命周期钩子）；`devsys worktree prepare\|remove\|list`；`run exec` 启动前校验工作区不变量并执行 `before_run`（致命）/`after_run`（仅记录） |
| M6.3 Run 生命周期与多轮续跑 | 已完成：`internal/prompt` 装配（首轮全量含策略正文渲染，续跑只发「上一轮以来变化 + 未完成项」，确定性 Hash 可重放）；轮次簿记在 run 事件流（`round` 记录），下一轮号从流推导；轮数上限 `limits.max_attempts` 超限即拒；§4.8 阶段推进（`building_prompt→launching_agent→streaming_turns→finishing`）与终态（succeeded/failed/timed_out/stalled/canceled）；`devsys run prompt\|complete\|fail\|cancel` + MCP `run_complete/run_fail/run_cancel` |
| M6.1 Harness Adapter 接口与 Shell Adapter | 已完成：`internal/harness`（方案 §9.2 九方法映射 + 八项能力声明 + Session 句柄承载 stream/stop/collect）；Shell 适配器 argv 直通、逐行 stdout/stderr、超长行按 rune 边界 64 KiB 分块、超时与取消终止整棵进程树（POSIX 进程组 / Windows `taskkill /T /F`）；`devsys run exec` 把输出实时镜像并写入 `.devsys/runs/<run-id>.jsonl`（追加写、周期 fsync、断尾修复留痕），结束后更新 run 证据（commands/logs/result.errors）并记事件 |

## 构建与验收

```sh
go build -o bin/devsys.exe ./cmd/devsys
bin/devsys.exe --version      # devsys 0.1.0-dev
bin/devsys.exe --help
bin/devsys.exe bogus; echo $? # 2（用法错误）

# 在 Git 仓库根目录初始化项目状态目录（幂等；重复执行不覆盖已有内容）
bin/devsys.exe init
bin/devsys.exe --json init    # 机器可读输出
bin/devsys.exe init; echo $?  # 非 Git 目录：3（前置条件错误）
```

`init` 创建 `.devsys/`（方案 §14.3，另含 `.devsys/.gitignore` 排除 `local/` 与 `.cache/`），
并在用户级注册表（`DEVSYS_CONFIG_DIR` 可覆盖）登记项目路径。

校验受管元数据文件（只读：不加锁、不写盘，未知 `schema_version` 时仍可运行，方案 §14.1）：

```sh
bin/devsys.exe config check
bin/devsys.exe --json config check    # 问题以结构化数组输出
bin/devsys.exe config check; echo $?  # 0 通过；4 解析/字段/版本问题；3 未初始化
```

有问题的文件会逐条给出 `文件:行号: 字段: 原因`（例如 `project.yaml:5: unknown_field: unknown key`）；
写命令（当前为 `init`）在任何改动前先校验既有受管文件，未知 `schema_version` 直接拒绝并提示迁移。

关键字检索（无数据库、无索引，直接扫描 `.devsys/` 文本，排除 `.cache/` 与 `local/`）：

```sh
bin/devsys.exe search <keyword>
bin/devsys.exe --json search <keyword>   # {ok,root,query,matches,total}
bin/devsys.exe search; echo $?           # 缺关键字：2（用法错误）
```

文本输出为 `<相对路径>:<行号>: <行内容>`；大小写不敏感子串匹配，单文件最多报 5 条命中行，
行内容截断 200 字节（UTF-8 安全）。实测 1000 文件（三分支目录 + 每 YAML 约 40B）并行扫描
约 0.6–0.7s（Windows/Defender 环境串行约 3.1s）；输出顺序与单线程遍历一致（路径+行号稳定排序）。

M1 完整剧本（Go 与 Git 必须在 PATH）：

```sh
# Windows PowerShell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m1.ps1
# Bash；Windows 请在 Git Bash 中执行（WSL 需自行安装 Go/Git）
bash scripts/smoke-m1.sh
```

脚本从仓库根构建 CLI 与 helper，在新临时 Git 项目中执行：初始化 → 创建 draft 任务 →
记录决策/发现、评论回复、Artifact 三版本与 Run 快照 → 乐观更新任务为 done → 检索及回读验证。
这里只验证 M1 原始持久化，不实现 M2 状态机或门禁。所有暂存状态文件必须为 UTF-8 且无 NUL。
默认清理临时目录；PowerShell `-Keep` / Bash `--keep` 保留项目并打印 `KEPT_PROJECT`。

`docs/examples/m1-devsys/` 是一次通过验收的 `.devsys/` 内容快照（13 个文本文件），不含本地锁、缓存和用户注册表。
可复制其内容到新 Git 仓库的 `.devsys/`，然后在该仓库运行 `devsys init` 补齐目录并登记注册表。
示例项目 ID 为 `demo-project`；用于真实项目时需一致替换各记录中的项目 ID 与示例内容。

M2 状态机与调度面（工作项命令成对使用 `get` 打印的版本哈希，防止旧视图覆盖新状态）：

```sh
bin/devsys.exe workitem get WLM-1                     # 含 version: <64-hex>
bin/devsys.exe workitem create --title <标题> --actor <操作者> --reason <原因>
bin/devsys.exe workitem transition --id WLM-1 --to ready --actor <a> --reason <r> --expect <hash>
bin/devsys.exe workitem claim --id WLM-1 --owner <身份> --reason <r> [--expect <hash>]
bin/devsys.exe workitem transition --id WLM-1 --to backlog --actor <a> --reason <r> --expect <hash>
# 非法转换：exit 4 并列出允许的下一步；旧快照：exit 4；缺 .devsys/：exit 3
```

对账与修复（`doctor` 只读，不建锁、不恢复；`recover` 先确定性恢复事务再释放过期/孤儿领取）：

```sh
bin/devsys.exe doctor                                  # 过期领取、孤儿领取、待恢复事务与提议
bin/devsys.exe recover --actor <a> --reason <r>        # 幂等；每次释放写 lease_recovered 事件
bin/devsys.exe repair --dry-run --actor <a> --reason <r>          # 打印提议表与 digest
bin/devsys.exe repair --apply --confirm <digest> --actor <a> --reason <r>
# digest 不符或证据漂移：exit 3，不写任何文件；apply 是唯一允许降低完成度的路径
```

M2 完整剧本（Go 与 Git 必须在 PATH）：

```sh
# Windows PowerShell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m2.ps1
# Bash；Windows 请在 Git Bash 中执行（系统 bash.exe 指向 WSL 时其中无 Go）
bash scripts/smoke-m2.sh
```

M2 剧本执行：初始化 → 创建任务 → `ready` → 领取（原子创建 Run + 租约）→ 模拟崩溃（租约过期、
Run 缺失）→ `doctor` 只读报告且不改锁文件 → `recover` 释放并留事件 → 构造 done 缺失产物 →
`repair --dry-run` 摘要 → `--apply` 回到 `in_progress` 并留 `repair_applied` 事件。默认清理临时目录；
PowerShell `-Keep` / Bash `--keep` 保留项目并打印 `KEPT_PROJECT`。

`internal/workitem/m2_crash_test.go` 用真实子进程中断覆盖提交点两侧：commit 前崩溃 → 事务丢弃、
领取不可见、可重新领取；commit 后崩溃 → 决定已定、完整重放、其余领取者被 fencing 拒绝。

工作流策略文件（M3.1，方案 §5.3；`.devsys/workflows/<id>.md` = YAML front matter + 提示词正文）：

```sh
bin/devsys.exe workflow check            # 只读校验全部策略；未知键为 warning（exit 0）
bin/devsys.exe --json workflow check     # {ok,root,policies[],warnings[]}
bin/devsys.exe workflow check; echo $?   # 0 通过（warning 不失败）；4 结构/类型/必填/引用错误；3 未初始化
```

非法 front matter 逐条报 `文件:行号:字段:原因`（如 `workflows/broken.md:2: id: required`）。
`workflow check` 不加锁、不恢复、不写盘。示例策略：`docs/examples/workflows/{quick-fix,feature-development,architecture-change}.md`。

正文模板（M3.2）：`{{name}}` 引用在加载期做语法校验（未闭合/非法名报 `body` 绝对行）；渲染时缺失变量按分类错误返回（`workflow.Render`），不静默变空。`$VAR`/`${VAR}` 环境引用只在运行时由 `ExpandEnv` 解析、不写入状态（方案 §5.3 密钥不落盘）。

阶段门禁与领取质量门（M3.3）：任务声明 `workflow` 实例后，`workitem transition` 在进入目标阶段前检查 `gates.stages[<目标状态>]`（`exempt_stages` 豁免），`workitem claim` 在领取前检查 `quality_gate.min_score`；拒绝时 exit 4 并逐条列出缺失项/改进项（定位到策略文件）。完成（进入 `done`）时相关 decision/finding 以引用（`decision://…`、`finding://…`）传播到父任务与直接兄弟的 `context_refs`，写在同一个完成事务里：不改对方状态、已存在则跳过（幂等）、无父链接不广播。

```sh
# Git Bash（系统 bash.exe 指向 WSL 时其中无 Go）
bash scripts/smoke-m3.sh          # 门禁拦截 → 补证据放行 → 传播 → 质量门拦截 → 改进后领取
```

就绪门与下一步（M3.4，方案 §7.4；只读，不建锁、不恢复）：

```sh
bin/devsys.exe next              # readiness: PASS|CONCERNS|FAIL + 风险信号 + next 建议
bin/devsys.exe --json next       # {ok,verdict,reasons[],risks[],fixes[],next{}}
```

风险信号：租约过期、孤儿领取、调度文件不可读、滞留审查（review/verification ≥24h）、未解决阻塞（blocked）、受管元数据问题、只读检查受限。存在待恢复事务时判定 FAIL 并给出恢复命令，且不输出业务事实；next 建议按固定顺序（恢复领取 → 审查 → ready 派发 → backlog 启动 → 里程碑回顾 → 报告完成），不含时间估算。

策略热载入与 last-known-good（M3.5，方案 §5.3）：每次消费都重新解析 `.devsys/workflows/<id>.md`；解析成功会把原文快照到 `.devsys/.cache/workflows/`（本地缓存、可删除、不提交）。当前文件非法或缺失时：`claim`（新任务派发）被阻塞（exit 4，提示首个定位问题）；`transition` 门禁回退到快照评估既有任务并打印 `warning: … using last-known-good`（gate 不满足时该提示仍然输出）；`next` 增加 `invalid_policy` 风险（含引用缺失策略的工作项）但仍可用；`workflow check` 始终展示当前文件的真实错误。

工作流实例与步骤推进（M3.6，方案 §5.3）：

```sh
bin/devsys.exe workflow start --id <wi> --policy <id> --actor <a> --reason <r> [--expect <hash>]
bin/devsys.exe workflow next --id <wi>                      # 只读：当前步候选与条件求值结果
bin/devsys.exe workflow step-complete --id <wi> [--to <step>] --actor <a> --reason <r> [--expect <hash>]
bin/devsys.exe workflow pause|resume|cancel --id <wi> --actor <a> --reason <r> [--expect <hash>]
```

`step-complete` 按声明序推进到首个满足条件的候选；`--to` 非声明目标（跳步）、指向当前步或条件不满足时 exit 4 并逐条列出允许的下一步。实例操作只写 `workflow` 字段与事件：不改工作项状态、不写调度态/租约；步骤声明 `status` 为进入要求（不匹配即拒绝）。条件白名单：`workitem.{id,status,type,priority,clarification_needed,approval_required,parent_id}`（`exists` 仅 `parent_id`）；字符串字面量可加成对单/双引号（`''` 表示空串），`in` 列表允许逗号后空格，引号不平衡按策略错误拒绝。`start` 属派发类，坏策略时拒绝，推进/`next` 允许 last-known-good + warning。工作项处于领取状态（lease 活跃）时，实例写操作必须携带当前 `--owner` 与 `--token`（与 `UpdateClaimed` 相同的 fencing 契约），否则拒绝。

审批（M3.7，方案 §4.9/§5.8）：

```sh
bin/devsys.exe approval list [--workitem <id>] [--status pending|approved|rejected]
bin/devsys.exe approval request --id <wi> --stage <target-status> [--scope stage_gate|action] --actor <a> --reason <r>
bin/devsys.exe approval approve --id <approval-id> --by <decider> [--comment <c>]
bin/devsys.exe approval reject  --id <approval-id> --by <decider> --reason <r>
```

`require_approval` 门禁要求存在「已批准、未消费、目标阶段与 `requested_status` 匹配」的审批；推进时审批在**同一事务内**被消费（写 `consumed_at` 并追加 `approval_consumed`），消费失败整个推进回滚。工作项离开请求时的状态会立即把该状态下未消费的审批标记为 `invalidated_at`（即使之后回到同一状态也需重新请求）；重复的未决请求会被拒绝。拒绝默认把工作项置为 `blocked`（`status_changed` 事件带审批引用与理由），策略 `on_reject: regress:<status>` 时回退到指定状态；`reject` 的决定与工作项处置在**同一事务**内提交，失败则整单回滚（审批保持 pending）。

MCP 接入（M4.1/M4.2，方案 §8.1/§8.6；stdio 优先，HTTP 后置）：

```sh
bin/devsys.exe mcp serve                      # 默认 profile：session+executor
bin/devsys.exe mcp serve --profile admin      # 显式启用 reviewer/admin 工具子集
```

传输层由官方 Go SDK（`github.com/modelcontextprotocol/go-sdk`，已 vendored）实现：stdout 只承载协议、诊断走 stderr；协议版本协商、`tools/list`/`tools/call`、输入 schema 校验（结构体推断，`additionalProperties:false`）与错误包装均由 SDK 负责。工具按 profile 分级**注册**（未暴露的工具不可按名调用）：`session`（查询与会话）、`executor`（创建/推进/领取/工作流/审批请求）、`reviewer`（审批决定）、`admin`（项目元数据与状态变更）；默认 `session,executor`，未知 profile 是用法错误。

工具面（M4.2/M4.3）：`health`、`project_{list,get,status,blueprint_get,create,update,state_update}`、`workitem_{list,get,next,create,update,transition,claim,release,start,block,complete,comment,add_dependency,remove_dependency}`、`workflow_{list,get,start,step_next,step_complete,pause,resume,cancel}`、`approval_{list,get,request,decide}`、`decision_{list,get,create,approve}`、`finding_{list,get,create,resolve}`、`event_{list,record}`、`artifact_{list,get,register,update,history}`、`run_{list,get,log,create,update,heartbeat}`、`context_{get,for_workitem,refresh,compact}`、`knowledge_status`（共 58 个）。所有写工具都要求 `expect`（先读后写；过期哈希一律拒绝；`event_record`/`workitem_comment` 为 append-only 例外），并且与 CLI 共用同一应用服务（`internal/app`）——门禁、质量门、审批消费、事务恢复在两侧是同一份实现。工具失败返回 `isError` 结果 + `{code,message,problems?,notice?}`（code 对齐退出码分类 usage/precondition/invalid/internal，problems 与 CLI 打印同形）。MCP 客户端（如 codex）以命令 `devsys.exe mcp serve`、工作目录指向项目根接入。

文件交换协议与退出码（M4.5，方案 §8.1/§12.5）：

退出码（脚本可分支，不解析消息）：

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 内部错误 |
| 2 | 用法错误 |
| 3 | 前置条件错误（非 Git 仓库、未 init、权限、摘要不匹配、对象不存在） |
| 4 | 受管状态不可信（解析/字段/schema_version 问题） |
| 10 / 11 | 预留给知识层状态（10 过期 / 11 缺失，方案 §12.5）。`knowledge status` 已可用（当前为降级报告，恒 exit 0）；10/11 状态码随 M5.3 引入 |

机器可读输出：`--json` 输出单个文档（成功 `{"ok":true,...}`；失败写 stderr 的 `{"ok":false,"error":{"code","kind","message","problems"}}`，`problems` 与人类输出同形 `文件:行号:字段:原因`）；`--jsonl` 输出**一行一条记录**（列表命令：`workitem|decision|finding|event|artifact|run|approval list`），记录字段与 `--json` 信封内的同名。两者互斥（同时给出是用法错误）。

文件交换：`.devsys/**` 是事实来源（YAML/JSON，稳定键序、原子替换、乐观并发版本哈希），只读脚本可以直接读取；**运行时写入必须经过 CLI/MCP 应用服务**（版本守卫、门禁、事务恢复都在那里），不要直接编辑受管文件（测试夹具与人工修复除外——修复请用 `devsys repair`）；未来若提供导入命令，同样必须走该服务。示例：`scripts/exchange-demo.sh`（原始文件读取、`--jsonl`/`--json` 解析、退出码分支，全部为可运行断言）。

运行一次尝试（M6.1，方案 §4.8/§9.2）：

```sh
bin/devsys.exe run exec --id <run-id> --actor <a> --reason <r> [--timeout 30s] -- <command...>
bin/devsys.exe --json run exec --id <run-id> --actor <a> --reason <r> -- <command...>   # {run_id,exit_code,timed_out,canceled,duration_ms,lines,log}
```

`run exec` 通过 Harness Adapter 接口（`internal/harness`）启动命令：argv 直通、不做 shell 解释（要 shell 特性就自己传 `sh -c` / `cmd /c`），stdout/stderr 逐行实时镜像（`--json` 时命令输出走 stderr，stdout 只承载信封）；输出同时追加到 `.devsys/runs/<run-id>.jsonl`（`start` / `output` / `exit` 记录，控制记录与每 128 行刷盘，崩溃留下的断尾在下次运行时截断并写入 `repair` 记录）。命令结束后把 `commands`、`logs` 引用与非零退出的说明写回 run，并记 `run_exec_started`/`run_exec_finished` 事件。超时或 Ctrl-C 会终止整棵进程树（POSIX 进程组 / Windows `taskkill /T /F`），不会留下孙进程。

**退出码语义**：`run exec` 的退出码描述 devsys 操作本身（0 已执行并落盘 / 2 用法 / 3 前置条件 / 4 受管状态不可信 / 1 内部）；被跑命令自己的退出码是**证据**，写在 JSONL 的 `exit` 记录与 run 的 `result.errors` 里，并由 `--json` 输出——两者不混用（否则命令退出 3 会被误读成 devsys 前置条件错误）。

工作区与生命周期钩子（M6.2，方案 §4.8）：

```sh
bin/devsys.exe worktree prepare --workitem <id> --actor <a> --reason <r> [--run <run-id>] [--branch <b>]
bin/devsys.exe worktree remove  --workitem <id> | --path <p> --actor <a> --reason <r> [--force]
bin/devsys.exe worktree list    [--workitem <id>]
```

工作区默认落在 `<项目根>/.devsys/workspaces/<key>`（`.devsys/config.yaml` 的可选键 `workspace_root` 可改到绝对或相对路径；`.devsys/.gitignore` 与仓库 `.gitignore` 已排除该目录）。`key` 由工作项标识净化而来（只允许 `[A-Za-z0-9._-]`），一旦净化改变了标识就追加原始标识的 sha256 前 16 位，避免 `a/b` 与 `a_b` 撞名；`..`、全点、空标识强制带哈希后缀。工作区是 git worktree（分支默认 `devsys/<key>`），**存在即复用且不再跑 `after_create`**，删除只由显式 `worktree remove` 触发（分支保留——它可能装着这次运行的提交）。路径不变量在创建、删除与 `run exec` 启动前都校验：必须位于工作区根内、拒绝 `..` 逃逸与符号链接逃逸，越界即 exit 3 且不启动任何进程。四个钩子按方案 §4.8 的语义：`after_create` 致命（失败即回收 worktree/目录/本次创建的分支，不留半成品）、`before_run` 致命（中止本次尝试，不 spawn）、`after_run` 与 `before_remove` 仅记录。

多轮会话与运行生命周期（M6.3，方案 §4.8）：

```sh
bin/devsys.exe run exec --id <run-id> --actor <a> --reason <r> [--round N] [--timeout 30s] -- <command...>
bin/devsys.exe run prompt --id <run-id> [--round N] [--write]     # 只读重放某一轮的提示词（--write 落盘）
bin/devsys.exe run complete|fail|cancel --id <run-id> [--expect <hash>] --actor <a> --reason <r> [--note <text>]
```

一次 Run = 一次尝试，可以包含多轮（同一会话继续推进）。**首轮**给完整任务简报：任务身份与描述、任务规格（`.devsys/specs/<id>.md` 若存在）、策略正文（`{{workitem.title}}`、`{{project.name}}`、`{{step}}` 等变量严格渲染，缺失即报错）、就绪判定、上下文引用（决策/发现/产物/评论的指针，正文按需再读）与汇报协议；**续跑轮**只给「上一轮以来的变化 + 未完成项 + 继续指令」，不再重发简报。装配是纯函数：同一输入必得同一文本与 sha256，`run prompt --round N` 可离线重放并与事件流里记录的 `prompt_hash` 对照。轮次上限取策略 `limits.max_attempts`（方案 §5.3 的「轮数上限」，M3.1 落在该键）；超限 exit 3 且不写 start 记录。提示词正文写到 `.devsys/local/runs/<run-id>/round-N.md`（不提交、可重建），子进程环境得到 `DEVSYS_RUN_ID`、`DEVSYS_ROUND`、`DEVSYS_PROMPT_FILE`、`DEVSYS_WORKITEM`、`DEVSYS_WORKSPACE`、`DEVSYS_BRANCH`、`DEVSYS_PROJECT_ROOT`。

阶段按 §4.8 推进并逐段落盘（run 记录的 `phase` + 事件流的 `phase` 记录）：`building_prompt → launching_agent → streaming_turns → finishing`。**干净的轮次不结束尝试**：退出码 0 只在流里记 `status: succeeded` 与 `continuation_due_at`（缺省 30s，供 M6.4 的 tick 判定是否需要再跑一轮），run 保持 `running`；非零退出、超时、取消分别把 run 推到 `failed`/`timed_out`/`canceled`，`run complete|fail|cancel` 由人或调度显式收尾（写终态 + `run_finished` 事件，重复收尾被拒）。

M4 完整剧本（Go、Git 与 Python 必须在 PATH）：

```sh
# Windows PowerShell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m4.ps1
# Bash；Windows 请在 Git Bash 中执行（WSL 需自行安装 Go/Git）
bash scripts/smoke-m4.sh
```

脚本构建 CLI 与 `scripts/m4helper`（用官方 SDK 作客户端，走真实 stdio 传输），在新临时 Git 项目中执行：MCP 面（profile 过滤、health、decision 创建/读取往返、`agent_session_start`、错误分类 `precondition` 与未知参数拒绝）、CLI/MCP 读写一致（MCP 创建的记录由 CLI 读出同一版本哈希）、退出码分支（0/2/3）、`--jsonl` 一行一条、`wire` 三次幂等且保留手写内容与 repowiki 块、`knowledge status` 降级报告。默认清理临时目录；PowerShell `-Keep` / Bash `--keep` 保留项目并打印 `KEPT_PROJECT`。

测试与静态检查（`vendor/` 已提交，离线可跑）：

```sh
gofmt -l cmd internal   # 无输出
go vet ./...
go test ./...           # vendor 模式下不需要网络
```

带版本信息构建（发布用）：

```sh
go build -ldflags "-X workloom/internal/version.Version=0.1.0 -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" -o bin/devsys.exe ./cmd/devsys
```

## 目录结构

```
cmd/devsys/          命令入口（进程退出码）
internal/cli/        命令分发、全局开关、输出与退出码
internal/storage/    存储基元：原子写、JSONL、稳定序列化、项目锁、事务日志与恢复（方案 §15.2）
internal/config/     严格配置解析、错误定位与 schema_version 校验（方案 §14.1）
internal/domain/     Project/WorkItem/Run/Decision/Finding/Event/Artifact/Approval 与版本化序列化
internal/project/    init 与项目内布局（后续：领域 / 访问 / 执行）
internal/registry/   用户级项目路径注册表（方案 §14.4）
internal/workflow/   策略文件解析与严格校验 .devsys/workflows/<id>.md（方案 §5.3；M3.1）
internal/mcp/        MCP stdio 服务：JSON-RPC 生命周期、工具注册与 profile 分级（方案 §8.1/§8.6；M4.1）
internal/version/    构建标识（可用 -ldflags 覆盖）
vendor/              依赖副本（gopkg.in/yaml.v3），保证干净机器离线构建
docs/原始文档/       方案与实施计划（源文档，不再拆分）
docs/开发记录.md     实施中的问题、偏差、决策与遗留事项（本地记录）
bin/                 构建产物（不提交）
```

后续面向使用者的手册与迁移说明放在 `docs/` 下（不进 `docs/原始文档/`）。

## 依赖策略

- 唯一外部依赖是 `gopkg.in/yaml.v3`，已 `go mod vendor` 并提交 `vendor/`；干净机器无需网络：
  `GOFLAGS=-mod=vendor GOPROXY=off go build ./...`。
- 新增或升级依赖：`GOPROXY=https://goproxy.cn go get <module>`（本机直连 `proxy.golang.org` 实测超时；
  也可 `HTTPS_PROXY=http://127.0.0.1:10808` 走本地代理），随后 `go mod tidy && go mod vendor` 一起提交。
- 锁与事务恢复材料只依赖标准库系统调用（Windows 动态调用 LockFileEx，POSIX 用 flock），不引入锁库。

## 约定

- 核心用 Go；M7 的工作区视图用 TypeScript/React 实现，只读渲染、不碰进程。
- 一切项目状态写成可 diff、可合并的文本（YAML / Markdown / JSONL）。
- 不使用数据库；任何缓存都是可删除、可重建的普通文件。
- 行尾统一 LF（见 `.gitattributes`），保证状态文件跨机器合并不产生幽灵 diff。
- 时间估算不进入状态输出（见方案 §7.4）。

### 领域文件契约（M1.1）

- 受管文档 `schema_version: 1`；领域 YAML/JSON 解码拒绝未知版本、未知字段和第二份文档。
- Project 包含方案 §5.1 全字段；`id`、`name`、`schema_version` 必填，旧最小元数据仍可读取。时间使用 RFC3339，写入者提供 UTC。
- `state/current.yaml` 严格接受 summary/risks/blockers/next_focus；`state/milestones.yaml` 接受 milestones（id/name/status 对象列表），不再放行未知键。
- `config.yaml` 仍只接受 schema_version。业务配置在相应里程碑扩展。
- `null` 与空集合保留差异；Run verification.advanced 的 null/false/true 区分未验证、失败与成功。
- Artifact `previous_id` 表示前一版本记录引用；Event 使用 type/subject/time/actor/related/content/reply_to，回复关联验证在 M1.3 实现。
