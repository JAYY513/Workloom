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
