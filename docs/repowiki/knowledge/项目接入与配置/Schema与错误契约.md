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
description: devsys 受管 YAML 文件的严格 schema：每文件白名单 + schema_version 闸门 + 行号定位 + Problems 错误结构 + 退出码 4 的语义边界 + M1 完整 Project 模型 + 嵌套字段校验；M2 新增退出码 3/4 在 workitem 与 repair 中的扩展语义、--expect 64-hex sha256 + fail-closed、--confirm 摘要格式、写命令 --actor/--reason 必填。
generated: true
source_commit: 85b0de7
generator: repowiki-gen
---

# Schema 与错误契约

本卡描述 `workloom/internal/config` 落地的**严格 schema 契约**以及 M1 起 `workloom/internal/domain` 引入的统一记录契约：哪些字段是合法的、字段必须是什么类型、`schema_version` 不被支持时会发生什么、错误如何结构化传给调用方。**这是写命令与只读诊断命令共用的契约**，也是后续里程碑扩展白名单时的唯一参考。

## 契约的物理边界

| 名称 | 值 | 来源 |
|---|---|---|
| `config.SupportedSchemaVersion` | `1` | [internal/config/config.go:25](../../../internal/config/config.go#L25) |
| `domain.SchemaVersion` | `1` | [internal/domain/models.go:6](../../../internal/domain/models.go#L6) |
| 受管文件清单 | `project.yaml`、`config.yaml`、`state/current.yaml`、`state/milestones.yaml` | [internal/config/config.go:28-37](../../../internal/config/config.go#L28-L37) |
| 解析器入口 | `config.Load(root)` / `config.Diagnose(root)` | [internal/config/config.go:99-104](../../../internal/config/config.go#L99-L104) |
| 错误结构 | `Problem{File, Line, Field, Reason}` 与 `Problems` | [internal/config/config.go:46-79](../../../internal/config/config.go#L46-L79) |
| 记录契约 | `domain.{Project,Scope,Milestone,CurrentState,CurrentStateFile,MilestonesFile,WorkItem,Run,…}` | [internal/domain/models.go:8-221](../../../internal/domain/models.go#L8-L221) |

`config.SupportedSchemaVersion` 与 `domain.SchemaVersion` 必须**始终相等**：`domain.EncodeYAML` / `DecodeYAML` 在序列化末尾调用 `storage.CheckSchemaVersion(b, SchemaVersion)`（[internal/domain/serialize.go:26-28](../../../internal/domain/serialize.go#L26-L28)）作为最后一道闸门。

**当前没有任何其它文件受本契约约束**。其它受写命令操作的 `.devsys/` 子目录（`workitems/`、`specs/`、`runs/`、…）由后续里程碑定义各自的领域契约；但它们的字段形状最终都汇聚到 `internal/domain` 的对应 struct。

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
|---|---|---|---|
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |

业务键未定义：写文件目前**仅接受 `schema_version`**，任何其它键会被拒绝（[internal/config/validate.go:133-137](../../../internal/config/validate.go#L133-L137)）。后续里程碑扩字段时必须同步更新这里的白名单、`config.Config` struct、以及 `project.configFile` 占位（[internal/project/tree.go:68-71](../../../internal/project/tree.go#L68-L71)）。

### `state/current.yaml` 与 `state/milestones.yaml`

两个 state spec 在 M1 起被显式收紧（[internal/config/validate.go:63-73](../../../internal/config/validate.go#L63-L73)）：

- `state/current.yaml` 由 `currentSpec` 校验：`schema_version`（必填 `int`）+ `summary`（`string`）+ `risks` / `blockers` / `next_focus`（皆 `string[]` 或 `null`）。
- `state/milestones.yaml` 由 `milestonesSpec` 校验：`schema_version`（必填 `int`）+ `milestones`（嵌套序列，每元素必填 `id/name/status`）。
- 两者仍**没有**全局 `allowExtra`：除白名单外的键会直接被拒绝（`validate` 的统一分支，[internal/config/validate.go:133-137](../../../internal/config/validate.go#L133-L137)）。
- `Load` 对这两个文件只调用 `checkStateFile`（[internal/config/config.go:167-174](../../../internal/config/config.go#L167-L174)），不做完整领域解析。

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

`Problem.String()` 渲染为 `file[:line][: field]: reason`（[internal/config/config.go:54-67](../../../internal/config/config.go#L54-L67)），与 [internal/cli/cli_test.go:218-226](../../../internal/cli/cli_test.go#L218-L226) 断言的输出严格一致：

```text
project.yaml:3: id: expected string, got !!int
project.yaml:4: updated_at: unknown key
project.yaml:2: name: required
project.yaml:5: milestones[0].status: required
```

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

它**只**由 `errInvalid(config.Problems)` 触发（[internal/cli/cli.go:103-114](../../../internal/cli/cli.go#L103-L114)）。写命令在拿到 `Problems` 时返回 `CodeInvalid` 并**不**修改磁盘（被 `TestInitRefusesUnknownSchemaVersion` 守护，见 [internal/cli/cli_test.go:282-331](../../../internal/cli/cli_test.go#L282-L331)）。

M2 起 `CodeInvalid = 4` 还覆盖 workitem / reconcile 写路径下的领域错误，由 `workitemError` 把非 `storage.ErrNotInitialized` / `workitem.ErrNotFound` 的领域错误升为带 `kind="workitem"` 的 `codedError{Code: CodeInvalid}`（[internal/cli/cli.go:477-482](../../../internal/cli/cli.go#L477-L482)）。`code=4` 在 `--json` 载荷里与配置错误走同一字段，调用脚本可统一按 `code` 分支。

### `CodePrecondition = 3`：前提不满足

原本只承担"环境侧错误"（非 git 仓库、git 不在 PATH、CWD 不是仓库根）。M2 在同一退出码下聚合两类新用法（[internal/cli/cli.go:70](../../../internal/cli/cli.go#L70)）：

| 触发点 | 来源 | 错误提示 |
|---|---|---|
| 非 git 仓库 / git 不在 PATH / CWD 不在仓库根 | `project.PreconditionError` → `codedError{Code: CodePrecondition}` | `not a git repository` 等 |
| `.devsys/` 不存在（`search` / `workitem get` / `workitem create` 等） | `storage.ErrNotInitialized` / `workitem.ErrNotFound` → `workitemError` → `errPrecondition` | `project not initialized; run devsys init` 等 |
| workitem `--expect` 哈希与现状不符 | `expectedSnapshot` 返回 `version mismatch` → `workitemError` → `errPrecondition` | `version mismatch: work item changed since your read; rerun workitem get` |
| `repair --apply` 收到的 `--confirm` 与重跑摘要不符 | `reconcile.ErrDigestMismatch` → `errPrecondition` | `confirmation digest does not match current state; run `devsys repair --dry-run` again` |

第 3、4 行即方案 §15.4 "推断→确认→重写" 闭环在退出码层面的体现：调用方必须重新读证据 / 重新干跑，才能继续动盘。脚本可以**只信 `code == 3` 重新发起一次 `get` / `dry-run`**。

`devsys search <keyword>` 路径**不**涉及 `config.Load` / `Diagnose`，因此**不**返回 `CodeInvalid`；它的失败模式是 `CodeInternal`（搜索失败）或 `CodePrecondition`（缺 `.devsys/`）。

## `--expect`：64-hex sha256 + fail-closed

`workitem transition` 与 `workitem claim` 的 `--expect` 是调用方持有的"工作项快照版本哈希"，由 `workitem get` 在 `--json` 模式下的 `version` 字段给出（[internal/cli/cli.go:381-389](../../../internal/cli/cli.go#L381-L389)）。

格式与解析（[internal/cli/cli.go:493-506](../../../internal/cli/cli.go#L493-506)）：

- 编码：小写 64 字符十六进制串，等价于 `fmt.Sprintf("%x", storage.HashBytes(raw))`，即 sha256 的字节级表示。
- 解析：`strings.TrimSpace` 后用 `hex.DecodeString` 解码；解码结果长度必须等于 `crypto/sha256.Size`（32 字节）；解码失败、长度错误、哈希不匹配**全部**视为哈希不匹配，统一返回 `version mismatch` 错误。
- fail-closed：`expectedSnapshot` 在哈希不匹配时**不**返回 `raw` 字节；底层 `Transition` / `Claim` 拿不到 `Expected` 入参，写路径被彻底拦截。
- 退出码：不匹配 → `CodePrecondition = 3`，错误提示 `version mismatch: work item changed since your read; rerun workitem get`。

调用契约：

```sh
VERSION=$(bin/devsys.exe --json workitem get WLM-0001 | jq -r .version)
bin/devsys.exe workitem transition --id WLM-0001 --to in_progress \
    --actor alice --reason "M2 kickoff" --expect "$VERSION"
```

## `--confirm`：确定性 digest + 摘要回放

`devsys repair --apply --confirm <digest>` 的 `--confirm` 是 `repair --dry-run` 在 `--json` 模式下的 `plan.digest` 字段（[internal/cli/cli.go:659-664](../../../internal/cli/cli.go#L659-L664)）。

格式与判定（[internal/cli/cli.go:614-635](../../../internal/cli/cli.go#L614-L635) + [internal/reconcile/reconcile.go:319-335](../../../internal/reconcile/reconcile.go#L319-335)）：

- 编码：`reconcile.ComputeDigest(plan.Proposals)` 输出小写十六进制摘要；墙钟时间**不**参与计算，相同证据输入产生相同摘要。
- 判定：`--apply` 内部**先**重跑 `reconcile.RepairDryRun` 取得当前 `Digest`，**再**用调用方的 `--confirm` 覆盖 `plan.Digest`，**再**调 `reconcile.RepairApply`；摘要不一致由 `reconcile.ErrDigestMismatch` 上浮为 `CodePrecondition = 3`，并提示 `run `devsys repair --dry-run` again`。
- 空摘要：`--apply` 在缺 `--confirm` 时直接返回 `CodeUsage = 2`，**不**进入 reconcile（[internal/cli/cli.go:614-617](../../../internal/cli/cli.go#L614-L617)）。

调用契约：

```sh
DIGEST=$(bin/devsys.exe --json repair --dry-run --actor alice --reason "M2 audit" | jq -r .plan.digest)
bin/devsys.exe repair --apply --confirm "$DIGEST" --actor alice --reason "M2 audit"
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

`kind` 取值与 `codedError.kind` 一致：`"usage"` / `"precondition"` / `"invalid"` / `"internal"`。M2 起 workitem 领域错误的 `kind="workitem"`，code 与 `CodeInvalid = 4` 一致。**调用脚本应当只信 `code` 字段做分支**，不要解析 `message`。

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

`devsys --json doctor` 与 `devsys --json repair --dry-run` 分别输出 `InspectionReport` 与 `Plan`（[internal/cli/cli.go:523-525](../../../internal/cli/cli.go#L523-L525)、[internal/cli/cli.go:659-664](../../../internal/cli/cli.go#L659-L664)）。

## 与存储层的边界

本契约**只**诊断"受管元数据文件"（`project.yaml`、`config.yaml`、`state/*.yaml`）。它**不**诊断：

- 项目级写锁或事务日志（属于 `internal/storage`）。
- 工作项 / 知识 / 事件等领域文件（M1+ 在各自 package 内自行校验，落盘前必须通过对应 `domain.DecodeYAML`，未通过时由 `storage.CheckSchemaVersion` 拒绝）。
- 用户级注册表 `registry.yaml`（它有自己的解析器，见 [internal/registry/registry.go:84-106](../../../internal/registry/registry.go#L84-L106)，不在 schema_version 闸门管辖内）。
- 租约文件（`.devsys/scheduling/<id>.yaml`，M2 新增；其 schema 由 `internal/workitem` 自管理，**不**走 `config.Load`；doctor / recover / repair 走 `internal/reconcile` 通过 `storage.Inspect` 检查）。

"配置诊断"与"存储恢复"刻意分开：前者是 M0.4 的只读工具，后者是 M0.3 的写路径守护者。**不要**用 `config check` 去尝试恢复锁状态或回放事务日志——这两层互不调用。

## 演进规则（后续里程碑扩展白名单时）

每加一个新键到任一受管文件，**至少**同步以下三处：

1. `internal/config/validate.go` 的对应 `fileSpec`（新增字段 + 类型 + 必填；嵌套结构同步加 `fieldKind` 与 `checkNested` 分支）。
2. `internal/config/config.go` 的对应 `Project` / `Config` struct（让 `Load` 能解析；嵌套字段对应到 `domain.*` 类型）。
3. `internal/project/tree.go` 的对应占位 struct + builder（让 `init` 写出的字节仍然合法；M1 起优先用 `domain.EncodeYAML` 序列化）。

写命令前置校验 `config.Load` 会把第三处漏改的字段立刻暴露为"未知键"，从而保证占位文件与白名单**必须**同步。`domain.DecodeYAML` 在解码时若发现 `schema_version` 不等于 `domain.SchemaVersion` 会立即拒绝（[internal/domain/serialize.go:73-78](../../../internal/domain/serialize.go#L73-L78)），保证 struct 与白名单永远在同一个版本号上。