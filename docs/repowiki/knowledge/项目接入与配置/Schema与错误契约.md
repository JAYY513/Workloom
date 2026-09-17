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
description: devsys 受管 YAML 文件的严格 schema：每文件白名单 + schema_version 闸门 + 行号定位 + Problems 错误结构 + 退出码 4 的语义边界 + M1 完整 Project 模型 + 嵌套字段校验。
generated: true
source_commit: 2735f62
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
| `kindStrings` | `!!null`（保留“未记录”态）或 `!!seq` 元素皆为 `!!str` | 见 [internal/config/validate.go:160-163](../../../internal/config/validate.go#L160-L163) |
| `kindScope` | `!!map`，键仅 `in` / `out`，值皆 `kindStrings` | 见 [internal/config/validate.go:164-167](../../../internal/config/validate.go#L164-L167) + [internal/config/nested.go:23-24](../../../internal/config/nested.go#L23-L24) |
| `kindCurrent` | `!!map`，子键集合 = `summary/risks/blockers/next_focus` | 见 [internal/config/nested.go:25-26](../../../internal/config/nested.go#L25-L26) |
| `kindMilestones` | `!!seq`，每元素 mapping 必填 `id/name/status`（皆 `kindString`） | 见 [internal/config/nested.go:18-22](../../../internal/config/nested.go#L18-L22) |

任何不匹配的键值在 `checkField` 里返回 `Problem{Line: val.Line, Field: name, Reason: "expected <kind>, got <nodeKind>"}`（[internal/config/validate.go:157-193](../../../internal/config/validate.go#L157-L193)）；嵌套失败则通过 `checkNested` / `checkMapping` 沿 `field[index]` / `field.child` 形式把路径展开（[internal/config/nested.go:8-68](../../../internal/config/nested.go#L8-L68)）。

## `schema_version` 闸门

`validate` 的第一步是检查 `schema_version`（[internal/config/validate.go:98-114](../../../internal/config/validate.go#L98-L114)）。它**早于**字段白名单检查，因此一份未知版本的受管文件**只**会产生一条 `unsupported version N` 问题，不会被后续“未知键 / 类型错误”的噪声淹没。

触发的拒绝文本（[internal/config/validate.go:110-113](../../../internal/config/validate.go#L110-L113)）：

```text
unsupported version N (this build supports M); refusing to write, migration must be explicit (实施计划 M9.2)
```

写命令（`init`）因为拿到这条 `Problems` 会返回 `CodeInvalid`；只读诊断（`config check`）同样返回 `CodeInvalid` 但仍继续（[internal/cli/cli.go:314-317](../../../internal/cli/cli.go#L314-L317)）。

`domain.EncodeYAML` / `DecodeYAML` 在写出 / 读入时再次调用 `storage.CheckSchemaVersion`（[internal/domain/serialize.go:26-28、73-78](../../../internal/domain/serialize.go#L26-L28)），保证领域记录的 schema_version 与配置层**始终一致**。

## 行号定位

每个 `Problem` 都带 `Line`（1-based，0 表示“整文件层级的问题”）。`yaml.Node.Line` 由 `gopkg.in/yaml.v3` 在解码时填入。覆盖以下情形：

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

`CodeInvalid = 4`（[internal/cli/cli.go:27-34](../../../internal/cli/cli.go#L27-L34)）的语义在 `usage` 文本里写明（[internal/cli/cli.go:52-57](../../../internal/cli/cli.go#L52-L57)）：

```text
4  invalid managed state (parse, field or schema_version problems)
```

它**只**由 `errInvalid(config.Problems)` 触发（[internal/cli/cli.go:89-100](../../../internal/cli/cli.go#L89-L100)）。写命令在拿到 `Problems` 时返回 `CodeInvalid` 并**不**修改磁盘（被 `TestInitRefusesUnknownSchemaVersion` 守护，见 [internal/cli/cli_test.go:282-331](../../../internal/cli/cli_test.go#L282-L331)）。

`devsys search <keyword>` 路径**不**涉及 `config.Load` / `Diagnose`，因此**不**返回 `CodeInvalid`；它的失败模式是 `CodeInternal`（搜索失败）或 `CodePrecondition`（缺 `.devsys/`）。

## JSON 错误结构（`--json`）

`render` 在 `--json` 时输出（[internal/cli/cli.go:156-170](../../../internal/cli/cli.go#L156-L170)）：

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

`kind` 取值与 `codedError.kind` 一致：`"usage"` / `"precondition"` / `"invalid"` / `"internal"`。**调用脚本应当只信 `code` 字段做分支**，不要解析 `message`。

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

## 与存储层的边界

本契约**只**诊断“受管元数据文件”（`project.yaml`、`config.yaml`、`state/*.yaml`）。它**不**诊断：

- 项目级写锁或事务日志（属于 `internal/storage`）。
- 工作项 / 知识 / 事件等领域文件（M1+ 在各自 package 内自行校验，落盘前必须通过对应 `domain.DecodeYAML`，未通过时由 `storage.CheckSchemaVersion` 拒绝）。
- 用户级注册表 `registry.yaml`（它有自己的解析器，见 [internal/registry/registry.go:84-106](../../../internal/registry/registry.go#L84-L106)，不在 schema_version 闸门管辖内）。

“配置诊断”与“存储恢复”刻意分开：前者是 M0.4 的只读工具，后者是 M0.3 的写路径守护者。**不要**用 `config check` 去尝试恢复锁状态或回放事务日志——这两层互不调用。

## 演进规则（后续里程碑扩展白名单时）

每加一个新键到任一受管文件，**至少**同步以下三处：

1. `internal/config/validate.go` 的对应 `fileSpec`（新增字段 + 类型 + 必填；嵌套结构同步加 `fieldKind` 与 `checkNested` 分支）。
2. `internal/config/config.go` 的对应 `Project` / `Config` struct（让 `Load` 能解析；嵌套字段对应到 `domain.*` 类型）。
3. `internal/project/tree.go` 的对应占位 struct + builder（让 `init` 写出的字节仍然合法；M1 起优先用 `domain.EncodeYAML` 序列化）。

写命令前置校验 `config.Load` 会把第三处漏改的字段立刻暴露为“未知键”，从而保证占位文件与白名单**必须**同步。`domain.DecodeYAML` 在解码时若发现 `schema_version` 不等于 `domain.SchemaVersion` 会立即拒绝（[internal/domain/serialize.go:73-78](../../../internal/domain/serialize.go#L73-L78)），保证 struct 与白名单永远在同一个版本号上。