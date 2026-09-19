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
description: M4 CLI 的 `--json` / `--jsonl` 两种结构化输出形态与退出码/MCP 错误对应；M7.1–M7.4 workspace 子命令族 view/build/serve（信封差异 + hint 行），人类输出一行一事实；M8 sync status / repair conflict notes / archive events|runs / dispatch merge gate 的 `--json` 信封与人类输出约定
generated: true
source_commit: 52294b0
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

## `devsys workspace view/build/serve` 的协议（M7.1–M7.4）

M7.1 起 workspace 子命令族扩成三种形态：view（M7.1）打印聚合视图、build（M7.2）落地离线静态站、serve（M7.3）暴露本地只读 HTTP。它们共享同一份 `view.Model`，但每一种都重新组装自己的外层信封——`--jsonl` 不复用，因为视图与站点清单都不是单条记录。

### view（`--json`）—— `view.Model` 平铺信封

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

- 形态：`json.NewEncoder(stdout).Encode(struct{OK bool; view.Model})`（[internal/cli/workspace.go:135-140](../../../internal/cli/workspace.go#L135-L140)）——`OK: true` 与 `view.Model` 字段在 JSON 顶层并列；M4 的 `{ok: true, data}` 写法在此被打破，因为视图本身就是数据。
- 字段顺序：与 `view.Model` struct 字段序一致（[internal/view/view.go:65-80](../../../internal/view/view.go#L65-L80)），保证序列化稳定。
- 错误：`--json` 模式下错误仍走 M4 错误信封 `{ok: false, error: {code, kind, message, problems?}}`（`render` 函数统一处理）。

### build（`--json`）—— 站点清单信封

`workspace build --static`（M7.2）走自己的外层信封，承载"产物路径 + 页列表 + 时间戳"，不再回退到 `view.Model`：

```json
{
  "ok": true,
  "out": ".devsys/dist/site",
  "pages": ["index.html", "tasks.html", "workflows.html", "runs.html", "records.html", "knowledge.html"],
  "generated_at": "2026-09-19T12:34:56Z",
  "baseline": { "available": true, "commit": "abc…", "branch": "main", "dirty": false, "changed_files": 0 },
  "trust_state": "ok"
}
```

- 形态：`json.NewEncoder(stdout).Encode(struct{OK bool; Out string; Pages []string; GeneratedAt string; Baseline view.Baseline; TrustState string})`（[internal/cli/workspace.go:84-91](../../../internal/cli/workspace.go#L84-L91)）。
- `out` 字段是相对仓库根的相对路径（无法相对时回退到绝对路径）；`pages` 与 `sitestatic.PageFiles` 列出的离线页一一对应（首页 + 五个二级页）。
- `baseline` 与 `trust_state` 取自同一份 `view.Model`——脚本拿到的"站点 + 时间 + 信任态"和 view 共源，但接口不再共用，避免下游把站点清单误读成视图。

### serve（`/api/view`）—— 与 view --json 同一模型，加 `generated_at`

`workspace serve`（M7.3）在 `/api/view` 暴露同样的视图模型，外层补一个 `generated_at` 时间戳供前端缓存/对比：

```json
{
  "ok": true,
  "generated_at": "2026-09-19T12:34:56Z",
  "schema_version": 1,
  "project": { ... },
  "trust":   { ... },
  "baseline": { ... },
  "progress": { ... },
  "runs":    { ... },
  "records": { ... },
  "knowledge": { ... },
  "sources":  [ ... ],
  "problems": []
}
```

- 形态：`json.NewEncoder(w).Encode(struct{OK bool; GeneratedAt string; view.Model})`（[internal/cli/workspace_serve.go:85-87](../../../internal/cli/workspace_serve.go#L85-L87)，嵌套在 `serveHTTP` 的 `/api/view` 分支里）。
- 与 `view --json` 共享同一份 `view.Build` 结果——`/data/model.json` 走 `sitestatic.ModelJSON`（无 `generated_at`），`/api/view` 走这条带时间戳的信封，前端按需选。
- 其它路由：`/` 与 `/<page>.html` 走 `servePage` 渲染离线页，`/assets/style.css` 走 `sitestatic.Stylesheet()`，`/healthz` 是 `{"ok":true}`，写方法一律 `405`。

### 人类输出（一行一事实 + 缩进）

无 `--json` / 无 `--quiet` 时由 `renderWorkspaceView`（[internal/cli/workspace.go:149-228](../../../internal/cli/workspace.go#L149-L228)）生成：

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
| `  affected: <rel page>` | 每个 `k.Affected` 项 | 二级 |
| `  unverifiable: <rel page>` | 每个 `k.Unverifiable` 项 | 二级 |
| `  hint: run `devsys knowledge refresh` to regenerate affected pages` | `k.Status == KnowledgeStale` | 二级 |
| `  hint: run `devsys knowledge status` to see the underlying error` | `k.Status == KnowledgeUnavailable && trust != pending` | 二级 |
| `  hint: run `devsys doctor` to inspect before `devsys recover`` | `len(m.Trust.Pending) > 0` | 二级 |
| `  hint: configure `knowledge_generator`, then run `devsys knowledge refresh --full`` | `k.Status == KnowledgeMissing` | 二级 |
| `  trust: <state> — <note>` | `m.Trust.State != TrustOK` | 二级 |
| `  pending: <id1>, <id2>, …` | `len(m.Trust.Pending) > 0` | 二级 |
| `  problem: <msg>` | 每个 `m.Problems` 项 | 二级 |
| `  sources: <rel path>, …` | 恒有 | 二级 |

退出码（沿用既有体系）：成功 0 / 子命令缺失或未知（`view | build | serve` 之外的子命令）或 `--limit <= 0`（view/build）、`--port` 越界或非 loopback host 且无 `--allow-remote`（serve） → 2 / `storage.ErrNotInitialized` → 3 / `view.Build` 其它失败 → 1。**`advisory_unlocked` / `pending_transaction` 都是 exit 0**——`trust` 与 `pending` 字段在 JSON / 人类输出里告诉调用方状态不可信，而不是用退出码隐藏语义。10/11 仍专属 `devsys knowledge status`，视图层把 `knowledge.status` 用字符串承载（`KnowledgeFresh / Stale / Missing / Unavailable`），不与退出码耦合。`build --static` 没有 `--static` 也走 2（与 M7.2 "唯一支持的站点形态"约定一致）；`serve` 在 `--json` / `--quiet` 下也走 2（HTTP 服务器不接受这两种输出形态）。

## 与其他层的关系

- [架构设计](./架构设计.md) — 命令分发 + `render` + `toCoded`
- [共享应用与 MCP 错误语义](../共享应用与MCP/错误语义.md) — `*app.Error.Class()` 是双入口错误翻译的源头
- [视图层 · 概述](../视图层/概述.md) — M7.1 `view.Model` 字段序与 Provenance / Trust / Knowledge 状态的完整定义

## M8 sync / archive / repair / dispatch 命令面（M8.1–M8.4）

M8 把「handoff 接力 / 归档 / 冲突标注 / dispatch 门控」四条新命令面并入 `--json` 信封与人类输出协议。下面只写**信封字段与退出码**，tick 内部顺序、Plan/Apply 步骤细节由 [执行命令与 DEVSYS_PROJECT_ROOT](./执行命令与DEVSYS_PROJECT_ROOT.md) 与「对账与修复」卡承载。

### `sync status` —— `--json` 信封与人类输出（M8.1）

`devsys sync status` 不接 flag（[internal/cli/sync.go:36-68](../../../internal/cli/sync.go#L36-L68)）；调用 `syncstatus.Check(ctx, svc.Root)`（[internal/syncstatus/syncstatus.go:82-199](../../../internal/syncstatus/syncstatus.go#L82-L199)），**只读判定、不写盘、不恢复事务、不 fetch 网络**。`--json` 模式下输出 `{ok: true, ...Status}`（[internal/cli/sync.go:58-63](../../../internal/cli/sync.go#L58-L63)）—— `Status` 字段（[internal/syncstatus/syncstatus.go:52-77](../../../internal/syncstatus/syncstatus.go#L52-L77)）：

```json
{
  "ok": true,
  "branch": "main",
  "commit": "52294b0...",
  "upstream": "origin/main",
  "ahead": 0,
  "behind": 0,
  "unmerged": [],
  "merge_work": [],
  "uncommitted_devsys": [],
  "uncommitted_other": [],
  "pending_transactions": [],
  "leases_deferred": false,
  "leases": [],
  "handoff_ready": false,
  "blockers": [{"code": "no-upstream", "detail": "..."}],
  "note": ""
}
```

- `Blocker.Code` 10 个稳定字符串（[internal/syncstatus/syncstatus.go:24-35](../../../internal/syncstatus/syncstatus.go#L24-L35)）：`pending-transactions` / `unmerged-paths` / `merge-in-progress` / `active-leases` / `expired-leases` / `orphan-leases` / `unreadable-leases` / `uncommitted-devsys` / `diverged-from-upstream` / `no-upstream`（脚本按 `code` 分支，不要解析 `detail`）。
- `handoff_ready` 是布尔结论：`len(Blockers) == 0` 时为 `true`（[internal/syncstatus/syncstatus.go:197](../../../internal/syncstatus/syncstatus.go#L197)）。
- `pending_transactions` 非空时 `leases_deferred = true`，`leases` 留空，`note` = `lease audit deferred: pending transactions make the lease snapshot partial`（[internal/syncstatus/syncstatus.go:165-171](../../../internal/syncstatus/syncstatus.go#L165-L171)）。

人类输出（`renderSyncStatus`，[internal/cli/sync.go:82-126](../../../internal/cli/sync.go#L82-L126)）固定顺序：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `git: <branch>@<12-hex> -> <upstream> (+<ahead>/-<behind>)` | `Upstream != ""` | 顶层 |
| `git: <branch>@<12-hex> (no upstream)` | `Upstream == ""` | 顶层 |
| `unmerged: <paths>` | `len(Unmerged) > 0` | 二级 |
| `merge: unfinished (<machine>)` | `len(MergeWork) > 0` | 二级 |
| `uncommitted .devsys/: <paths>` | `len(UncommittedDevsys) > 0` | 二级 |
| `uncommitted other: <paths>` | `len(UncommittedOther) > 0` | 二级 |
| `leases: audit deferred (pending transactions)` | `LeasesDeferred` | 二级 |
| `lease: <wi> (<kind>)  owner=<o>  run=<r>` | 每个 lease（有 run） | 二级 |
| `lease: <wi> (<kind>)  owner=<o>` | 每个 lease（无 run） | 二级 |
| `handoff: ready — old device already stopped? \`git push\`, then the new device runs \`git fetch\` + \`devsys sync status\`` | `HandoffReady` | 顶层 |
| `blocked [<code>]: <detail>` | 每个 Blocker | 二级 |
| `handoff: NOT ready` | `!HandoffReady` | 顶层 |

退出码：`storage.ErrNotInitialized`（`.devsys/` 缺失或非目录）→ `CodePrecondition = 3`（[internal/cli/sync.go:51-53](../../../internal/cli/sync.go#L51-L53)）；其它环境失败（git 缺失、当前不在仓库内）→ `CodePrecondition = 3` + 单行 stderr（`trimSyncErr` 截首行，[internal/cli/sync.go:54-77](../../../internal/cli/sync.go#L54-L77)）。**`handoff_ready = false` / `len(Blockers) > 0` / `leases_deferred = true` 全部 `exit 0`**——handoff 不可达是事实标签，调用脚本按 `handoff_ready` 分支而不是退出码。

### `repair --dry-run / --apply` —— 冲突标注与 apply rejected（M8.2）

M8.2 在 `repair` 的人类输出与 `--apply` 路径里加入「git 冲突」的**人类项**与拒绝形态。`--json` 信封与 M2 协议相同：`--dry-run` 走 `{ok: true, plan: reconcile.Plan}`；`--apply` 走 `{ok: true, apply: reconcile.ApplyReport}`（[internal/cli/diagnostics.go:242-247](../../../internal/cli/diagnostics.go#L242-L247)、[internal/cli/diagnostics.go:265-270](../../../internal/cli/diagnostics.go#L265-L270)）。

人类输出（[internal/cli/diagnostics.go:271-286](../../../internal/cli/diagnostics.go#L271-L286)）新增 / 强化：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `note: <text>` | `plan.Note != ""`（含 git 探针不可用 / inspection 无共享锁 / 软失败） | 二级 |
| `<kind>\t<workitem-id>\t<description>` | 每个 `plan.Proposals` | 二级 |
| `  absent  git:<path>` | 每个 `Evidence{Kind: EvidenceAbsent, Path: "git:..."}` 证据（**含 `note_unmerged_paths`**） | 三级 |
| `  file    <path>  <12-hex>` | 每个文件型证据 | 三级 |
| `digest: <hex>` | 恒有 | 顶层 |
| `applied  <workitem-id>` | `--apply` 后的每条成功（`Applied`） | 二级 |
| `rejected <workitem-id>` | `--apply` 后被拒的每条（`Rejected`） | 二级 |

`Plan` 形态（M8.2 三键，[internal/reconcile/reconcile.go:82-87](../../../internal/reconcile/reconcile.go#L82-L87)）：`Proposals / Digest / Note`；`Proposal.Kind = ProposalNoteUnmerged = "note_unmerged_paths"`（[internal/reconcile/reconcile.go:45-49](../../../internal/reconcile/reconcile.go#L45-L49)）由 `conflictNotes` 注入（[internal/reconcile/reconcile.go:306-314](../../../internal/reconcile/reconcile.go#L306-L314)、[internal/reconcile/reconcile.go:327-363](../../../internal/reconcile/reconcile.go#L327-L363)），**`RepairApply` 总是将其路由到 `Rejected`，写盘为空**（[internal/reconcile/reconcile.go:501-505](../../../internal/reconcile/reconcile.go#L501-L505)）——冲突归 git，devsys 不挑边、不写注解、不删 marker。`--apply` 的 `--confirm` digest 比对失败（`ErrDigestMismatch`）→ `errPrecondition` → `CodePrecondition = 3` + 提示 `run \`devsys repair --dry-run\` again`（[internal/cli/diagnostics.go:236-239](../../../internal/cli/diagnostics.go#L236-L239)）。

### `archive events|runs` —— `--json` 信封与人类输出（M8.3）

`devsys archive {events|runs}`（[internal/cli/archive.go:19-31](../../../internal/cli/archive.go#L19-L31)）不接共享 `--json` 子命令开关；走各自的 `runArchiveEvents` / `runArchiveRuns`。`--json` 输出 `{ok: true, archive: archive.Report}`（[internal/cli/archive.go:87-93](../../../internal/cli/archive.go#L87-L93)），`Report` 字段（[internal/archive/archive.go:65-74](../../../internal/archive/archive.go#L65-L74)）：`Archived / Skipped / BytesArchived / BytesBefore / BytesAfter / Manifest{schema_version, entries} / DryRun / Note`。`Spec`（[internal/archive/archive.go:77-86](../../../internal/archive/archive.go:77-L86)）：`BeforeMonth = "YYYY-MM"`、`RunIDs []string`、`Actor / Reason`、`DryRun`、`Now`（默认 `time.Now`）。

人类输出（`renderArchive`，[internal/cli/archive.go:87-110](../../../internal/cli/archive.go#L87-L110)）：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `dry-run: nothing was moved` | `DryRun` | 顶层 |
| `archived <rel>` | 每个 `Archived`（如 `events/2020-01.jsonl -> archive/events/2020-01.jsonl`） | 二级 |
| `skipped  <rel>` | 每个 `Skipped` | 二级 |
| `note: <text>` | `Note != ""` | 二级 |
| `live bytes: <before> -> <after> (archived <n>)` | 恒有 | 顶层 |

退出码：`--before`（events）/`--id`（runs）/ `--actor` / `--reason` 缺失或子命令缺失/未知 → `errUsage` → `CodeUsage = 2`（[internal/cli/archive.go:48-49](../../../internal/cli/archive.go#L48-L49)、[internal/cli/archive.go:71-72](../../../internal/cli/archive.go#L71-L72)、[internal/cli/archive.go:20-22](../../../internal/cli/archive.go#L20-L22)）；`archive.Apply` 内部错误（年月份格式、actor/reason 缺失、storage CAS 失败）→ `errInternal` → `CodeInternal = 1`。`--dry-run` 成功仍 exit 0（不是「待确认」信号）。

### `dispatch --dry-run / --watch` 的 merge 门控文案（M8.2）

dispatch tick 在 `recover` **之前**跑 `mergeConflict(root)`（[internal/app/dispatch.go:86-95](../../../internal/app/dispatch.go#L86-L95)、[internal/app/dispatch.go:230-263](../../../internal/app/dispatch.go#L230-L263)）：发现 `knowledge.PorcelainStatus` 的 unmerged XY 或 `knowledge.MergeHeads` 报出的 merge machinery → `Preconditionf("dispatch blocked: <summary>; resolve the merge with git, then \`devsys repair --dry-run\`", blocked)` → `CodePrecondition = 3`。人类输出走 `renderDispatch`（[internal/cli/dispatch.go:66-137](../../../internal/cli/dispatch.go#L66-L137)），错误仍走 M4 stderr JSON 信封，**不在 notice 列表里**—— `Notice` 仅记录「git 探针不可用（`"git conflict probe unavailable (...)"`）」的**降级**，不是冲突本身。

成功路径的人类输出按既有约定（`dry-run: ...` / `recovered: ...` / `in flight: N (global cap N)` / `swept <wi>\t<action>\t...` / `started <wi>\t<run>[ pid=N]\tlog=...` / `planned <wi>\t(not started)` / `skipped (<reason>): N\t<ids>` / `notice: <text>`）逐行落到 stdout。
