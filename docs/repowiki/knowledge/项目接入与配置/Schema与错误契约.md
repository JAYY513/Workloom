---
status: stable
type: module
dimension: schema_contract
triggers:
  - schema_version 怎么校验
  - 未知键怎么处理
  - 字段类型错误怎么报
  - Problems 怎么用
  - 退出码 4 何时返回
  - Project 模型有哪些字段
  - 嵌套结构怎么校验
  - domain.SchemaVersion 是几
  - --expect 哈希怎么传
  - --confirm 摘要格式
  - 写命令必填参数
  - 退出码 3 何时返回
  - workflow check 退出码
  - approval kind
  - workflow kind
  - workspace_root 怎么配
  - dispatch_command 怎么配
  - DEVSYS_PROJECT_ROOT
  - HookEnv
  - 执行命令族
  - M6 退出码
  - run verify exit code
  - run complete exit code
description: devsys 受管 YAML 文件的严格 schema：每文件白名单 + schema_version 闸门 + 行号定位 + Problems 错误结构（含 SeverityWarning） + 退出码 4 的语义边界 + M1 完整 Project 模型 + 嵌套字段校验；M2 新增退出码 3/4 在 workitem 与 repair 中的扩展语义、--expect 64-hex sha256 + fail-closed、--confirm 摘要格式、写命令 --actor/--reason 必填；M3 新增 workflow/approval/next 的 kind 与 code 语义、workitem.TransitionRequest.Guard 签名、policy issue 与 LKG 报错；M4 收口到 *app.Error.Class() 四类（映射 CLI 退出码 + MCP tool error code）、--jsonl 列表流、知识层 10/11 退出码预留；M5 新增 KnowledgePages / KnowledgeGenerator 配置键、kindStrings（接受 null 与空列表）+ kindMilestones 双路径、warning severity、KindKnowledge 错误类、knowledge_pages / knowledge_generator 字段解析；M6 增 workspace_root / dispatch_command 配置键、HookEnv 注入、run exec / run complete 在执行层的扩展退出码语义；M7.1 增 workspace view 在视图域的退出码语义（沿用 0/2/3/1，10/11 仍专属 knowledge status）、workspace view 的 --limit 与子命令缺失 → 2、缺 .devsys/ → 3、view.Build 失败 → 1、advisory_unlocked 是事实标签不是失败码。
generated: true
source_commit: fa2a87d
generator: repowiki-gen
---

# Schema 与错误契约

本卡描述 `workloom/internal/config` 落地的**严格 schema 契约**以及 M1 起 `workloom/internal/domain` 引入的统一记录契约：哪些字段是合法的、字段必须是什么类型、`schema_version` 不被支持时会发生什么、错误如何结构化传给调用方。**这是写命令与只读诊断命令共用的契约**，也是后续里程碑扩展白名单时的唯一参考。

## 契约的物理边界

| 名称 | 值 | 来源 |
|---|---|---|
| `config.SupportedSchemaVersion` | `1` | [internal/config/config.go:25](../../../internal/config/config.go#L25) |
| `domain.SchemaVersion` | `1` | [internal/domain/models.go:6](../../../internal/domain/models.go#L6) |
| 错误结构 | `Problem{File, Line, Field, Severity, Reason}`（M5 增 Severity）与 `Problems`；`SeverityWarning = "warning"` 常量（[internal/config/config.go:46-79](../../../internal/config/config.go#L46-L79)） | [internal/config/config.go:46-79](../../../internal/config/config.go#L46-L79) |
| 知识层配置键（M5） | `config.yaml` 新增 `knowledge_pages []string` 与 `knowledge_generator string`；`validate.go` 拆 `kindStrings`（接受 null / 空列表 / 字符串标量列表）与 `kindMilestones` 双路径 | [internal/config/config.go:104-114](../../../internal/config/config.go#L104-L114)、[internal/config/validate.go:64-92](../../../internal/config/validate.go#L64-L92) |
| 知识层错误类（M5） | `app.KindKnowledge = "knowledge"`；`Class()` 归一到 `KindInvalid`，CLI 退出码 4；MCP tool error code `invalid` | [internal/app/app.go:28-49](../../../internal/app/app.go#L28-L49)、[internal/app/knowledge.go](../../../internal/app/knowledge.go) |
| 记录契约 | `domain.{Project,Scope,Milestone,CurrentState,CurrentStateFile,MilestonesFile,WorkItem,Run,…}` | [internal/domain/models.go:8-221](../../../internal/domain/models.go#L8-L221) |
| Workflow 策略契约 | `internal/workflow.Policy` 与 `internal/workflow.Issue` | [internal/workflow/policy.go](../../../internal/workflow/policy.go)、[internal/workflow/parse.go](../../../internal/workflow/parse.go) |
| Approval 契约 | `domain.Approval` 与 `approval.ErrXxx` | [internal/domain/models.go](../../../internal/domain/models.go)、[internal/approval/approval.go:49-58](../../../internal/approval/approval.go#L49-L58) |
| Next 判定契约 | `next.Report` 与 `Verdict` | [internal/next/evaluate.go](../../../internal/next/evaluate.go) |

`config.SupportedSchemaVersion` 与 `domain.SchemaVersion` 必须**始终相等**：`domain.EncodeYAML` / `DecodeYAML` 在序列化末尾调用 `storage.CheckSchemaVersion(b, SchemaVersion)`（[internal/domain/serialize.go:26-28](../../../internal/domain/serialize.go#L26-L28)）作为最后一道闸门。

**Workflow 策略文件不设 `schema_version`**：策略自身只有 `version`（必填 ≥1）字段，是策略文件语义版本；详见下文「Workflow 策略文件契约」小节。

**当前没有任何其它文件受本契约约束**。其它受写命令操作的 `.devsys/` 子目录（`workitems/`、`specs/`、`runs/`、`approvals/`、…）由后续里程碑定义各自的领域契约；但它们的字段形状最终都汇聚到 `internal/domain` 的对应 struct。

## 白名单：每文件允许的字段

由 `fileSpec.fields` 静态定义（[internal/config/validate.go:41-57](../../../internal/config/validate.go#L41-L57)）。`schema_version` 始终是第一个、必填、必须是整数。

### `project.yaml`（M1 起扩展为完整 `Project` 模型）

| 字段 | 类型 | 必填？ | 备注 |
|---|---|---|---|
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `id` | `string` | 是 | 来自目录名的 slug，见 `project.Slug` |
| `name` | `string` | 是 | 仓库根目录名 |
| `description` | `string` | 否 | 项目一句话简介 |
| `status` | `string` | 否 | 项目状态字符串（init 写 `"active"`） |
| `current_phase` | `string` | 否 | 当前阶段标识 |
| `goals` | `string[]`（或 `null`） | 否 | 目标列表；M1 起通过 `kindStrings` 校验 |
| `constraints` | `string[]`（或 `null`） | 否 | 约束列表 |
| `tech_stack` | `string[]`（或 `null`） | 否 | 技术栈列表 |
| `scope` | mapping `{in, out}` | 否 | `kindScope` 嵌套结构，两键皆为 `string[]` |
| `current_state` | mapping | 否 | `kindCurrent` 嵌套结构，校验 `summary/risks/blockers/next_focus` |
| `milestones` | sequence | 否 | `kindMilestones` 嵌套结构，每元素映射校验 `id/name/status` |
| `blueprint_artifact_id` | `string` | 否 | 蓝图 Artifact 引用 |
| `created_at` | RFC3339（`!!str` 或 `!!timestamp`） | 否 | M1 起白名单同时包含 `updated_at` |
| `updated_at` | RFC3339（`!!str` 或 `!!timestamp`） | 否 | 由运行态改写 |

`init` 写出的 `project.yaml` 由 `domain.Project` struct 序列化（[internal/project/tree.go:60-66](../../../internal/project/tree.go#L60-L66)），键序 = struct 字段序；`config` 包的白名单（[internal/config/validate.go:41-57](../../../internal/config/validate.go#L41-L57)）与 struct 字段必须同步演进。

### `config.yaml`

| 字段 | 类型 | 必填？ | 备注 |
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `workspace_root` | `string` | 否 | **M6** 工作区根目录；绝对路径或相对项目根；缺失回退 `<project>/.devsys/workspaces`；M6 起 `app.WorkspacePrepare` / `app.RunExec` / `internal/workspace.Root` 共同使用 |
| `dispatch_command` | `string` | 否 | **M6** 调度 tick 默认每个 attempt 的 argv 模板；M6.4 默认 `"devsys run exec --id {run_id} --actor dispatch --reason tick"`；M6.7 起由 per-workitem `assigned_harness` 取代 |

### `state/current.yaml` 与 `state/milestones.yaml`
| `current_state` | mapping | 否 | `kindCurrent` 嵌套结构，校验 `summary/risks/blockers/next_focus` |
两个 state spec 在 M1 起被显式收紧（[internal/config/validate.go:63-73](../../../internal/config/validate.go#L63-L73)）：

| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `workspace_root` | `string` | 否 | **M6** 工作区根目录；绝对路径或相对项目根；缺失回退 `<project>/.devsys/workspaces`；M6 起 `app.WorkspacePrepare` / `app.RunExec` / `internal/workspace.Root` 共同使用 |
| `dispatch_command` | `string` | 否 | **M6** 调度 tick 默认每个 attempt 的 argv 模板；M6.4 默认 `"devsys run exec --id {run_id} --actor dispatch --reason tick"`；M6.7 起由 per-workitem `assigned_harness` 取代 |

> **结果**：M0.4 时 state 文件仅校验 schema_version；M1 起两者都被收紧到 `currentSpec` / `milestonesSpec`，并通过 `internal/domain.CurrentStateFile` / `MilestonesFile` 提供反序列化视图（[internal/project/tree.go:73-84](../../../internal/project/tree.go#L73-L84)）。

## 字段类型：`fieldKind`

七档位（[internal/config/validate.go:17-25](../../../internal/config/validate.go#L17-L25)）：

| Kind | YAML 形态 | 判定 |
|---|---|---|
| `kindInt` | `!!int` 标量 | `yaml.Node.Decode(&int)` 成功 |
| `kindString` | `!!str` 标量；非空（当 `required`） | `Kind==ScalarNode && Tag=="!!str"` |
| `kindTimestamp` | `!!str` 或 `!!timestamp`，必须能 `time.Parse(time.RFC3339, ...)` | 见 [internal/config/validate.go:182-191](../../../internal/config/validate.go#L182-L191) |
| `kindStrings` | `!!null`（保留"未记录"态）或 `!!seq` 元素皆为 `!!str` | 见 [internal/config/validate.go:160-163](../../../internal/config/validate.go#L160-L163) |
| `kindScope` | `!!map`，键仅 `in` / `out`，值皆 `kindStrings` | 见 [internal/config/validate.go:164-167](../../../internal/config/validate.go#L164-L167) + [internal/config/nested.go:23-24](../../../internal/config/nested.go#L23-L24) |
| `kindCurrent` | `!!map`，子键集合 = `summary/risks/blockers/next_focus` | 见 [internal/config/nested.go:25-26](../../../internal/config/nested.go#L25-L26) |
| `kindMilestones` | `!!seq`，每元素 mapping 必填 `id/name/status`（皆 `kindString`） | 见 [internal/config/nested.go:18-22](../../../internal/config/nested.go#L18-L22) |

任何不匹配的键值在 `checkField` 里返回 `Problem{Line: val.Line, Field: name, Reason: "expected <kind>, got <nodeKind>"}`（[internal/config/validate.go:157-193](../../../internal/config/validate.go#L157-L193)）；嵌套失败则通过 `checkNested` / `checkMapping` 沿 `field[index]` / `field.child` 形式把路径展开（[internal/config/nested.go:8-68](../../../internal/config/nested.go#L8-L68)）。

## `schema_version` 闸门

`validate` 的第一步是检查 `schema_version`（[internal/config/validate.go:98-114](../../../internal/config/validate.go#L98-L114)）。它**早于**字段白名单检查，因此一份未知版本的受管文件**只**会产生一条 `unsupported version N` 问题，不会被后续"未知键 / 类型错误"的噪声淹没。

触发的拒绝文本（[internal/config/validate.go:110-113](../../../internal/config/validate.go#L110-L113)）：

```text
unsupported version N (this build supports M); refusing to write, migration must be explicit (实施计划 M9.2)
```

写命令（`init`）因为拿到这条 `Problems` 会返回 `CodeInvalid`；只读诊断（`config check`）同样返回 `CodeInvalid` 但仍继续（[internal/cli/cli.go:314-317](../../../internal/cli/cli.go#L314-L317)）。

`domain.EncodeYAML` / `DecodeYAML` 在写出 / 读入时再次调用 `storage.CheckSchemaVersion`（[internal/domain/serialize.go:26-28、73-78](../../../internal/domain/serialize.go#L26-L28)），保证领域记录的 schema_version 与配置层**始终一致**。

## 行号定位

每个 `Problem` 都带 `Line`（1-based，0 表示"整文件层级的问题"）。`yaml.Node.Line` 由 `gopkg.in/yaml.v3` 在解码时填入。覆盖以下情形：

- 顶层不是 mapping：根节点的 `Line`。
- `schema_version` 缺失：根节点 `Line`，`Field="schema_version"`。
- 重复键：第二次出现的 `Line`。
- 类型错误：值节点的 `Line`。
- 空文件 / 多文档 / 解析错误：整文件（`Line=0`）。
- 嵌套失败（如 `milestones[0].status` 缺失）：元素 / 子键节点的 `Line`，`Field` 形如 `milestones[0].status`（[internal/config/nested.go:13-22](../../../internal/config/nested.go#L13-L22)）。
- Workflow 策略文件：[internal/workflow/parse.go](../../../internal/workflow/parse.go) 用 `yaml.Node.Line` 同样填入；body 模板错误行号 = `bodyLine + rerr.Line - 1`（`splitFrontMatter` 输出 1-based 起始行）。

`Problem.String()` 渲染为 `file[:line][: field]: reason`（[internal/config/config.go:54-67](../../../internal/config/config.go#L54-L67)）：

```text
project.yaml:3: id: expected string, got !!int
project.yaml:4: updated_at: unknown key
project.yaml:2: name: required
project.yaml:5: milestones[0].status: required
workflows/quick-fix.md:9: body: unclosed {{
workflows/feature-development.md:17: steps[3].status: unknown status "inprog" (valid: draft, backlog, ready, in_progress, review, verification, done, blocked, cancelled)
```

`workflow.Issue.String()` 复用同一渲染（[internal/workflow/policy.go:37-50](../../../internal/workflow/policy.go#L37-L50)）。

## Workflow 策略文件契约（M3）

`internal/workflow.Parse(rel, data)` 解析 `.devsys/workflows/<id>.md`，前导 YAML front matter + 提示词正文。

### 顶层键白名单（[internal/workflow/parse.go:25-31](../../../internal/workflow/parse.go#L25-L31)）

`id` / `name` / `version` / `input` / `steps` / `transitions` / `approval_points` / `completion_rules` / `gates` / `hooks` / `concurrency` / `limits` / `quality_gate` / `on_reject`。

未知键 → warning（仍加载）；结构/类型/必填/交叉引用错误 → error（`Policy = nil`，CLI exit 4）。

### 关键字段

| 字段 | 类型 | 必填？ | 备注 |
|---|---|---|---|
| `id` | `string` | 是 | 必须匹配文件名（`^[a-z][a-z0-9-]*$`） |
| `name` | `string` | 是 | 显示名 |
| `version` | `int ≥ 1` | 是 | 策略自身版本号（**不是** schema_version） |
| `input.required` / `input.optional` | `string[]` | 否 | token 列表（小写） |
| `steps[].id` | `string` | 是 | `^[a-z][a-z0-9_-]*$`；不可重复 |
| `steps[].type` | `string` | 是 | 同上形状；词表开放（仅校验形状，避免发明规范未定义的枚举） |
| `steps[].required` | `bool` | 否 | 默认 `true` |
| `steps[].status` | `string` | 否 | 必须等于 `domain.AllStatuses()` 之一；进入该步时工作项状态需匹配（不隐式改状态） |
| `transitions[].from` / `to` | `string` | 是 | `from` 必须是声明的 step id；`to` 是 step id 或 `"done"` |
| `transitions[].when` | `string` | 否 | 声明式条件；白名单 7 字段 |
| `approval_points` | `string[]` | 否 | 审批触发点 token 列表 |
| `completion_rules` | `string[]` | 否 | 完成规则 token 列表 |
| `gates.exempt_stages` | `string[]` | 否 | 不做门禁的工作项状态；值 = 工作项九状态 |
| `gates.stages[<status>]` | mapping | 否 | `require_artifacts[]` + `require_min_artifacts:int` + `require_comment:bool` + `require_approval:bool` |
| `hooks.<name>` | mapping | 否 | `<name> ∈ {after_create, before_run, after_run, before_remove}`；含 `command: string`（必填）+ `timeout_seconds: int ≥ 1` |
| `concurrency.global` | `int ≥ 1` | 否 | 全局并发上限 |
| `concurrency.per_status.<status>` | `int ≥ 1` | 否 | 每状态并发上限；status 必须在九状态内 |
| `limits.max_attempts` | `int ≥ 1` | 否 | 重试上限 |
| `limits.run_timeout_seconds` / `stall_threshold_seconds` / `backoff_max_seconds` | `int ≥ 1` | 否 | 各类超时 |
| `quality_gate.min_score` | `0 ≤ int ≤ 100` | 否 | 领取质量门阈值；未声明则永不拦截 |
| `on_reject` | `"block"` 或 `"regress:<status>"` | 否 | 默认 `"block"`；`regress:<status>` 必须以九状态结尾 |

### 条件白名单（[internal/workflow/condition.go:57-65](../../../internal/workflow/condition.go#L57-L65)）

```text
workitem.id                   string
workitem.status               string
workitem.type                 string
workitem.priority             int
workitem.clarification_needed bool
workitem.approval_required    bool
workitem.parent_id            string (optional)
```

操作符 `==` / `!=` / `<` / `>` / `in` / `exists`；类型规则：`<` `>` 仅 int；`in` 仅 string；`exists` 仅 `workitem.parent_id`。复合表达式（`&&` / `||` / `()`）在加载期拒绝。

### 模板与正文

`body` 是 front matter 下方整段文本（CRLF 闭合行后的空行**保留**为正文内容，verbatim 语义）。模板语法 `{{name}}`（点分标识符、括号内允许空白）；孤立 `}}` / 未闭合 `{{` / 空名 / 非法名在加载期报 `body: <reason>`（[internal/workflow/template.go](../../../internal/workflow/template.go)）。

`$VAR` / `${VAR}` **不在**加载期解析；`ExpandEnv` 在使用时由调用方（M6 hook 执行）解析——「密钥不落盘」的可实现边界（[docs/开发记录.md:113-115](../../../docs/开发记录.md#L113-L115)）。

### LKG 报错（[internal/workflow/cache.go](../../../internal/workflow/cache.go)）

`workflow.Resolve(ctx, root, id)` 失败时：

- 当前文件解析失败 + 无 LKG → `workflow policy %q is invalid: %s (no last-known-good retained)`
- 当前文件缺失 + 无 LKG → `workflow policy %q not found under .devsys/workflows/`
- 文件读取错 → `workflow policy %q: read: %w`

`workflow check` 走 `workflow.Load`，逐文件列 issues；error-severity → `config.Problem` → `errInvalid(problems)` → exit 4。

## 退出码契约

`CodeInvalid = 4`（[internal/cli/cli.go:33-44](../../../internal/cli/cli.go#L33-L44)）的语义在 `usage` 文本里写明（[internal/cli/cli.go:66-71](../../../internal/cli/cli.go#L66-L71)）：

```text
0  success
1  internal error
2  usage error
3  precondition error (not a git repository, wrong directory, permissions, digest mismatch)
4  invalid managed state (parse, field or schema_version problems)
```

### `CodeInvalid = 4`：受管状态存在但不可信

它**只**由 `errInvalid(config.Problems)` 触发（[internal/cli/cli.go:103-114](../../../internal/cli/cli.go#L103-L114)）。写命令在拿到 `Problems` 时返回 `CodeInvalid` 并**不**修改磁盘。

M2 起 `CodeInvalid = 4` 还覆盖 workitem / reconcile 写路径下的领域错误，由 `workitemError` 把非 `storage.ErrNotInitialized` / `workitem.ErrNotFound` 的领域错误升为带 `kind="workitem"` 的 `codedError{Code: CodeInvalid}`（[internal/cli/cli.go:477-482](../../../internal/cli/cli.go#L477-L482)）。

M3 起又扩展：

- `kind="approval"`：`approval.ErrInvalidInput` 走 `errUsage`；`storage.ErrNotInitialized` 走 `errPrecondition`；其余 `ErrNotFound` / `ErrNotPending` / `ErrNotApproved` / `ErrAlreadyConsumed` / `ErrInvalidated` / `ErrMismatch` 走 `codedError{kind: "approval", code: CodeInvalid}`（[internal/cli/cli.go:1218-1232](../../../internal/cli/cli.go#L1218-L1232)）。
- `kind="workflow"`：workflow 实例操作的拒绝（含 `WorkflowStepError` + `Resolve` 失败 + `Render` 错误）走 `codedError{kind: "workflow", code: CodeInvalid}`（[internal/cli/cli.go:522-543](../../../internal/cli/cli.go#L522-L543)）。
- `workflow check` 把策略文件的结构/类型/必填/交叉引用错误升为 `CodeInvalid`（同 `config check` 路径）。

### `CodePrecondition = 3`：前提不满足

原本只承担"环境侧错误"（非 git 仓库、git 不在 PATH、CWD 不是仓库根）。M2 在同一退出码下聚合两类新用法（[internal/cli/cli.go:70](../../../internal/cli/cli.go#L70)）：

| 触发点 | 来源 | 错误提示 |
|---|---|---|
| 非 git 仓库 / git 不在 PATH / CWD 不在仓库根 | `project.PreconditionError` → `codedError{Code: CodePrecondition}` | `not a git repository` 等 |
| `.devsys/` 不存在（`search` / `workitem get` / `workitem create` 等） | `storage.ErrNotInitialized` / `workitem.ErrNotFound` → `workitemError` → `errPrecondition` | `project not initialized; run devsys init` 等 |
| workitem `--expect` 哈希与现状不符 | `expectedSnapshot` 返回 `version mismatch` → `workitemError` → `errPrecondition` | `version mismatch: work item changed since your read; rerun workitem get` |
| `repair --apply` 收到的 `--confirm` 与重跑摘要不符 | `reconcile.ErrDigestMismatch` → `errPrecondition` | `confirmation digest does not match current state; run `devsys repair --dry-run` again` |
| workflow / approval 实例操作 `--expect` 不匹配 | `expectedSnapshot` 同上 | 同上 |
| approval 操作时 `.devsys/` 不存在 | `storage.ErrNotInitialized` → `approvalError` | `project not initialized; run devsys init` 等 |

第 3–5 行即方案 §15.4 "推断→确认→重写" / "快照必填" 闭环在退出码层面的体现：调用方必须重新读证据 / 重新干跑，才能继续动盘。脚本可以**只信 `code == 3` 重新发起一次 `get` / `dry-run`**。

### `CodeUsage = 2`：参数错误

M3 新增触发：

- `approval list --status` 取值不在 `{pending, approved, rejected}` → exit 2。
- `workflow` 子命令缺失或不在 `{check, start, next, step-complete, pause, resume, cancel}` → exit 2。
- `approval` 子命令缺失或不在 `{list, request, approve, reject}` → exit 2。

M7.1–M7.4 在 `CodeUsage = 2` 与 `CodePrecondition = 3` / `CodeInternal = 1` 下扩展 `devsys workspace` 子命令面（视图域只读，与 §4.8 `worktree` 子命令独立）：

- `devsys workspace` 子命令缺失或不在 `{view, build, serve}` → exit 2：`errUsage("`devsys workspace` needs a subcommand: view | build | serve")` / `errUsage("unknown `devsys workspace` subcommand %q", rest[0])`（[internal/cli/workspace.go:24-38](../../../internal/cli/workspace.go#L24-L38)）。
- `devsys workspace view --limit N` 且 `N <= 0` → exit 2：`errUsage("`--limit` must be positive")`（[internal/cli/workspace.go:121-123](../../../internal/cli/workspace.go#L121-L123)）。
- `devsys workspace build` 不带 `--static`（M7.2 唯一支持的站点形态）→ exit 2：`errUsage("`devsys workspace build` needs --static (the only site form in M7.2)")`（[internal/cli/workspace.go:53-54](../../../internal/cli/workspace.go#L53-L54)）；参数解析失败 / 含未知位置参数 → exit 2：`errUsage("`devsys workspace build --static [--out DIR] [--limit N]`")`（[internal/cli/workspace.go:50-51](../../../internal/cli/workspace.go#L50-L51)）；非法 `--out` → exit 2：`errUsage("workspace build: %v", err)`（[internal/cli/workspace.go:62-64](../../../internal/cli/workspace.go#L62-L64)）。
- `devsys workspace serve --host <non-loopback>` 且未带 `--allow-remote` → exit 2：`errUsage("refusing non-loopback bind %q without --allow-remote ...")`（[internal/cli/workspace_serve.go:159-161](../../../internal/cli/workspace_serve.go#L159-L161)）；`--port` 越界或带 `--json`/`--quiet` → exit 2（[internal/cli/workspace_serve.go:153-157](../../../internal/cli/workspace_serve.go#L153-L157)）。

`workspace view` / `build` / `serve` 不引入新退出码：成功（含 `trust.state = advisory_unlocked` / `pending_transaction`）→ 0；缺 `.devsys/`（`storage.ErrNotInitialized`）→ 3（precondition）；`view.Build` 其它失败 → 1（internal）；`workspace build` 写盘失败 → 1（`sitestatic.Build` 错误，[internal/cli/workspace.go:70-73](../../../internal/cli/workspace.go#L70-L73)）；`workspace serve` 端口占用 → 1（`net.Listen` 失败，[internal/cli/workspace_serve.go:172-174](../../../internal/cli/workspace_serve.go#L172-L174)）。**build/serve 沿用同一 0/1/2/3/4**，10/11 仍专属 `devsys knowledge status`（[internal/cli/cli.go:52-55](../../../internal/cli/cli.go#L52-L55)）。

## `--expect`：64-hex sha256 + fail-closed

`workitem transition` / `claim` / `workflow start` / `step-complete` / `pause` / `resume` / `cancel` 的 `--expect` 是调用方持有的"工作项快照版本哈希"，由 `workitem get` 在 `--json` 模式下的 `version` 字段给出（[internal/cli/cli.go:493-506](../../../internal/cli/cli.go#L493-L506)）。

格式与解析：

- 编码：小写 64 字符十六进制串，等价于 `fmt.Sprintf("%x", storage.HashBytes(raw))`，即 sha256 的字节级表示。
- 解析：`strings.TrimSpace` 后用 `hex.DecodeString` 解码；解码结果长度必须等于 `crypto/sha256.Size`（32 字节）；解码失败、长度错误、哈希不匹配**全部**视为哈希不匹配，统一返回 `version mismatch` 错误。
- fail-closed：`expectedSnapshot` 在哈希不匹配时**不**返回 `raw` 字节；底层 `Transition` / `Claim` / `WorkflowStart` / `WorkflowStepComplete` / `WorkflowSignal` 拿不到 `Expected` 入参，写路径被彻底拦截。
- 退出码：不匹配 → `CodePrecondition = 3`，错误提示 `version mismatch: work item changed since your read; rerun workitem get`。

调用契约：

```sh
VERSION=$(bin/devsys.exe --json workitem get WLM-0001 | jq -r .version)
bin/devsys.exe workitem transition --id WLM-0001 --to in_progress \
    --actor alice --reason "M3 kickoff" --expect "$VERSION"
```

## `--confirm`：确定性 digest + 摘要回放

`devsys repair --apply --confirm <digest>` 的 `--confirm` 是 `repair --dry-run` 在 `--json` 模式下的 `plan.digest` 字段（[internal/cli/cli.go:659-664](../../../internal/cli/cli.go#L659-L664)）。

格式与判定（[internal/cli/cli.go:614-635](../../../internal/cli/cli.go#L614-L635) + [internal/reconcile/reconcile.go:319-335](../../../internal/reconcile/reconcile.go#L319-L335)）：

- 编码：`reconcile.ComputeDigest(plan.Proposals)` 输出小写十六进制摘要；墙钟时间**不**参与计算，相同证据输入产生相同摘要。
- 判定：`--apply` 内部**先**重跑 `reconcile.RepairDryRun` 取得当前 `Digest`，**再**用调用方的 `--confirm` 覆盖 `plan.Digest`，**再**调 `reconcile.RepairApply`；摘要不一致由 `reconcile.ErrDigestMismatch` 上浮为 `CodePrecondition = 3`，并提示 `run `devsys repair --dry-run` again`。
- 空摘要：`--apply` 在缺 `--confirm` 时直接返回 `CodeUsage = 2`，**不**进入 reconcile（[internal/cli/cli.go:614-617](../../../internal/cli/cli.go#L614-L617)）。

调用契约：

```sh
DIGEST=$(bin/devsys.exe --json repair --dry-run --actor alice --reason "M3 audit" | jq -r .plan.digest)
bin/devsys.exe repair --apply --confirm "$DIGEST" --actor alice --reason "M3 audit"
```

## 写命令必填 `--actor` 与 `--reason`

所有动盘的命令在 CLI 层强制记录操作者与原因（审计追溯），缺失即返回 `CodeUsage = 2`：

| 命令 | 必填参数 | 来源 |
|---|---|---|
| `workitem create` | `--title` `--actor` `--reason`（`--prefix` 可选） | [internal/cli/cli.go:392-400](../../../internal/cli/cli.go#L392-L400) |
| `workitem transition` | `--id` `--to` `--actor` `--reason` `--expect` | [internal/cli/cli.go:416-425](../../../internal/cli/cli.go#L416-L425) |
| `workitem claim` | `--id` `--owner` `--reason`（`--expect` 可选但建议必带） | [internal/cli/cli.go:441-449](../../../internal/cli/cli.go#L441-L449) |
| `recover` | `--actor` `--reason` | [internal/cli/cli.go:559-566](../../../internal/cli/cli.go#L559-L566) |
| `repair --dry-run` | `--actor` `--reason` | [internal/cli/cli.go:598-608](../../../internal/cli/cli.go#L598-L608) |
| `repair --apply --confirm <digest>` | `--actor` `--reason` `--confirm` | [internal/cli/cli.go:598-617](../../../internal/cli/cli.go#L598-L617) |
| `workflow start` | `--id` `--policy` `--actor` `--reason` `--expect`（租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:592-624](../../../internal/cli/cli.go#L592-L624) |
| `workflow step-complete` | `--id` `--actor` `--reason` `--expect`（`--to` 可选；租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:655-691](../../../internal/cli/cli.go#L655-L691) |
| `workflow pause` / `resume` / `cancel` | `--id` `--actor` `--reason` `--expect`（租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:693-735](../../../internal/cli/cli.go#L693-L735) |
| `approval request` | `--id` `--stage`（`scope=stage_gate`） `--actor` `--reason`（`--scope`、`--run` 可选） | [internal/cli/cli.go:822-867](../../../internal/cli/cli.go#L822-L867) |
| `approval approve` | `--id` `--by`（`--comment` 可选） | [internal/cli/cli.go:869-897](../../../internal/cli/cli.go#L869-L897) |
| `approval reject` | `--id` `--by` `--reason`（`scope=stage_gate` 时随状态变更走 `rejectStageGate`） | [internal/cli/cli.go:869-936](../../../internal/cli/cli.go#L869-L936) |

## `devsys next` 判定输出

[internal/next/evaluate.go](../../../internal/next/evaluate.go) 输出 `Report{Verdict, Reasons, Risks, Fixes, Next}`：

- `Verdict ∈ {"PASS", "CONCERNS", "FAIL"}`
- `Risks[].Kind ∈ {"expired_lease", "orphan_claim", "unreadable_lease", "stale_review", "blocked", "invalid_metadata", "invalid_policy", "pending_approval", "inspection_limited"}`
- `Next.Action ∈ {"recover_claim", "review", "start", "start_backlog", "milestone_review", "report_done"}`
- `Next.WorkitemID` / `Next.MilestoneID` 单值；`report_done` 时为空

`--json` 模式：`{ok: true, readiness, reasons[], risks[], fixes[], next}`。exit 恒 0；脚本按 `readiness` 分支。

## JSON 错误结构（`--json`）

`render` 在 `--json` 时输出（[internal/cli/cli.go:178-198](../../../internal/cli/cli.go#L178-L198)）：

```json
{
  "ok": false,
  "error": {
    "code": 4,
    "kind": "invalid",
    "message": "invalid managed state (N problems)",
    "problems": [
      {"file": "project.yaml", "line": 3, "field": "id", "reason": "expected string, got !!int"}
    ]
  }
}
```

`kind` 取值与 `codedError.kind` 一致：`"usage"` / `"precondition"` / `"invalid"` / `"internal"`。M2 起 workitem 领域错误的 `kind="workitem"`，M3 起又扩 `"approval"` / `"workflow"`。**调用脚本应当只信 `code` 字段做分支**，不要解析 `message`。

`devsys --json search <keyword>` 的成功载荷则是（[internal/cli/cli.go:270-282](../../../internal/cli/cli.go#L270-L282)）：

```json
{
  "ok": true,
  "root": "<cwd>",
  "query": "<keyword>",
  "matches": [{"path": "<rel>", "line": 12, "text": "<trimmed line>"}],
  "total": 7
}
```

`devsys --json doctor` / `repair --dry-run` / `workflow check` / `approval list` / `next` 分别输出 `InspectionReport` / `Plan` / `{ok, policies[], warnings[]}` / `{ok, approvals[]}` / `{ok, readiness, …}`。
## M4 错误分类：`*app.Error` 与 `Class()`

M4 把所有业务错误收口到 `internal/app/app.go` 的 `*app.Error` 类型：

```go
type Error struct {
    Kind     string            // 展示类型（9 种）
    Message  string
    Problems []config.Problem  // 仅 invalid / workflow 等结构化失败存在
}

func (e *Error) Class() string {
    switch e.Kind {
    case KindUsage, KindPrecondition, KindInternal:
        return e.Kind
    }
    return KindInvalid
}
```

### Kind 值域（9 种）

| 常量 | 值 | 含义 | Class |
|---|---|---|---|
| `KindUsage` | `usage` | 参数或调用形态错误 | `usage` |
| `KindPrecondition` | `precondition` | 状态/环境不满足（未初始化、版本哈希不符等） | `precondition` |
| `KindInternal` | `internal` | 系统/IO 失败 | `internal` |
| `KindInvalid` | `invalid` | 受管状态不可信（parse / 未知键 / schema_version） | `invalid` |
| `KindWorkflow` | `workflow` | 策略或实例操作拒绝 | `invalid` |
| `KindGate` | `gate` | 门禁未满足（artifact / comment / approval） | `invalid` |
| `KindQuality` | `quality` | 质量门未通过 | `invalid` |
| `KindWorkitem` | `workitem` | 工作项非法状态转换 / 锁冲突 | `invalid` |
| `KindApproval` | `approval` | 审批请求/决定/消费被拒绝 | `invalid` |

### `Class()` 四类归一

归一到 `usage / precondition / internal / invalid` 四类。CLI 与 MCP 共用此分类：

| `Class()` | CLI 退出码 | MCP tool error code |
|---|---|---|
| `usage` | `CodeUsage = 2` | `usage` |
| `precondition` | `CodePrecondition = 3` | `precondition` |
| `internal` | `CodeInternal = 1` | `internal` |
| `invalid` | `CodeInvalid = 4` | `invalid` |

### 构造器

- `Usagef(format, ...)` / `Preconditionf(format, ...)` / `Internalf(format, ...)` — 直接构造 `*app.Error`。
- `Invalidf(kind string, problems []config.Problem, format string, args ...)` — 构造结构化失败，`kind` 取 `KindInvalid / KindWorkflow / KindGate / KindQuality / KindWorkitem / KindApproval` 之一。
- `Classify(err)` — 任意 error 包装：`nil` 透传；`*app.Error` 透传；其它 → `Internalf("%v", err)`。

### CLI 翻译：`toCoded`

[internal/cli/cli.go:99-117](../../../internal/cli/cli.go#L99-L117)：

```go
func toCoded(err error) *codedError {
    var ae *app.Error
    if errors.As(err, &ae) {
        code := CodeInvalid
        switch ae.Class() {
        case app.KindUsage:        code = CodeUsage
        case app.KindPrecondition: code = CodePrecondition
        case app.KindInternal:     code = CodeInternal
        }
        return &codedError{code: code, kind: ae.Kind, msg: ae.Message, problems: ae.Problems}
    }
    var ce *codedError
    if errors.As(err, &ce) { return ce }
    return errInternal("%v", err)
}
```

M3 之前的 `workitemError` / `approvalError` / `mapWorkflowError` 三个工具函数被统一替换——任何 `*app.Error` 都走 `toCoded`。

### MCP 翻译：`fail` / `failNotice` / `usageFail`

`internal/mcp/tools.go:108-156` 的 `toolError` 结构 + `fail[Out any](err)`：

```go
type toolError struct {
    Code     string           `json:"code"`
    Message  string           `json:"message"`
    Problems []config.Problem `json:"problems,omitempty"`
    Notice   string           `json:"notice,omitempty"`
}
```

| 函数 | 行为 |
|---|---|
| `fail[Out](err)` | `*app.Error` → `toolError{Code: ae.Class()}`；其它 → `toolError{Code: "internal"}` |
| `failNotice[Out](err, notice)` | 同上，附 last-known-good `notice` |
| `usageFail[Out](format, args...)` | 构造 `app.Usagef` 并调用 `fail` |

返回的 `isError=true` tool result 通过 SDK `StructuredContent` 暴露给客户端。

### JSON 错误信封（CLI `--json`）

```json
{
  "ok": false,
  "error": {
    "code": 4,
    "kind": "approval",
    "message": "approval: not approved",
    "problems": null
  }
}
```

`problems` 仅当 `kind` 为结构化失败（`invalid` / `workflow` / `gate` / `quality` / `workitem` / `approval`）且构造时携带时存在；其它情况为 `null` 或省略。

### 调用脚本分支约定

只信 `code` 字段：

- `usage` (exit 2)：修正参数。
- `precondition` (exit 3)：运行 `devsys init` / `devsys recover` / 重新 `get` 取新 version。
- `internal` (exit 1)：重试或上报。
- `invalid` (exit 4)：按 `problems[]` 修复受管状态，或按 `message` 处理拒绝原因（门禁未满足、审批失效等）。

`kind` 字段是给人看的（让 stderr 更易读），**不是**分支依据。

## M4 输出形态：`--json` 与 `--jsonl`

- `--json`：成功输出走 stdout（`{ok: true, …}`），错误信封走 stderr（`{ok: false, error: {code, kind, message, problems?}}`）。
- `--jsonl`：列表类命令按「一行一条 JSON 记录」输出，字段名与 `--json` 信封内同名数组字段一致。
- `--quiet`：抑制成功输出；error / warning 始终走 stderr。
- 列表子命令：`workitem list` / `decision list` / `finding list` / `event list` / `artifact list` / `run list` / `approval list` / `workflow list` 都支持 `--jsonl`。
- 详情子命令（`get` / `create` / `update` 等）只支持 `--json`，不支持 `--jsonl`。

实现：[internal/cli/cli.go:124-133](../../../internal/cli/cli.go#L124-L133) 的 `writeJSONL[T]` 泛型。

## M4 知识层退出码（10/11 预留）

CLI 注释 [internal/cli/cli.go:33-44](../../../internal/cli/cli.go#L33-L44)：

```go
// The knowledge layer reserves its own 0/10/11 convention (0 fresh, 10 stale,
// 11 missing — 方案 §12.5) for `knowledge status`, arriving with M5.
const (
    CodeOK           = 0
    CodeInternal     = 1
    CodeUsage        = 2
    CodePrecondition = 3
    CodeInvalid      = 4
)
```

M4 阶段 `devsys knowledge status` 仍然返回 `CodeOK = 0`（与 `app.Error{Kind: precondition}` 同源，仅语义占位）。M5 达到时 `CodeKnowledgeStale = 10` / `CodeKnowledgeMissing = 11` 启用。

## 与存储层的边界

本契约**只**诊断"受管元数据文件"（`project.yaml`、`config.yaml`、`state/*.yaml`）。它**不**诊断：

- 项目级写锁或事务日志（属于 `internal/storage`）。
- 工作项 / 知识 / 事件 / 审批等领域文件（M1+ 在各自 package 内自行校验，落盘前必须通过对应 `domain.DecodeYAML`，未通过时由 `storage.CheckSchemaVersion` 拒绝）。
- 用户级注册表 `registry.yaml`（它有自己的解析器，见 [internal/registry/registry.go:84-106](../../../internal/registry/registry.go#L84-L106)，不在 schema_version 闸门管辖内）。
- 租约文件（`.devsys/scheduling/<id>.yaml`）：其 schema 由 `internal/workitem` 自管理，**不**走 `config.Load`；doctor / recover / repair 走 `internal/reconcile` 通过 `storage.Inspect` 检查。
- 工作流策略文件（`.devsys/workflows/<id>.md`）：由 `internal/workflow.Parse` 校验，**不**走 `config.Load`；`workflow check` 把 issue 转 `config.Problem` 后走 `errInvalid`。
- 审批文件（`.devsys/approvals/approval-<N>.yaml`）：由 `internal/approval` 通过 `domain.DecodeYAML` + `storage.CheckSchemaVersion` 校验。

"配置诊断"与"存储恢复"刻意分开：前者是 M0.4 的只读工具，后者是 M0.3 的写路径守护者。**不要**用 `config check` 去尝试恢复锁状态或回放事务日志——这两层互不调用。

## 演进规则（后续里程碑扩展白名单时）

每加一个新键到任一受管文件，**至少**同步以下三处：

1. `internal/config/validate.go` 的对应 `fileSpec`（新增字段 + 类型 + 必填；嵌套结构同步加 `fieldKind` 与 `checkNested` 分支）。
2. `internal/config/config.go` 的对应 `Project` / `Config` struct（让 `Load` 能解析；嵌套字段对应到 `domain.*` 类型）。
3. `internal/project/tree.go` 的对应占位 struct + builder（让 `init` 写出的字节仍然合法；M1 起优先用 `domain.EncodeYAML` 序列化）。

写命令前置校验 `config.Load` 会把第三处漏改的字段立刻暴露为"未知键"，从而保证占位文件与白名单**必须**同步。`domain.DecodeYAML` 在解码时若发现 `schema_version` 不等于 `domain.SchemaVersion` 会立即拒绝（[internal/domain/serialize.go:73-78](../../../internal/domain/serialize.go#L73-L78)），保证 struct 与白名单永远在同一个版本号上。

Workflow 策略文件新增字段时同步三处：

1. `internal/workflow/parse.go` 的 `knownTopLevel` 或嵌套 `mapping(known)`（新增字段 + 类型 + 必填 + 形状校验）。
2. `internal/workflow/policy.go` 的对应 struct。
3. 示例 `docs/examples/workflows/*.md`（端到端 `smoke-m3` 覆盖）。

写命令前置校验通过 `workflow.Load` 把 issue 转 `config.Problem`，问题定位保持 `file[:line][: field]: reason` 一致风格。

## M6 退出码语义扩展

M6 在 `CodePrecondition = 3` 与 `CodeInvalid = 4` 下扩展执行层错误语义：

| 触发点 | 来源 | 错误分类 |
|---|---|---|
| `devsys worktree prepare` 时 git 不可用 | `workspace.ErrGitMissing` → `app.Preconditionf` | `CodePrecondition` |
| `devsys worktree prepare --path` 越界（不在配置根内） | `workspace.Validate` → `app.Preconditionf` | `CodePrecondition` |
| `devsys run exec --harness <unknown>` | `harness.ByName` 返回 `false` → `app.Usagef` | `CodeUsage` |
| `devsys run exec --harness codex` 但 codex 未安装 | `harness.Availability.Installed == false` → `app.Preconditionf` | `CodePrecondition` |
| `devsys run exec --timeout 30s` 到期 | `harness.Result.TimedOut == true` → run 进 `timeout` 终态 | run 终态（不是 exit code） |
| `devsys run verify` `Advanced=false` 且未 `--force` | `app.refuseCompletion` → run 进 `failed`，workitem 进 `review` | run 终态（不是 exit code） |
| `devsys run complete` 验证未 advance | `app.RunFinish` 内部升 `*app.Error{Kind: workflow}` | `CodeInvalid`（kind="workflow"） |
| `devsys run fail` / `cancel` 已经终态 | `app.isTerminalRun` → `*app.Error{Kind: workflow}` | `CodeInvalid` |
| `devsys dispatch --once` 无候选 dispatchable | `app.Dispatch` 返回 `Report{Started: nil}` + notice | exit 0（不是错误） |
| `devsys dispatch --watch` Ctrl-C | `signal.NotifyContext` cancel | exit 0 |

## M7.1–M7.4 退出码语义扩展（视图域）

M7.1–M7.4 视图域只读入口（`workspace view` / `workspace build --static` / `workspace serve`）沿用既有 0/1/2/3 退出码（[internal/cli/cli.go:31-55](../../../internal/cli/cli.go#L31-L55)），**不**新设退出码、不复用 10/11：

| 触发点 | 来源 | 错误分类 |
|---|---|---|
| `devsys workspace` 子命令缺失或不在 `{view, build, serve}` | `runWorkspace` `errUsage`（[internal/cli/workspace.go:24-38](../../../internal/cli/workspace.go#L24-L38)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace view` 参数解析失败 / 含未知位置参数 | `runWorkspaceView` `errUsage`（[internal/cli/workspace.go:118-120](../../../internal/cli/workspace.go#L118-L120)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace view --limit N` 且 `N <= 0` | `runWorkspaceView` `errUsage`（[internal/cli/workspace.go:121-123](../../../internal/cli/workspace.go#L121-L123)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace view` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace.go:128-134](../../../internal/cli/workspace.go#L128-L134)） | `CodePrecondition = 3`（kind="precondition"） |
| `view.Build` 其它失败（inspect / read / decode） | `errInternal`（[internal/cli/workspace.go:132-133](../../../internal/cli/workspace.go#L132-L133)） | `CodeInternal = 1`（kind="internal"） |
| `devsys workspace build` 不带 `--static` | `runWorkspaceBuild` `errUsage`（[internal/cli/workspace.go:53-54](../../../internal/cli/workspace.go#L53-L54)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace build` 参数解析失败 / 含未知位置参数 | `errUsage`（[internal/cli/workspace.go:50-51](../../../internal/cli/workspace.go#L50-L51)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace build --out DIR` 非法 | `sitestatic.ResolveOut` 失败 → `errUsage`（[internal/cli/workspace.go:62-64](../../../internal/cli/workspace.go#L62-L64)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace build` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace.go:66-69](../../../internal/cli/workspace.go#L66-L69)） | `CodePrecondition = 3`（kind="precondition"） |
| `view.Build` 其它失败 / `sitestatic.Build` 写盘失败 | `errInternal`（[internal/cli/workspace.go:69-73](../../../internal/cli/workspace.go#L69-L73)） | `CodeInternal = 1`（kind="internal"） |
| `devsys workspace serve --host <non-loopback>` 无 `--allow-remote` | `runWorkspaceServe` `errUsage`（[internal/cli/workspace_serve.go:159-161](../../../internal/cli/workspace_serve.go#L159-L161)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace serve --port` 越界 / 带 `--json` / `--quiet` / `--limit N<=0` | `errUsage`（[internal/cli/workspace_serve.go:150-157](../../../internal/cli/workspace_serve.go#L150-L157)） | `CodeUsage = 2`（kind="usage"） |
| `devsys workspace serve` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace_serve.go:166-170](../../../internal/cli/workspace_serve.go#L166-L170)） | `CodePrecondition = 3`（kind="precondition"） |
| `net.Listen` 端口占用 / 服务运行期错误 | `errInternal`（[internal/cli/workspace_serve.go:172-193](../../../internal/cli/workspace_serve.go#L172-L193)） | `CodeInternal = 1`（kind="internal"） |
| 成功（含 `trust.state = advisory_unlocked` / `pending_transaction`） | `renderWorkspaceView` / `Build` 写 stdout，`render` 返 0 | `CodeOK = 0` |
| 成功且 `knowledge.status == "missing"` | 同上，但 `view.Knowledge` 段降级提示 | `CodeOK = 0`（`missing` 是事实标签，不是失败） |

`advisory_unlocked`（无锁文件、未跑过写命令）→ `view.Build` 仍渲染业务事实，`Trust.State` 写进 `view.Model.trust.state` 与人类输出 `trust: advisory_unlocked — <note>` 行；**不**算作错误。`pending_transaction` → `view.Build` 进入 §15.4 路径：`emptyBusiness` 抹掉业务事实，`Progress.Readiness` 走 `pendingReadiness`（`next.Evaluate` FAIL + `recoverCommand`），但**仍**退出码 0——`trust` 字段已经告诉调用方状态不可信。10/11 仍只承载 `devsys knowledge status`（[internal/cli/cli.go:52-56](../../../internal/cli/cli.go#L52-L56)）；视图层把 `view.Knowledge.Status` 用 `KnowledgeFresh / Stale / Missing / Unavailable` 字符串承载（[internal/view/view.go:46-51](../../../internal/view/view.go#L46-L51)），但不映射到退出码。
