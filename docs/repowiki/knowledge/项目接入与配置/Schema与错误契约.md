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
  - doctor INVALID 行
  - exit 4 不可信受管状态
  - InvalidFile
  - VisibleTools
  - WorkflowInitTemplate
  - BlueprintArtifactID
  - CreateWorkitemCommand
  - git 可选能力
  - setup 退出码
  - mcp install 退出码
description: "devsys 受管 YAML 文件的严格 schema：每文件白名单 + schema_version 闸门 + 行号定位 + Problems 错误结构（含 SeverityWarning） + 退出码 4 的语义边界 + M1 完整 Project 模型 + 嵌套字段校验；M2 新增退出码 3/4 在 workitem 与 repair 中的扩展语义、--expect 64-hex sha256 + fail-closed、--confirm 摘要格式、写命令 --actor/--reason 必填；M3 新增 workflow/approval/next 的 kind 与 code 语义、workitem.TransitionRequest.Guard 签名、policy issue 与 LKG 报错；M4 收口到 *app.Error.Class() 四类（映射 CLI 退出码 + MCP tool error code）、--jsonl 列表流、知识层 10/11 退出码预留；M5 新增 KnowledgePages / KnowledgeGenerator 配置键、kindStrings（接受 null 与空列表）+ kindMilestones 双路径、warning severity、KindKnowledge 错误类、knowledge_pages / knowledge_generator 字段解析；M6 增 workspace_root / dispatch_command 配置键、HookEnv 注入、run exec / run complete 在执行层的扩展退出码语义；M7.1 增 workspace view 在视图域的退出码语义（沿用 0/2/3/1，10/11 仍专属 knowledge status）、workspace view 的 --limit 与子命令缺失 → 2、缺 .devsys/ → 3、view.Build 失败 → 1、advisory_unlocked 是事实标签不是失败码。；**本批**：`config.yaml` 增 `default_policy` 键（未绑定实例的工作项按其过门禁；键值非法或指向缺失策略时 claim fail closed）、`project blueprint` 未声明蓝图 exit 0、非法 workitem id 归 usage/exit 2、`storage: version conflict` 附「重新读取重试 / `--latest`」出路。；本轮（ca58d27→19b9149，v0.1.9 发布链 + 主命令改名 workloom + Git 可选能力）：退出码 3 的「非 git 仓库」用法作废（`init` 允许非 Git 目录与 git 子目录，禁止自动 `git init`），`workspace.ErrGitMissing` 只在 worktree 操作上保留；`run verify` / `run complete` 的非 Git 路径以 `CompletionCheck.skipped=true` 跳过 Git 证据（不标 `Advanced`），`wire --check` 的 git 行改记能力缺失；CLI 退出码与 JSON 错误信封结构不变（仅前缀/文本改 `workloom`）"
source_commit: 21a9e71
generated: true
generator: repowiki-gen
---

# Schema 与错误契约

本卡描述 `github.com/JAYY513/Workloom/internal/config` 落地的**严格 schema 契约**以及 M1 起 `github.com/JAYY513/Workloom/internal/domain` 引入的统一记录契约：哪些字段是合法的、字段必须是什么类型、`schema_version` 不被支持时会发生什么、错误如何结构化传给调用方。**这是写命令与只读诊断命令共用的契约**，也是后续里程碑扩展白名单时的唯一参考。

## 契约的物理边界

| 名称 | 值 | 来源 |
|---|---|---|
| `config.SupportedSchemaVersion` | `1` | [internal/config/config.go:25](file://internal/config/config.go#L25) |
| `domain.SchemaVersion` | `1` | [internal/domain/models.go:6](file://internal/domain/models.go#L6) |
| 错误结构 | `Problem{File, Line, Field, Severity, Reason}`（M5 增 Severity）与 `Problems`；`SeverityWarning = "warning"` 常量（[internal/config/config.go:46-79](file://internal/config/config.go#L46-L79)） | [internal/config/config.go:46-79](file://internal/config/config.go#L46-L79) |
| 知识层配置键（M5） | `config.yaml` 新增 `knowledge_pages []string` 与 `knowledge_generator string`；`validate.go` 拆 `kindStrings`（接受 null / 空列表 / 字符串标量列表）与 `kindMilestones` 双路径 | [internal/config/config.go:104-114](file://internal/config/config.go#L104-L114)、[internal/config/validate.go:64-92](file://internal/config/validate.go#L64-L92) |
| 知识层错误类（M5） | `app.KindKnowledge = "knowledge"`；`Class()` 归一到 `KindInvalid`，CLI 退出码 4；MCP tool error code `invalid` | [internal/app/app.go:28-49](file://internal/app/app.go#L28-L49)、[internal/app/knowledge.go](file://internal/app/knowledge.go) |
| 记录契约 | `domain.{Project,Scope,Milestone,CurrentState,CurrentStateFile,MilestonesFile,WorkItem,Run,…}` | [internal/domain/models.go:8-221](file://internal/domain/models.go#L8-L221) |
| Workflow 策略契约 | `internal/workflow.Policy` 与 `internal/workflow.Issue` | [internal/workflow/policy.go](file://internal/workflow/policy.go)、[internal/workflow/parse.go](file://internal/workflow/parse.go) |
| Approval 契约 | `domain.Approval` 与 `approval.ErrXxx` | [internal/domain/models.go](file://internal/domain/models.go)、[internal/approval/approval.go:49-58](file://internal/approval/approval.go#L49-L58) |
| Next 判定契约 | `next.Report` 与 `Verdict` | [internal/next/evaluate.go](file://internal/next/evaluate.go) |

`config.SupportedSchemaVersion` 与 `domain.SchemaVersion` 必须**始终相等**：`domain.EncodeYAML` / `DecodeYAML` 在序列化末尾调用 `storage.CheckSchemaVersion(b, SchemaVersion)`（[internal/domain/serialize.go:26-28](file://internal/domain/serialize.go#L26-L28)）作为最后一道闸门。

**Workflow 策略文件不设 `schema_version`**：策略自身只有 `version`（必填 ≥1）字段，是策略文件语义版本；详见下文「Workflow 策略文件契约」小节。

**当前没有任何其它文件受本契约约束**。其它受写命令操作的 `.devsys/` 子目录（`workitems/`、`specs/`、`runs/`、`approvals/`、…）由后续里程碑定义各自的领域契约；但它们的字段形状最终都汇聚到 `internal/domain` 的对应 struct。

## 白名单：每文件允许的字段

由 `fileSpec.fields` 静态定义（[internal/config/validate.go:41-57](file://internal/config/validate.go#L41-L57)）。`schema_version` 始终是第一个、必填、必须是整数。

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

`init` 写出的 `project.yaml` 由 `domain.Project` struct 序列化（[internal/project/tree.go:60-66](file://internal/project/tree.go#L60-L66)），键序 = struct 字段序；`config` 包的白名单（[internal/config/validate.go:41-57](file://internal/config/validate.go#L41-L57)）与 struct 字段必须同步演进。

### `config.yaml`

| 字段 | 类型 | 必填？ | 备注 |
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `workspace_root` | `string` | 否 | **M6** 工作区根目录；绝对路径或相对项目根；缺失回退 `<project>/.devsys/workspaces`；M6 起 `app.WorkspacePrepare` / `app.RunExec` / `internal/workspace.Root` 共同使用 |
| `dispatch_command` | `string` | 否 | **M6** 调度 tick 默认每个 attempt 的 argv 模板；M6.4 默认 `"workloom run exec --id {run_id} --actor dispatch --reason tick"`；M6.7 起由 per-workitem `assigned_harness` 取代 |
| `default_policy` | `string` | 否 | **b89ffae** 工作项未绑 Workflow 实例时按它判定质量门 / 阶段门 / 提示词 / 工作区钩子（方案 §4.7/§5.3 项目级默认）；空保持现有行为（未绑实例的工作项 ungated）；`internal/project/tree.go:68-70` 的 `configYAML` 头注释列出全部可选键 |
### `state/current.yaml` 与 `state/milestones.yaml`

两个 state spec 在 M1 起被显式收紧（[internal/config/validate.go:63-73](file://internal/config/validate.go#L63-L73)）：

| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `current_state` | mapping | 否 | `kindCurrent` 嵌套结构，校验 `summary/risks/blockers/next_focus` |
> **结果**：M0.4 时 state 文件仅校验 schema_version；M1 起两者都被收紧到 `currentSpec` / `milestonesSpec`，并通过 `internal/domain.CurrentStateFile` / `MilestonesFile` 提供反序列化视图（[internal/project/tree.go:73-84](file://internal/project/tree.go#L73-L84)）。

## 字段类型：`fieldKind`

七档位（[internal/config/validate.go:17-25](file://internal/config/validate.go#L17-L25)）：

| Kind | YAML 形态 | 判定 |
|---|---|---|
| `kindInt` | `!!int` 标量 | `yaml.Node.Decode(&int)` 成功 |
| `kindString` | `!!str` 标量；非空（当 `required`） | `Kind==ScalarNode && Tag=="!!str"` |
| `kindTimestamp` | `!!str` 或 `!!timestamp`，必须能 `time.Parse(time.RFC3339, ...)` | 见 [internal/config/validate.go:182-191](file://internal/config/validate.go#L182-L191) |
| `kindStrings` | `!!null`（保留"未记录"态）或 `!!seq` 元素皆为 `!!str` | 见 [internal/config/validate.go:160-163](file://internal/config/validate.go#L160-L163) |
| `kindScope` | `!!map`，键仅 `in` / `out`，值皆 `kindStrings` | 见 [internal/config/validate.go:164-167](file://internal/config/validate.go#L164-L167) + [internal/config/nested.go:23-24](file://internal/config/nested.go#L23-L24) |
| `kindCurrent` | `!!map`，子键集合 = `summary/risks/blockers/next_focus` | 见 [internal/config/nested.go:25-26](file://internal/config/nested.go#L25-L26) |
| `kindMilestones` | `!!seq`，每元素 mapping 必填 `id/name/status`（皆 `kindString`） | 见 [internal/config/nested.go:18-22](file://internal/config/nested.go#L18-L22) |

任何不匹配的键值在 `checkField` 里返回 `Problem{Line: val.Line, Field: name, Reason: "expected <kind>, got <nodeKind>"}`（[internal/config/validate.go:157-193](file://internal/config/validate.go#L157-L193)）；嵌套失败则通过 `checkNested` / `checkMapping` 沿 `field[index]` / `field.child` 形式把路径展开（[internal/config/nested.go:8-68](file://internal/config/nested.go#L8-L68)）。

## `schema_version` 闸门

`validate` 的第一步是检查 `schema_version`（[internal/config/validate.go:100-128](file://internal/config/validate.go#L100-L128)）。它**早于**字段白名单检查，因此一份未知版本的受管文件**只**会产生一条 `unsupported version N` 问题，不会被后续"未知键 / 类型错误"的噪声淹没。

触发的拒绝文本（[internal/config/validate.go:125-128](file://internal/config/validate.go#L125-L128)）：

```text
unsupported version N (this build supports M); refusing to write, migration must be explicit
```

写命令（`init`）因为拿到这条 `Problems` 会返回 `CodeInvalid`；只读诊断（`config check`）同样返回 `CodeInvalid` 但仍继续（[internal/cli/diagnostics.go:90-93](file://internal/cli/diagnostics.go#L90-L93)）。

`domain.EncodeYAML` / `DecodeYAML` 在写出 / 读入时再次调用 `storage.CheckSchemaVersion`（[internal/domain/serialize.go:26-28、73-78](file://internal/domain/serialize.go#L26-L28)），保证领域记录的 schema_version 与配置层**始终一致**。

## 行号定位

每个 `Problem` 都带 `Line`（1-based，0 表示"整文件层级的问题"）。`yaml.Node.Line` 由 `gopkg.in/yaml.v3` 在解码时填入。覆盖以下情形：

- 顶层不是 mapping：根节点的 `Line`。
- `schema_version` 缺失：根节点 `Line`，`Field="schema_version"`。
- 重复键：第二次出现的 `Line`。
- 类型错误：值节点的 `Line`。
- 空文件 / 多文档 / 解析错误：整文件（`Line=0`）。
- 嵌套失败（如 `milestones[0].status` 缺失）：元素 / 子键节点的 `Line`，`Field` 形如 `milestones[0].status`（[internal/config/nested.go:13-22](file://internal/config/nested.go#L13-L22)）。
- Workflow 策略文件：[internal/workflow/parse.go](file://internal/workflow/parse.go) 用 `yaml.Node.Line` 同样填入；body 模板错误行号 = `bodyLine + rerr.Line - 1`（`splitFrontMatter` 输出 1-based 起始行）。

`Problem.String()` 渲染为 `file[:line][: field]: reason`（[internal/config/config.go:54-67](file://internal/config/config.go#L54-L67)）：

```text
project.yaml:3: id: expected string, got !!int
project.yaml:4: updated_at: unknown key
project.yaml:2: name: required
project.yaml:5: milestones[0].status: required
workflows/quick-fix.md:9: body: unclosed {{
workflows/feature-development.md:17: steps[3].status: unknown status "inprog" (valid: draft, backlog, ready, in_progress, review, verification, done, blocked, cancelled)
```

`workflow.Issue.String()` 复用同一渲染（[internal/workflow/policy.go:37-50](file://internal/workflow/policy.go#L37-L50)）。

## Workflow 策略文件契约（M3）

`internal/workflow.Parse(rel, data)` 解析 `.devsys/workflows/<id>.md`，前导 YAML front matter + 提示词正文。

### 顶层键白名单（[internal/workflow/parse.go:25-31](file://internal/workflow/parse.go#L25-L31)）

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

### 条件白名单（[internal/workflow/condition.go:57-65](file://internal/workflow/condition.go#L57-L65)）

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

`body` 是 front matter 下方整段文本（CRLF 闭合行后的空行**保留**为正文内容，verbatim 语义）。模板语法 `{{name}}`（点分标识符、括号内允许空白）；孤立 `}}` / 未闭合 `{{` / 空名 / 非法名在加载期报 `body: <reason>`（[internal/workflow/template.go](file://internal/workflow/template.go)）。

`$VAR` / `${VAR}` **不在**加载期解析；`ExpandEnv` 在使用时由调用方（M6 hook 执行）解析——「密钥不落盘」的可实现边界（原始依据见 git 历史中的 docs/开发记录.md）。

### LKG 报错（[internal/workflow/cache.go](file://internal/workflow/cache.go)）

`workflow.Resolve(ctx, root, id)` 失败时：

- 当前文件解析失败 + 无 LKG → `workflow policy %q is invalid: %s (no last-known-good retained)`
- 当前文件缺失 + 无 LKG → `workflow policy %q not found under .devsys/workflows/`
- 文件读取错 → `workflow policy %q: read: %w`

`workflow check` 走 `workflow.Load`，逐文件列 issues；error-severity → `config.Problem` → `errInvalid(problems)` → exit 4。

## 退出码契约

`CodeInvalid = 4`（[internal/cli/cli.go:44-56](file://internal/cli/cli.go#L44-L56)）的语义在 `usage` 文本里写明（[internal/cli/cli.go:102-108](file://internal/cli/cli.go#L102-L108)）：

```text
0  success
1  internal error
2  usage error
3  precondition error (not a git repository, wrong directory, permissions, digest mismatch)
4  invalid managed state (parse, field or schema_version problems)
```

### `CodeInvalid = 4`：受管状态存在但不可信

它**只**由 `errInvalid(config.Problems)` 触发（[internal/cli/cli.go:141-152](file://internal/cli/cli.go#L141-L152)）。写命令在拿到 `Problems` 时返回 `CodeInvalid` 并**不**修改磁盘。

M2 起 `CodeInvalid = 4` 还覆盖 workitem / reconcile 写路径下的领域错误：非 `storage.ErrNotInitialized` / `workitem.ErrNotFound` 的领域错误升为带 `kind="workitem"` 的 `codedError{Code: CodeInvalid}`（M4 起统一由 `toCoded` 翻译，现行实现见 [internal/cli/cli.go:156-175](file://internal/cli/cli.go#L156-L175)）。

M3 起又扩展：

- `kind="approval"`：`approval.ErrInvalidInput` 走 `errUsage`；`storage.ErrNotInitialized` 走 `errPrecondition`；其余 `ErrNotFound` / `ErrNotPending` / `ErrNotApproved` / `ErrAlreadyConsumed` / `ErrInvalidated` / `ErrMismatch` 升为带 `kind="approval"` 的 `CodeInvalid`（M4 起统一由 `toCoded` 翻译，现行实现见 [internal/cli/cli.go:156-175](file://internal/cli/cli.go#L156-L175)）。
- `kind="workflow"`：workflow 实例操作的拒绝（含 `WorkflowStepError` + `Resolve` 失败 + `Render` 错误）升为带 `kind="workflow"` 的 `CodeInvalid`（M4 起统一由 `toCoded` 翻译，现行实现见 [internal/cli/cli.go:156-175](file://internal/cli/cli.go#L156-L175)）。
- `workflow check` 把策略文件的结构/类型/必填/交叉引用错误升为 `CodeInvalid`（同 `config check` 路径）。

### `CodePrecondition = 3`：前提不满足

原本只承担"环境侧错误"（当时的清单是「非 git 仓库、git 不在 PATH、CWD 不是仓库根」；**本轮起 Git 不再是入场券**，见下）。M2 在同一退出码下聚合新用法（[internal/cli/cli.go:48](file://internal/cli/cli.go#L48)）：

| 触发点 | 来源 | 错误提示 |
|---|---|---|
| **~~非 git 仓库~~（本轮已废）** / 嵌套项目 / 目标不是目录 | `project.PreconditionError` → `codedError{Code: CodePrecondition}` | `.devsys/ already exists at <parent>; refusing to nest another project`、`<abs> is not a directory`（[internal/project/init.go:60-67](file://internal/project/init.go#L60-L67)） |
| worktree 操作需要 git 而 git 不在 PATH | `workspace.ErrGitMissing` → `errPrecondition` | `git is not available on PATH`（[internal/workspace/git.go:12-14](file://internal/workspace/git.go#L12-L14)）；**目录工作区不需要 git**，`Ensure` / `List` 不再要求 |
| `.devsys/` 不存在（`search` / `workitem get` / `workitem create` 等） | `storage.ErrNotInitialized` / `workitem.ErrNotFound` → `errPrecondition`（M4 起经 `toCoded` 翻译） | `project not initialized; run workloom init` 等 |
| ~~workitem `--expect` 哈希与现状不符~~（M2 时归此类；**现行已改** `CodeInvalid = 4` / kind=`workitem`，见 [internal/app/workitem.go:475-480](file://internal/app/workitem.go#L475-L480)） | `expectedSnapshot` 返回 `version mismatch` | `version mismatch: work item changed since your read; rerun workitem get` |
| `repair --apply` 收到的 `--confirm` 与重跑摘要不符 | `reconcile.ErrDigestMismatch` → `errPrecondition` | `confirmation digest does not match current state; run `workloom repair --dry-run` again` |
| workflow / approval / project 实例操作 `--expect` 不匹配（现行同改 `CodeInvalid = 4`，[internal/app/project.go:364](file://internal/app/project.go#L364)） | `expectedSnapshot` 返回 `version mismatch` | 同上 |
| approval 操作时 `.devsys/` 不存在 | `storage.ErrNotInitialized` → `errPrecondition` | `project not initialized; run workloom init` 等 |

第 3–5 行即方案 §15.4 "推断→确认→重写" / "快照必填" 闭环在退出码层面的体现：调用方必须重新读证据 / 重新干跑，才能继续动盘。脚本可以**只信 `code == 3` 重新发起一次 `get` / `dry-run`**。

### `CodeUsage = 2`：参数错误

M3 新增触发：

- `approval list --status` 取值不在 `{pending, approved, rejected}` → exit 2。
- `workflow` / `approval` 子命令缺失 → stdout 打印用法文本 + **exit 0**（v0.1.4 起统一走 `familyUsage`，[internal/cli/cli.go:347-353](file://internal/cli/cli.go#L347-L353)）；子命令不在已知集合 → `errUsage` → exit 2。

M7.1–M7.4 在 `CodeUsage = 2` 与 `CodePrecondition = 3` / `CodeInternal = 1` 下扩展 `workloom workspace` 子命令面（视图域只读，与 §4.8 `worktree` 子命令独立）：
- `workloom workspace` 子命令缺失 → stdout 打印用法 + **exit 0**（`familyUsage`）；子命令不在 `{view, build, serve}` → exit 2：`errUsage("unknown `workloom workspace` subcommand %q", rest[0])`（[internal/cli/workspace.go:24-38](file://internal/cli/workspace.go#L24-L38)）。
- `workloom workspace view --limit N` 且 `N <= 0` → exit 2：`errUsage("`--limit` must be positive")`（[internal/cli/workspace.go:121-122](file://internal/cli/workspace.go#L121-L122)）。
- `workloom workspace build` 不带 `--static`（M7.2 唯一支持的站点形态）→ exit 2：`errUsage("`workloom workspace build` needs --static (the only site form in M7.2)")`（[internal/cli/workspace.go:53-55](file://internal/cli/workspace.go#L53-L55)）；参数解析失败 / 含未知位置参数 → exit 2：`errUsage("`workloom workspace build --static [--out DIR] [--limit N]`")`（[internal/cli/workspace.go:50-52](file://internal/cli/workspace.go#L50-L52)）；非法 `--out` → exit 2：`errUsage("workspace build: %v", err)`（[internal/cli/workspace.go:63-66](file://internal/cli/workspace.go#L63-L66)）。
- `workloom workspace serve --host <non-loopback>` 且未带 `--allow-remote` → exit 2：`errUsage("refusing non-loopback bind %q without --allow-remote ...")`（[internal/cli/workspace_serve.go:170-172](file://internal/cli/workspace_serve.go#L170-L172)）；`--port` 越界或带 `--json`/`--quiet` → exit 2（[internal/cli/workspace_serve.go:164-168](file://internal/cli/workspace_serve.go#L164-L168)）。

`workspace view` / `build` / `serve` 不引入新退出码：成功（含 `trust.state = advisory_unlocked` / `pending_transaction`）→ 0；缺 `.devsys/`（`storage.ErrNotInitialized`）→ 3（precondition）；`view.Build` 其它失败 → 1（internal）；`workspace build` 写盘失败 → 1（`sitestatic.Build` 错误，[internal/cli/workspace.go:72-78](file://internal/cli/workspace.go#L72-L78)）；`workspace serve` 端口占用（EADDRINUSE，含 Windows Winsock 10048，`addrInUse` 判定）→ 3（`errPrecondition`，v0.1.4 起；[internal/cli/workspace_serve.go:183-191](file://internal/cli/workspace_serve.go#L183-L191)），其它 `net.Listen` 失败 / 服务运行期错误 → 1（`errInternal`）。**build/serve 沿用同一 0/1/2/3/4**，10/11 仍专属 `workloom knowledge status`（[internal/cli/cli.go:53-56](file://internal/cli/cli.go#L53-L56)）。

## 本批（M9 #336/#337 提交 362b637 / 7e08e3d）

### `CodeInvalid = 4` 新增触发点

- `workloom doctor` 人类模式在 `InspectionReport.InvalidFiles` 非空时打 `INVALID  <path>  <err>` 行（[internal/cli/diagnostics.go:156-158](file://internal/cli/diagnostics.go#L156-L158)）；`--json` 模式 `rep.InvalidFiles` 嵌入信封同时 `exitWithCode(CodeInvalid)`（[internal/cli/diagnostics.go:131-139](file://internal/cli/diagnostics.go#L131-L139)、[internal/cli/diagnostics.go:170-174](file://internal/cli/diagnostics.go#L170-L174)）。健康项目（`InvalidFiles == nil`）→ exit 0。新增的 `reconcile.InvalidFile{Path, Err}`（[internal/reconcile/reconcile.go:107-114](file://internal/reconcile/reconcile.go#L107-L114)）是 doctor 的 1-based 错误载体：`String()` 返回 `path: err`，`InspectionReport.InvalidFiles` 字段 json/yaml tag `invalid_files,omitempty`。`readAllWorkitems` / `buildProposals` 在解码失败时**继续**扫其余文件，让一条坏 item 不遮蔽整棵树；doctor 的 `Note` 追加 `one or more work item files are unreadable…`。`RepairDryRun` 有 invalid 时拒绝 `cannot plan repairs: …`——保持 dry-run 的 fail-closed 承诺。
- `workloom next` / `prime` / `session start` / `project status` 在 `len(doc.InvalidFiles) > 0` 时由 [internal/app/next.go:28-46](file://internal/app/next.go#L28-L46) 装配 `next.Report{}` + `Invalidf(KindInvalid, nil, "next: managed state unreadable: <file>: <err>…")`：消息体 ≤ 3 条 + `+N more`。`Class()` 归一 `KindInvalid` → `CodeInvalid = 4`。理由：基于不完整工作项列表出的 verdict 会推荐错误动作，按方案 §14.1 视为 untrusted state。

### `CodeUsage = 2` 新增触发点

- `workloom mcp serve` 在构造 cfg 后调 `mcp.VisibleTools(cfg)` 拿到 `0` 工具时走 `errUsage("mcp serve: %s %s %s no tools in tier %s; use --tier standard", word, strings.Join(quoted, ", "), verb, tier)`（单 profile 渲染为 `mcp serve: profile "session" has no tools in tier core; use --tier standard`）（[internal/cli/mcp.go:79-91](file://internal/cli/mcp.go#L79-L91)）——多 profile 时单复数与动词变 `profiles %s have`。零工具的 silent server 是最常见的隐藏陷阱（只读 profile + core tier），文案直接给出 `--tier standard` 出路。
- `workloom workflow init --template <id>`（[internal/cli/cli.go:1212-1243](file://internal/cli/cli.go#L1212-L1243)）：usage 列四模板 `quick-fix / feature-development / architecture-change / reference-template`；未知 id / 已存在 → `Usagef`。`workloom project update --blueprint-artifact <artifact-id>`（fs.Visit 模式，[internal/cli/cli.go:721-747](file://internal/cli/cli.go#L721-L747)）：empty string clears；非空须 `record.KindArtifact.ValidID`（形态错 → `Usagef` / exit 2），未注册（`GetArtifact` ErrNotFound → `Preconditionf` / exit 3 + 出路 `workloom artifact register`）。

### 新增 schema 字段

- `project.yaml.blueprint_artifact_id`（已存在）：本批起 `ProjectUpdate` 把它纳入写事务；空串合法（清除），非空须指向已注册 artifact。`UpdateProjectRequest.BlueprintArtifactID *string`（nil = 不动；`&""` = 清除；`&"<id>"` = 校验 + 写）由 [internal/app/project.go](file://internal/app/project.go) 包装；MCP `project_update` 输入增 `blueprint_artifact_id`（[internal/mcp/tools_project.go](file://internal/mcp/tools_project.go)）。

## `--expect`：64-hex sha256 + fail-closed

`workitem transition` / `claim` / `workflow start` / `step-complete` / `pause` / `resume` / `cancel` 的 `--expect` 是调用方持有的"工作项快照版本哈希"，由 `workitem get` 在 `--json` 模式下的 `version` 字段给出（[internal/cli/cli.go:886-894](file://internal/cli/cli.go#L886-L894)）。

格式与解析：

- 编码：小写 64 字符十六进制串，等价于 `fmt.Sprintf("%x", storage.HashBytes(raw))`，即 sha256 的字节级表示。
- 解析：`strings.TrimSpace` 后用 `hex.DecodeString` 解码；解码结果长度必须等于 `crypto/sha256.Size`（32 字节）；解码失败、长度错误、哈希不匹配**全部**视为哈希不匹配，统一返回 `version mismatch` 错误。
- fail-closed：`expectedSnapshot` 在哈希不匹配时**不**返回 `raw` 字节；底层 `Transition` / `Claim` / `WorkflowStart` / `WorkflowStepComplete` / `WorkflowSignal` 拿不到 `Expected` 入参，写路径被彻底拦截。
- 退出码：不匹配 → `CodeInvalid = 4`（`Invalidf(KindWorkitem, …)`，[internal/app/workitem.go:475-480](file://internal/app/workitem.go#L475-L480)；M2 时曾归 `CodePrecondition = 3`，本刷新窗口内已改为 invalid/workitem），错误提示 `version mismatch: work item changed since your read; rerun workitem get`。

调用契约：

```sh
VERSION=$(bin/workloom.exe --json workitem get WLM-0001 | jq -r .version)
bin/workloom.exe workitem transition --id WLM-0001 --to in_progress \
    --actor alice --reason "M3 kickoff" --expect "$VERSION"
```

## `--confirm`：确定性 digest + 摘要回放

`workloom repair --apply --confirm <digest>` 的 `--confirm` 是 `repair --dry-run` 在 `--json` 模式下的 `plan.digest` 字段（[internal/cli/diagnostics.go:283-296](file://internal/cli/diagnostics.go#L283-L296)）。

格式与判定（[internal/cli/diagnostics.go:238-258](file://internal/cli/diagnostics.go#L238-L258) + [internal/reconcile/reconcile.go:319-335](file://internal/reconcile/reconcile.go#L319-L335)）：

- 编码：`reconcile.ComputeDigest(plan.Proposals)` 输出小写十六进制摘要；墙钟时间**不**参与计算，相同证据输入产生相同摘要。
- 判定：`--apply` 内部**先**重跑 `reconcile.RepairDryRun` 取得当前 `Digest`，**再**用调用方的 `--confirm` 覆盖 `plan.Digest`，**再**调 `reconcile.RepairApply`；摘要不一致由 `reconcile.ErrDigestMismatch` 上浮为 `CodePrecondition = 3`，并提示 `run `workloom repair --dry-run` again`。
- 空摘要：`--apply` 在缺 `--confirm` 时直接返回 `CodeUsage = 2`，**不**进入 reconcile（[internal/cli/diagnostics.go:239-241](file://internal/cli/diagnostics.go#L239-L241)）。

调用契约：

```sh
DIGEST=$(bin/workloom.exe --json repair --dry-run --actor alice --reason "M3 audit" | jq -r .plan.digest)
bin/workloom.exe repair --apply --confirm "$DIGEST" --actor alice --reason "M3 audit"
```

## 写命令必填 `--actor` 与 `--reason`

所有动盘的命令在 CLI 层强制记录操作者与原因（审计追溯），缺失即返回 `CodeUsage = 2`：

| 命令 | 必填参数 | 来源 |
|---|---|---|
| `workitem create` | `--title` `--actor` `--reason`（`--prefix` 可选） | [internal/cli/cli.go:898-911](file://internal/cli/cli.go#L898-L911) |
| `workitem transition` | `--id` `--to` `--actor` `--reason` `--expect` | [internal/cli/cli.go:965-989](file://internal/cli/cli.go#L965-L989) |
| `workitem claim` | `--id` `--owner` `--reason`（`--expect` 可选但建议必带） | [internal/cli/cli.go:991-1023](file://internal/cli/cli.go#L991-L1023) |
| `recover` | `--actor` `--reason` | [internal/cli/diagnostics.go:183-190](file://internal/cli/diagnostics.go#L183-L190) |
| `repair --dry-run` | `--actor` `--reason` | [internal/cli/diagnostics.go:223-232](file://internal/cli/diagnostics.go#L223-L232) |
| `repair --apply --confirm <digest>` | `--actor` `--reason` `--confirm` | [internal/cli/diagnostics.go:223-232](file://internal/cli/diagnostics.go#L223-L232) |
| `workflow start` | `--id` `--policy` `--actor` `--reason` `--expect`（租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:1378-1422](file://internal/cli/cli.go#L1378-L1422) |
| `workflow step-complete` | `--id` `--actor` `--reason` `--expect`（`--to` 可选；租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:1424-1454](file://internal/cli/cli.go#L1424-L1454) |
| `workflow pause` / `resume` / `cancel` | `--id` `--actor` `--reason` `--expect`（租约活跃时 `--owner` `--token`） | [internal/cli/cli.go:1455-1484](file://internal/cli/cli.go#L1455-L1484) |
| `approval request` | `--id` `--stage`（`scope=stage_gate`） `--actor` `--reason`（`--scope`、`--run` 可选） | [internal/cli/cli.go:1595-1621](file://internal/cli/cli.go#L1595-L1621) |
| `approval approve` | `--id` `--by`（`--comment` 可选） | [internal/cli/cli.go:1623-1650](file://internal/cli/cli.go#L1623-L1650) |
| `approval reject` | `--id` `--by` `--reason`（`scope=stage_gate` 时随状态变更走 `rejectStageGate`） | [internal/cli/cli.go:1623-1672](file://internal/cli/cli.go#L1623-L1672) |

## `workloom next` 判定输出

[internal/next/evaluate.go](file://internal/next/evaluate.go) 输出 `Report{Verdict, Reasons, Risks, Fixes, Next}`：

- `Verdict ∈ {"PASS", "CONCERNS", "FAIL"}`
- `Risks[].Kind ∈ {"expired_lease", "orphan_claim", "unreadable_lease", "stale_review", "blocked", "invalid_metadata", "invalid_policy", "pending_approval", "inspection_limited"}`
- `Next.Action ∈ {"recover_claim", "review", "start", "start_backlog", "milestone_review", "report_done"}`
- `Next.WorkitemID` / `Next.MilestoneID` 单值；`report_done` 时为空

`--json` 模式：`{ok: true, readiness, reasons[], risks[], fixes[], next}`。exit 恒 0；脚本按 `readiness` 分支。

## JSON 错误结构（`--json`）

`render` 在 `--json` 时输出（[internal/cli/cli.go:357-388](file://internal/cli/cli.go#L357-L388)）：

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

`workloom --json search <keyword>` 的成功载荷则是（[internal/cli/diagnostics.go:45-57](file://internal/cli/diagnostics.go#L45-L57)）：

```json
{
  "ok": true,
  "root": "<cwd>",
  "query": "<keyword>",
  "matches": [{"path": "<rel>", "line": 12, "text": "<trimmed line>"}],
  "total": 7
}
```

`workloom --json doctor` / `repair --dry-run` / `workflow check` / `approval list` / `next` 分别输出 `InspectionReport` / `Plan` / `{ok, policies[], warnings[]}` / `{ok, approvals[]}` / `{ok, readiness, …}`。
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

[internal/cli/cli.go:156-175](file://internal/cli/cli.go#L156-L175)：

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

`internal/mcp/tools.go:120-172` 的 `toolError` 结构 + `fail[Out any](err)`：

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
- `precondition` (exit 3)：运行 `workloom init` / `workloom recover` / 重新 `get` 取新 version。
- `internal` (exit 1)：重试或上报。
- `invalid` (exit 4)：按 `problems[]` 修复受管状态，或按 `message` 处理拒绝原因（门禁未满足、审批失效等）。

`kind` 字段是给人看的（让 stderr 更易读），**不是**分支依据。

## M4 输出形态：`--json` 与 `--jsonl`

- `--json`：成功输出走 stdout（`{ok: true, …}`），错误信封走 stderr（`{ok: false, error: {code, kind, message, problems?}}`）。
- `--jsonl`：列表类命令按「一行一条 JSON 记录」输出，字段名与 `--json` 信封内同名数组字段一致。
- `--quiet`：抑制成功输出；error / warning 始终走 stderr。
- 列表子命令：`workitem list` / `decision list` / `finding list` / `event list` / `artifact list` / `run list` / `approval list` / `workflow list` 都支持 `--jsonl`。
- 详情子命令（`get` / `create` / `update` 等）只支持 `--json`，不支持 `--jsonl`。

实现：[internal/cli/cli.go:180-188](file://internal/cli/cli.go#L180-L188) 的 `writeJSONL[T]` 泛型。

## M4 知识层退出码（10/11 预留）

CLI 注释 [internal/cli/cli.go:44-56](file://internal/cli/cli.go#L44-L56)：

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

M4 阶段 `workloom knowledge status` 仍然返回 `CodeOK = 0`（与 `app.Error{Kind: precondition}` 同源，仅语义占位）。M5 达到时 `CodeKnowledgeStale = 10` / `CodeKnowledgeMissing = 11` 启用。

## 与存储层的边界

本契约**只**诊断"受管元数据文件"（`project.yaml`、`config.yaml`、`state/*.yaml`）。它**不**诊断：

- 项目级写锁或事务日志（属于 `internal/storage`）。
- 工作项 / 知识 / 事件 / 审批等领域文件（M1+ 在各自 package 内自行校验，落盘前必须通过对应 `domain.DecodeYAML`，未通过时由 `storage.CheckSchemaVersion` 拒绝）。
- 用户级注册表 `registry.yaml`（它有自己的解析器，见 [internal/registry/registry.go:84-106](file://internal/registry/registry.go#L84-L106)，不在 schema_version 闸门管辖内）。
- 租约文件（`.devsys/scheduling/<id>.yaml`）：其 schema 由 `internal/workitem` 自管理，**不**走 `config.Load`；doctor / recover / repair 走 `internal/reconcile` 通过 `storage.Inspect` 检查。
- 工作流策略文件（`.devsys/workflows/<id>.md`）：由 `internal/workflow.Parse` 校验，**不**走 `config.Load`；`workflow check` 把 issue 转 `config.Problem` 后走 `errInvalid`。
- 审批文件（`.devsys/approvals/approval-<N>.yaml`）：由 `internal/approval` 通过 `domain.DecodeYAML` + `storage.CheckSchemaVersion` 校验。

"配置诊断"与"存储恢复"刻意分开：前者是 M0.4 的只读工具，后者是 M0.3 的写路径守护者。**不要**用 `config check` 去尝试恢复锁状态或回放事务日志——这两层互不调用。

## 演进规则（后续里程碑扩展白名单时）

每加一个新键到任一受管文件，**至少**同步以下三处：

1. `internal/config/validate.go` 的对应 `fileSpec`（新增字段 + 类型 + 必填；嵌套结构同步加 `fieldKind` 与 `checkNested` 分支）。
2. `internal/config/config.go` 的对应 `Project` / `Config` struct（让 `Load` 能解析；嵌套字段对应到 `domain.*` 类型）。
3. `internal/project/tree.go` 的对应占位 struct + builder（让 `init` 写出的字节仍然合法；M1 起优先用 `domain.EncodeYAML` 序列化）。

写命令前置校验 `config.Load` 会把第三处漏改的字段立刻暴露为"未知键"，从而保证占位文件与白名单**必须**同步。`domain.DecodeYAML` 在解码时若发现 `schema_version` 不等于 `domain.SchemaVersion` 会立即拒绝（[internal/domain/serialize.go:73-78](file://internal/domain/serialize.go#L73-L78)），保证 struct 与白名单永远在同一个版本号上。

Workflow 策略文件新增字段时同步三处：

1. `internal/workflow/parse.go` 的 `knownTopLevel` 或嵌套 `mapping(known)`（新增字段 + 类型 + 必填 + 形状校验）。
2. `internal/workflow/policy.go` 的对应 struct。
3. 示例 `docs/examples/workflows/*.md`（端到端 `smoke-m3` 覆盖）。

写命令前置校验通过 `workflow.Load` 把 issue 转 `config.Problem`，问题定位保持 `file[:line][: field]: reason` 一致风格。

## M6 退出码语义扩展

M6 在 `CodePrecondition = 3` 与 `CodeInvalid = 4` 下扩展执行层错误语义：

| 触发点 | 来源 | 错误分类 |
|---|---|---|
| `workloom worktree prepare` 时 git 不可用 | `workspace.ErrGitMissing` → `app.Preconditionf` | `CodePrecondition` |
| `workloom worktree prepare --path` 越界（不在配置根内） | `workspace.Validate` → `app.Preconditionf` | `CodePrecondition` |
| `workloom run exec --harness <unknown>` | `harness.ByName` 返回 `false` → `app.Usagef` | `CodeUsage` |
| `workloom run exec --harness codex` 但 codex 未安装 | `harness.Availability.Installed == false` → `app.Preconditionf` | `CodePrecondition` |
| `workloom run exec --timeout 30s` 到期 | `harness.Result.TimedOut == true` → run 进 `timeout` 终态 | run 终态（不是 exit code） |
| `workloom run verify` `Advanced=false` 且未 `--force` | `app.refuseCompletion` → run 进 `failed`，workitem 进 `review` | run 终态（不是 exit code） |
| `workloom run complete` 验证未 advance | `app.RunFinish` 内部升 `*app.Error{Kind: workflow}` | `CodeInvalid`（kind="workflow"） |
| `workloom run fail` / `cancel` 已经终态 | `app.isTerminalRun` → `*app.Error{Kind: workflow}` | `CodeInvalid` |
| `workloom dispatch --once` 无候选 dispatchable | `app.Dispatch` 返回 `Report{Started: nil}` + notice | exit 0（不是错误） |
| `workloom dispatch --watch` Ctrl-C | `signal.NotifyContext` cancel | exit 0 |

| **P1** `mcp serve --tier <unknown>` | `mcp.ParseTier` 失败（[internal/mcp/server.go:47-60](file://internal/mcp/server.go#L47-L60)）→ `app.Usagef` | `CodeUsage` |
| **c150007** 写命令同时给 `--expect` 与 `--latest` | `checkLatest`（[internal/cli/cli.go:592-598](file://internal/cli/cli.go#L592-L598)）→ `errUsage` | `CodeUsage` |
| **d726ed4** `workitem <cmd> --id <malformed>`（路径穿越 / 多行 / 含换行） | `app.storeError`（[internal/app/workitem.go:447-484](file://internal/app/workitem.go#L447-L484)）`workitem.ReadSnapshot` id 形状校验 → `app.Usagef("invalid workitem id <id>")` | `CodeUsage` |
| **b89ffae** `claim` 时 `config.yaml` 非法 / 默认策略缺失 | `policyForWorkItem`（[internal/app/workitem.go:505-516](file://internal/app/workitem.go#L505-L516)）→ `fmt.Errorf("config.yaml is invalid: ...; claims stay blocked until the file is fixed (\`workloom config check\`)")` 经 `app.Classify` 包装 | `CodeInvalid` |

## M7.1–M7.4 退出码语义扩展（视图域）

| 触发点 | 来源 | 错误分类 |
|---|---|---|
| `workloom workspace` 子命令缺失 | `runWorkspace` `familyUsage` → stdout 用法 + exit 0（[internal/cli/workspace.go:24-38](file://internal/cli/workspace.go#L24-38)） | `CodeOK = 0`（v0.1.4 起；未知子命令才 → `CodeUsage = 2`） |
| `workloom workspace view` 参数解析失败 / 含未知位置参数 | `runWorkspaceView` `errUsage`（[internal/cli/workspace.go:118-119](file://internal/cli/workspace.go#L118-L119)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace view --limit N` 且 `N <= 0` | `runWorkspaceView` `errUsage`（[internal/cli/workspace.go:121-122](file://internal/cli/workspace.go#L121-L122)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace view` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace.go:129-132](file://internal/cli/workspace.go#L129-L132)） | `CodePrecondition = 3`（kind="precondition"） |
| `view.Build` 其它失败（inspect / read / decode） | `errInternal`（[internal/cli/workspace.go:133-133](file://internal/cli/workspace.go#L133-L133)） | `CodeInternal = 1`（kind="internal"） |
| `workloom workspace build` 不带 `--static` | `runWorkspaceBuild` `errUsage`（[internal/cli/workspace.go:53-55](file://internal/cli/workspace.go#L53-L55)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace build` 参数解析失败 / 含未知位置参数 | `errUsage`（[internal/cli/workspace.go:50-52](file://internal/cli/workspace.go#L50-L52)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace build --out DIR` 非法 | `sitestatic.ResolveOut` 失败 → `errUsage`（[internal/cli/workspace.go:63-66](file://internal/cli/workspace.go#L63-L66)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace build` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace.go:68-71](file://internal/cli/workspace.go#L68-L71)） | `CodePrecondition = 3`（kind="precondition"） |
| `view.Build` 其它失败 / `sitestatic.Build` 写盘失败 | `errInternal`（[internal/cli/workspace.go:72-78](file://internal/cli/workspace.go#L72-L78)） | `CodeInternal = 1`（kind="internal"） |
| `workloom workspace serve --host <non-loopback>` 无 `--allow-remote` | `runWorkspaceServe` `errUsage`（[internal/cli/workspace_serve.go:170-172](file://internal/cli/workspace_serve.go#L170-L172)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace serve --port` 越界 / 带 `--json` / `--quiet` / `--limit N<=0` | `errUsage`（[internal/cli/workspace_serve.go:161-168](file://internal/cli/workspace_serve.go#L161-L168)） | `CodeUsage = 2`（kind="usage"） |
| `workloom workspace serve` 且 `.devsys/` 不存在 | `view.Build` 返回 `storage.ErrNotInitialized` → `errPrecondition`（[internal/cli/workspace_serve.go:177-181](file://internal/cli/workspace_serve.go#L177-L181)） | `CodePrecondition = 3`（kind="precondition"） |
| `net.Listen` 端口占用（EADDRINUSE / Winsock 10048） | `addrInUse` 判定 → `errPrecondition`（[internal/cli/workspace_serve.go:183-191](file://internal/cli/workspace_serve.go#L183-L191)） | `CodePrecondition = 3`（kind="precondition"） |
| 服务运行期错误 / 其它 `net.Listen` 失败 | `errInternal`（[internal/cli/workspace_serve.go:183-207](file://internal/cli/workspace_serve.go#L183-L207)） | `CodeInternal = 1`（kind="internal"） |
| 成功（含 `trust.state = advisory_unlocked` / `pending_transaction`） | `renderWorkspaceView` / `Build` 写 stdout，`render` 返 0 | `CodeOK = 0` |
| 成功且 `knowledge.status == "missing"` | 同上，但 `view.Knowledge` 段降级提示 | `CodeOK = 0`（`missing` 是事实标签，不是失败） |

`advisory_unlocked`（无锁文件、未跑过写命令）→ `view.Build` 仍渲染业务事实，`Trust.State` 写进 `view.Model.trust.state` 与人类输出 `trust: advisory_unlocked — <note>` 行；**不**算作错误。`pending_transaction` → `view.Build` 进入 §15.4 路径：`emptyBusiness` 抹掉业务事实，`Progress.Readiness` 走 `pendingReadiness`（`next.Evaluate` FAIL + `recoverCommand`），但**仍**退出码 0——`trust` 字段已经告诉调用方状态不可信。10/11 仍只承载 `workloom knowledge status`（[internal/cli/cli.go:53-57](file://internal/cli/cli.go#L53-L57)）；视图层把 `view.Knowledge.Status` 用 `KnowledgeFresh / Stale / Missing / Unavailable` 字符串承载（[internal/view/view.go:46-51](file://internal/view/view.go#L46-L51)），但不映射到退出码。

## 本批（v0.1.9，ca58d27→19b9149）

### Git 可选能力对错误契约的影响（cc5de1f）

- 退出码 3 里「非 git 仓库」这一用法**作废**：`init` 的项目身份是含 `.devsys/` 的目录，允许非 Git 目录与 git 子目录，且**禁止**自动 `git init`（[internal/project/init.go:40-46](file://internal/project/init.go#L40-L46)）。剩下的 `project.PreconditionError` 只有两条：目标不是目录、祖先已有 `.devsys/`（[internal/project/init.go:60-67](file://internal/project/init.go#L60-L67)）——错误文本 `<abs> is not a directory`、`.devsys/ already exists at <parent>; refusing to nest another project`。
- `workspace.ErrGitMissing`（`git is not available on PATH`）**只在需要 git 的路径**上升为 exit 3：worktree 形态的工作区（`ensureWorktree` 内的 `requireGit`）。目录工作区（`ensureDirectory`）与 `List` 都不再要求 git（[internal/workspace/workspace.go:73-77](file://internal/workspace/workspace.go#L73-L77)、[internal/workspace/workspace.go:318-330](file://internal/workspace/workspace.go#L318-L330)）。
- `run verify` / `run complete` 对非 Git 项目或目录工作区**不报错**：`CompletionCheck{Advanced:false, Skipped:true, Reason:"git completion check is not applicable"}`；`Skipped` 不是「已验证」（[internal/app/runverify.go:19-56](file://internal/app/runverify.go#L19-L56)）。
- `wire --check` 的 git 行在 git 缺失时是 `OK: true` + 能力说明——只读检查不因可选取能失败（[internal/app/wirecheck.go:49-54](file://internal/app/wirecheck.go#L49-L54)）。

### 新增命令面的退出码

- **`workloom setup`**（[internal/cli/setup.go:17-58](file://internal/cli/setup.go#L17-L58)）：未知 template → `Usagef` / exit 2；`init` / 起手工作流 / `wire` 失败 → 原样透传（多为 1 或 3）；`config check` 有问题 → `Invalidf(KindInvalid, problems, …)` / exit 4；`wire --check` 自检不过 → `Preconditionf("setup wrote state that wire --check still rejects: …")` / exit 3；doctor 有不可读文件 → `Invalidf(KindInvalid, nil, "unreadable managed files (%d)")` / exit 4（[internal/app/setup.go:43-198](file://internal/app/setup.go#L43-L198)）。蓝图未声明 / MCP 未注册**不是**错误：只在 `steps` 与 `next` 里报告。
- **`workloom mcp install`**（[internal/cli/mcp.go:54-113](file://internal/cli/mcp.go#L54-L113)）：`--scope` 非法 → `Usagef("unknown scope %q (expected user or project)")`；`--client` 非法 → `Usagef("unknown client %q (expected codex, claude or opencode)")`；`--apply` 与 `--dry-run` 同时给 → exit 2；单客户端配置不可解析（非严格 JSON / Codex 行内 `mcp_servers` 表）**不算致命**——该客户端记 `status="failed"` + detail 指向 `workloom wire --print-mcp`，其余客户端照常处理（[internal/app/mcpinstall.go:63-162](file://internal/app/mcpinstall.go#L63-L162)）。人类 mark：`installed`/`planned` → `v`，`skipped` → `=`，`not-detected` → `?`，`failed` → `x`（[internal/cli/mcp.go:118-127](file://internal/cli/mcp.go#L118-L127)）。
- 两条命令的成功/失败信封与既有约定一致：`--json` 单文档信封（`ok` + 数据，失败加 `error{code,kind,message}`，走 stderr 语义由 `render`/`exitWithCode` 承载）；**不**使用 `--jsonl`。
- **本批（#398）**：`workloom --help` 顶层命令表补回 `init` 与 `sync status` 两行（[internal/cli/cli.go:65](file://internal/cli/cli.go#L65)、[internal/cli/cli.go:83](file://internal/cli/cli.go#L83)）——两条命令一直可路由（`setup` 第一步就是 `init`，`sync status` 是 M8.1 只读接力判定），e4f1a9e 重写命令表时漏列；纯文案、无行为变更。本页引用的 `internal/cli/cli.go` 行号已按 +2 位移重算（`init` 之前不变，`init` 与 `sync status` 之间 +1，其后 +2）。
- **本批（v0.1.10）**：补丁版本发布（`--help` 顶层命令表修复，无行为变更）；本页口径不变，`source_commit` 跟进至 `21a9e71`。
