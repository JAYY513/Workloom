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
description: devsys 受管 YAML 文件的严格 schema：每文件白名单 + schema_version 闸门 + 行号定位 + Problems 错误结构 + 退出码 4 的语义边界。
generated: repowiki
generator: WikiProject
source_commit: 47620c7
---

# Schema 与错误契约

本卡描述 `workloom/internal/config` 在 M0.4 落地的**严格 schema 契约**：哪些字段是合法的、字段必须是什么类型、`schema_version` 不被支持时会发生什么、错误如何结构化传给调用方。**这是写命令与只读诊断命令共用的契约**，也是后续里程碑扩展白名单时的唯一参考。

## 契约的物理边界

| 名称 | 值 | 来源 |
|---|---|---|
| `SupportedSchemaVersion` | `1` | [internal/config/config.go:24](../../../internal/config/config.go#L24) |
| 受管文件清单 | `project.yaml`、`config.yaml`、`state/current.yaml`、`state/milestones.yaml` | [internal/config/config.go:27-37](../../../internal/config/config.go#L27-L37) |
| 解析器入口 | `config.Load(root)` / `config.Diagnose(root)` | [internal/config/config.go:108-113](../../../internal/config/config.go#L108-L113) |
| 错误结构 | `Problem{File, Line, Field, Reason}` 与 `Problems` | [internal/config/config.go:45-78](../../../internal/config/config.go#L45-L78) |

**当前没有任何其它文件受本契约约束**。其它受写命令操作的 `.devsys/` 子目录（`workitems/`、`specs/`、`runs/`、…）由后续里程碑（M1+）定义各自的领域契约。

## 白名单：每文件允许的字段

由 `fileSpec.fields` 静态定义（[internal/config/validate.go:37-51](../../../internal/config/validate.go#L37-L51)）。`schema_version` 始终是第一个、必填、必须是整数。

### `project.yaml`

| 字段 | 类型 | 必填？ | 备注 |
|---|---|---|---|
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |
| `id` | `string` | 是 | 来自目录名的 slug，见 `project.Slug` |
| `name` | `string` | 是 | 仓库根目录名 |
| `created_at` | `RFC3339` 时间戳（`!!str` 或 `!!timestamp`） | 否 | 缺省时由 `Load` 容忍，运行时输出 `current.yaml` 等位置时为 0 行 |

`init` 写出的 `project.yaml` 由 `projectFile` struct 序列化（[internal/project/tree.go:55-60、79-86](../../../internal/project/tree.go#L55-L60)），键序 = struct 字段序。

### `config.yaml`

| 字段 | 类型 | 必填？ | 备注 |
|---|---|---|---|
| `schema_version` | `int` | 是 | 必须等于 `SupportedSchemaVersion` |

业务键未定义：写文件目前**仅接受 `schema_version`**，任何其它键会被拒绝（[internal/config/validate.go:111-115](../../../internal/config/validate.go#L111-L115)）。后续里程碑扩字段时必须同步更新这里的白名单、`config.Config` struct、以及 `project.configFile` 占位（[internal/project/tree.go:62-64](../../../internal/project/tree.go#L62-L64)）。

### `state/current.yaml` 与 `state/milestones.yaml`

这两个文件用 `stateSpec`（[internal/config/validate.go:48-51](../../../internal/config/validate.go#L48-L51)）：

- 必填 `schema_version`（`int`）。
- `allowExtra: true`：除 `schema_version` 外的键不视为未知键，由 M1.1 的领域类型负责校验。
- `Load` 对这两个文件只调用 `checkStateFile`（[internal/config/config.go:184-187](../../../internal/config/config.go#L184-L187)），不做完整解析。

> **结果**：M0.4 时 `project.yaml` 与 `config.yaml` 是严格 schema；state 文件**只**校验 schema_version 与顶层必须是 mapping，**不**拒绝未知的 `summary` / `risks` 等键。这是有意为之，避免 schema 校验把 M1.1 的领域变更锁死。

## 字段类型：`fieldKind`

三档位（[internal/config/validate.go:17-21](../../../internal/config/validate.go#L17-L21)）：

| Kind | YAML 形态 | 判定 |
|---|---|---|
| `kindInt` | `!!int` 标量 | `yaml.Node.Decode(&int)` 成功 |
| `kindString` | `!!str` 标量；非空（当 `required`） | `Kind==ScalarNode && Tag=="!!str"` |
| `kindTimestamp` | `!!str` 或 `!!timestamp`，必须能 `time.Parse(time.RFC3339, ...)` | 见 [internal/config/validate.go:150-158](../../../internal/config/validate.go#L150-L158) |

任何不匹配的键值在 `checkField` 里返回 `Problem{Line: val.Line, Field: name, Reason: "expected <kind>, got <nodeKind>"}`（[internal/config/validate.go:134-161](../../../internal/config/validate.go#L134-L161)）。

## `schema_version` 闸门

`validate` 的第一步是检查 `schema_version`（[internal/config/validate.go:76-92](../../../internal/config/validate.go#L76-L92)）。它**早于**字段白名单检查，因此一份未知版本的受管文件**只**会产生一条 `unsupported version N` 问题，不会被后续“未知键 / 类型错误”的噪声淹没（[internal/config/validate.go:62-66 注释](../../../internal/config/validate.go#L62-L66)）。

触发的拒绝文本（[internal/config/validate.go:88-92](../../../internal/config/validate.go#L88-L92)）：

```text
unsupported version N (this build supports M); refusing to write, migration must be explicit (实施计划 M9.2)
```

写命令（`init`）因为拿到这条 `Problems` 会返回 `CodeInvalid`；只读诊断（`config check`）同样返回 `CodeInvalid` 但仍继续（[internal/cli/cli.go:271-273](../../../internal/cli/cli.go#L271-L273)）。

## 行号定位

每个 `Problem` 都带 `Line`（1-based，0 表示“整文件层级的问题”）。`yaml.Node.Line` 由 `gopkg.in/yaml.v3` 在解码时填入。覆盖以下情形：

- 顶层不是 mapping：根节点的 `Line`。
- `schema_version` 缺失：根节点 `Line`，`Field="schema_version"`。
- 重复键：第二次出现的 `Line`。
- 类型错误：值节点的 `Line`。
- 空文件 / 多文档 / 解析错误：整文件（`Line=0`）。

`Problem.String()` 渲染为 `file[:line][: field]: reason`（[internal/config/config.go:52-66](../../../internal/config/config.go#L52-L66)），与 [internal/cli/cli_test.go:218-226](../../../internal/cli/cli_test.go#L218-L226) 断言的输出严格一致：

```text
project.yaml:3: id: expected string, got !!int
project.yaml:4: updated_at: unknown key
project.yaml:2: name: required
```

## 退出码契约

`CodeInvalid = 4`（[internal/cli/cli.go:29-32](../../../internal/cli/cli.go#L29-L32)）的语义在 `usage` 文本里写明（[internal/cli/cli.go:53-55](../../../internal/cli/cli.go#L53-L55)）：

```text
4  invalid managed state (parse, field or schema_version problems)
```

它**只**由 `errInvalid(config.Problems)` 触发（[internal/cli/cli.go:87-98](../../../internal/cli/cli.go#L87-L98)）。写命令在拿到 `Problems` 时返回 `CodeInvalid` 并**不**修改磁盘（被 `TestInitRefusesUnknownSchemaVersion` 守护，见 [internal/cli/cli_test.go:325-331](../../../internal/cli/cli_test.go#L325-L331)）。

## JSON 错误结构（`--json`）

`render` 在 `--json` 时输出（[internal/cli/cli.go:152-166](../../../internal/cli/cli.go#L152-L166)）：

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

## 与存储层的边界

本契约**只**诊断“受管元数据文件”（`project.yaml`、`config.yaml`、`state/*.yaml`）。它**不**诊断：

- 项目级写锁或事务日志（属于 `internal/storage`）。
- 工作项 / 知识 / 事件等领域文件（M1+ 引入各自的领域契约）。
- 用户级注册表 `registry.yaml`（它有自己的解析器，见 [internal/registry/registry.go:84-106](../../../internal/registry/registry.go#L84-L106)，不在 schema_version 闸门管辖内）。

“配置诊断”与“存储恢复”刻意分开：前者是 M0.4 的只读工具，后者是 M0.3 的写路径守护者。**不要**用 `config check` 去尝试恢复锁状态或回放事务日志——这两层互不调用。

## 演进规则（后续里程碑扩展白名单时）

每加一个新键到任一受管文件，**至少**同步以下三处：

1. `internal/config/validate.go` 的对应 `fileSpec`（新增字段 + 类型 + 必填）。
2. `internal/config/config.go` 的对应 `Project` / `Config` struct（让 `Load` 能解析）。
3. `internal/project/tree.go` 的对应占位 struct + builder（让 `init` 写出的字节仍然合法）。

写命令前置校验 `config.Load` 会把第三处漏改的字段立刻暴露为“未知键”，从而保证占位文件与白名单**必须**同步。
