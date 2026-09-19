---
status: stable
type: module
dimension: exchange_protocol
triggers:
  - --jsonl
  - --json
  - writeJSONL
  - 一行一条
  - exit code
  - 输出约定
  - 10/11
  - knowledge status
  - 输出格式
description: M4 CLI 的 `--json` / `--jsonl` 两种结构化输出形态、`writeJSONL` 列表行流、退出码与 MCP 工具错误的对应；知识层保留的 10/11 约定；M7.1 增 `--json` 信封把 `view.Model` 嵌入 `data`（view.Model 字段：schema_version/project/trust/baseline/progress/runs/records/knowledge/sources/problems），人类输出走「一行一事实」+ 缩进层级（project/phase/baseline/state/milestones/progress/readiness/risk/next/runs/records/knowledge/trust/pending/problem/sources），退出码 0/1/2/3 沿用、10/11 仍只承载 knowledge status。
generated: true
source_commit: 463c2d8
generator: repowiki-gen
---

# CLI 渲染与交换协议 · 项目接入与配置

M4 把 CLI 改成「薄渲染层」：flag 解析 → `app.New(root)` → `svc.Xxx(...)` → `render`。所有结构化输出都按 `--json`（单文档信封）或 `--jsonl`（一行一条记录）两种形态统一约定，外部脚本按 exit code 或 `code` 字段分支。

## 全局开关

`internal/cli/cli.go` 顶部：

```go
const usage = `devsys - project-local agent development infrastructure

usage:
  devsys [--json | --jsonl] [--quiet] <command>
  ...
`
```

- `--json`：成功输出 `{"ok":true,...}` 单文档；错误输出 `{"ok":false,"error":{...}}` 到 stderr。
- `--jsonl`：列表类命令按「一行一条 JSON 记录」输出，字段名与 `--json` 信封内的同名数组一致。
- `--quiet`：抑制成功输出；错误/warning 仍然走 stderr。
- 全局开关必须**前置**（解析器遇首个非选项参数即停）。

## `writeJSONL[T]` 泛型

`internal/cli/cli.go:124-133`：

```go
func writeJSONL[T any](w io.Writer, items []T) error {
    enc := json.NewEncoder(w)
    for _, item := range items {
        if err := enc.Encode(item); err != nil {
            return errInternal("write jsonl: %v", err)
        }
    }
    return nil
}
```

要点：

- 逐行 encode，没有外层信封；每条记录**字段名**与 `--json` 模式信封内的同名数组字段一致（`workitem_list --jsonl` 输出每行一个 `WorkItem` JSON 对象；`event_list --jsonl` 每行一个 `Event`）。
- 写入失败 → `errInternal`，exit 1。
- 空列表渲染为「零行」（不是空文档）——脚本按行计数时不会遇到「空行 + EOF」的歧义。

## `--jsonl` 已覆盖列表命令

`scripts/exchange-demo.sh` 与 `internal/cli/exchange_test.go:17-78` 验证：所有 `list` 子命令都支持 `--jsonl`：

| 命令族 | `--jsonl` 字段（每行） |
|---|---|
| `workitem list` | `WorkItem` |
| `decision list` | `Decision` |
| `finding list` | `Finding` |
| `event list --type comment` | `Event` |
| `artifact list` | `Artifact` |
| `run list` | `Run` |
| `approval list` | `Approval` |
| `workflow list` | `PolicySummary` |

详情命令（`get` / `create` / `update` 等）输出**单条记录**——只支持 `--json`，不支持 `--jsonl`。

## 退出码与知识层约定

`internal/cli/cli.go:31-44`：

```go
const (
    CodeOK           = 0
    CodeInternal     = 1
    CodeUsage        = 2
    CodePrecondition = 3
    CodeInvalid      = 4
)
// 知识层预留 10/11（方案 §12.5），M5 到达：0 fresh / 10 stale / 11 missing
```

CLI 与 MCP 共享同一份 `*app.Error.Class()`（usage / precondition / internal / invalid），分别映射到 CLI 的 `2 / 3 / 1 / 4` 退出码与 MCP 的同名 tool error code。

## `--json` 错误信封

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

- `problems` 仅当 `kind` 为结构化失败（`invalid` / `workflow` 等含 `Problems`）时存在；其它情况为 `null` 或省略。
- `kind` 字段是 `ae.Kind`，可能是 `usage / precondition / internal / invalid / workflow / gate / quality / workitem / approval` 之一。

## 列表命令的人类可读形态（无 `--json`）

| 命令 | 文本格式 |
|---|---|
| `workitem list` | `<id>\t<status>\t<title>`（按存储序） |
| `decision list` | `<id>\t<status>\t<title>` |
| `finding list` | `<id>\t<status>\t<title>` |
| `event list` | `<id>\t<type>\t<subject>\t<actor>\t<time>` |
| `artifact list` | `<id>\t<name>\t<version>` |
| `run list` | `<id>\t<status>\t<workitem>` |
| `approval list` | `<id>\t<workitem>\t<stage>\t<status>` |
| `search <kw>` | `<rel-path>:<line>: <trimmed text>`，末行 `<N> match(es)` |

## `devsys workspace view` 的协议（M7.1）

M7.1 在 `--json` 单文档信封之上新增**视图域**形态，专用、不复用 `--jsonl`（视图不是单条记录，是一个多节快照）。

`--json` 信封（成功）：

```json
{
  "ok": true,
  "schema_version": 1,
  "project": { "id": "demo", "name": "demo", "status": "active", "degraded": false, "reason": "", "sources": ["project.yaml"] },
  "trust": { "state": "ok", "inspection_ok": true, "note": "", "pending": [] },
  "baseline": { "available": true, "commit": "abc…", "branch": "main", "dirty": false, "changed_files": 0 },
  "progress": { "counts": {"ready": 2, "in_progress": 1}, "items": [...], "readiness": {...}, "sources": ["workitems/"] },
  "runs":     { "entries": [...], "truncated": false, "sources": ["runs/"] },
  "records":  { "decisions": [...], "findings": [...], "artifacts": [...], "truncated": false },
  "knowledge":{ "status": "fresh", "pages": [...], "truncated": false, "root": "docs", "baseline": "abc…", "generator": "", "reason": "" },
  "sources":  [".devsys/project.yaml", ".devsys/workitems/", ".devsys/runs/", "..."],
  "problems": []
}
```

- 形态：`json.NewEncoder(stdout).Encode(struct{OK bool; view.Model})`（[internal/cli/workspace.go:58-63](../../../internal/cli/workspace.go#L58-L63)）——`OK: true` 与 `view.Model` 字段在 JSON 顶层并列；M4 的 `{ok: true, data}` 写法在此被打破，因为视图本身就是数据。
- 字段顺序：与 `view.Model` struct 字段序一致（[internal/view/view.go:65-80](../../../internal/view/view.go#L65-L80)），保证序列化稳定。
- 错误：`--json` 模式下错误仍走 M4 错误信封 `{ok: false, error: {code, kind, message, problems?}}`（`render` 函数统一处理）。

人类输出（无 `--json` / 无 `--quiet`）由 `renderWorkspaceView`（[internal/cli/workspace.go:72-135](../../../internal/cli/workspace.go#L72-L135)）生成，**一行一事实 + 缩进层级**：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `project: <name> (<id>) — <status>` | 恒有 | 顶层 |
| `  phase: <current_phase>` | `current_phase != ""` | 二级 |
| `  baseline: <short_sha> (<branch>)  dirty: yes/no/unknown [— <reason>]` | `baseline.Available` | 二级 |
| `  baseline: unavailable — <reason>` | `!baseline.Available` | 二级 |
| `  state: <summary>` | `p.Summary != ""` | 二级 |
| `  milestone: <id> <status>` | 每个 milestone | 二级 |
| `  progress: <N> item(s) — <status counts>` | 恒有 | 二级 |
| `  readiness: <verdict>` | 恒有 | 二级 |
| `    risk: <kind> [<workitem_id>]: <detail>` | 每个 risk | 三级 |
| `    next: <action> [<workitem_id>] — <reason>` | 恒有 | 三级 |
| `  runs: <N> — <status counts>[(truncated; raise --limit for more)]` | 恒有 | 二级 |
| `  records: decisions <N>  findings <N>  artifacts <N>[…]` | 恒有 | 二级 |
| `  knowledge: missing — <reason>` | `k.Status == KnowledgeMissing` | 二级 |
| `  knowledge: <status> — <N> page(s)[…][— <reason>]` | 其它 | 二级 |
| `  trust: <state> — <note>` | `m.Trust.State != TrustOK` | 二级 |
| `  pending: <id1>, <id2>, …` | `len(m.Trust.Pending) > 0` | 二级 |
| `  problem: <msg>` | 每个 `m.Problems` 项 | 二级 |
| `  sources: <rel path>, …` | 恒有 | 二级 |

`--quiet` 抑制整段；`-q` / `--json` 互斥（已知 `--json` 优先）。

退出码（沿用既有体系）：成功 0 / 子命令缺失或 `--limit <= 0` → 2 / `storage.ErrNotInitialized` → 3 / `view.Build` 其它失败 → 1。**`advisory_unlocked` / `pending_transaction` 都是 exit 0**——`trust` 与 `pending` 字段在 JSON / 人类输出里告诉调用方状态不可信，而不是用退出码隐藏语义。10/11 仍专属 `devsys knowledge status`，视图层把 `knowledge.status` 用字符串承载（`KnowledgeFresh / Stale / Missing / Unavailable`），不与退出码耦合。

## 与其他层的关系

- [架构设计](./架构设计.md) — 命令分发 + `render` + `toCoded`
- [共享应用与 MCP 错误语义](../共享应用与MCP/错误语义.md) — `*app.Error.Class()` 是双入口错误翻译的源头
- [视图层 · 概述](../视图层/概述.md) — M7.1 `view.Model` 字段序与 Provenance / Trust / Knowledge 状态的完整定义
