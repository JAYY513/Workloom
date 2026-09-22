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
  - workitem list []
  - workflow list []
  - doctor INVALID
  - mcp serve 0 工具
  - workflow init --template
  - --blueprint-artifact
  - workloom setup 信封
  - workloom mcp install 信封
  - workloom --version
description: "M4 CLI 的 `--json` / `--jsonl` 两种结构化输出形态与退出码/MCP 错误对应；M7.1–M7.4 workspace 子命令族 view/build/serve（信封差异 + hint 行），人类输出一行一事实；M8 sync status / repair conflict notes / archive events|runs / dispatch merge gate 的 `--json` 信封与人类输出约定；本批（#336/#337）workitem/workflow list 空集合统一 `[]`、doctor `INVALID` 行 + exit 4、`mcp serve` 0 工具 exit 2、`workflow init --template` 与 `--blueprint-artifact` flaggenerated: true；本轮（ca58d27→19b9149，v0.1.9 发布链 + 主命令改名 workloom + Git 可选能力）：usage 文本与错误行前缀改 `workloom`、`--version` 输出 `workloom <version> (<commit>)`；新增 `setup` / `mcp install` 两条命令的信封（`{ok, ready, template, steps[], next?}` 与 `{ok, scope, results[]}`，均沿用 `--json` 单文档信封、不走 `--jsonl`）"
generated: true；
source_commit: 21a9e71
---

# CLI 渲染与交换协议 · 项目接入与配置

M4 把 CLI 改成「薄渲染层」：flag 解析 → `app.New(root)` → `svc.Xxx(...)` → `render`。所有结构化输出都按 `--json`（单文档信封）或 `--jsonl`（一行一条记录）两种形态统一约定，外部脚本按 exit code 或 `code` 字段分支。

## 全局开关

`internal/cli/cli.go` 顶部：

```go
const usage = `workloom - project-local agent development infrastructure

usage:
  workloom [--json | --jsonl] [--quiet] <command>
  ...
`
```
- `--json`：成功输出 `{"ok":true,...}` 单文档；错误输出 `{"ok":false,"error":{...}}` 到 stderr。
- `--jsonl`：列表类命令按「一行一条 JSON 记录」输出，字段名与 `--json` 信封内的同名数组一致。
- `--quiet`：抑制成功输出；错误/warning 仍然走 stderr。
- 全局开关必须**前置**（解析器遇首个非选项参数即停）。
## `writeJSONL[T]` 泛型

`internal/cli/cli.go:180-188`：

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
| `approval list`（**d726ed4 起**） | `<id>\t<effective_state>\t<scope>\t<workitem>\t<stage>\t<requested_status>`（第二列由 `approvalState(apr)` 计算：已 `consumed_at` 显示 `consumed`，已 `invalidated_at` 显示 `invalidated`，否则原始 `apr.Status`） |
| `workloom prime`（**P2**） | `<project name/id> ... <next: action>` 与 `session start` 文本同源但 payload 较短；`--json` 信封与 `SessionView` 字段名一致 |
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

`internal/cli/cli.go:44-56`：

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

## `workloom workspace view/build/serve` 的协议（M7.1–M7.4）

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

- 形态：`json.NewEncoder(stdout).Encode(struct{OK bool; view.Model})`（[internal/cli/workspace.go:135-140](file://internal/cli/workspace.go#L135-L140)）——`OK: true` 与 `view.Model` 字段在 JSON 顶层并列；M4 的 `{ok: true, data}` 写法在此被打破，因为视图本身就是数据。
- 字段顺序：与 `view.Model` struct 字段序一致（[internal/view/view.go:65-80](file://internal/view/view.go#L65-L80)），保证序列化稳定。
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

- 形态：`json.NewEncoder(stdout).Encode(struct{OK bool; Out string; Pages []string; GeneratedAt string; Baseline view.Baseline; TrustState string})`（[internal/cli/workspace.go:83-92](file://internal/cli/workspace.go#L83-L92)）。
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

- 形态：`json.NewEncoder(w).Encode(struct{OK bool; GeneratedAt string; view.Model})`（[internal/cli/workspace_serve.go:84-86](file://internal/cli/workspace_serve.go#L84-L86)，嵌套在 `serveHTTP` 的 `/api/view` 分支里）。
- 与 `view --json` 共享同一份 `view.Build` 结果——`/data/model.json` 走 `sitestatic.ModelJSON`（无 `generated_at`），`/api/view` 走这条带时间戳的信封，前端按需选。
- 其它路由：`/` 与 `/<page>.html` 走 `servePage` 渲染离线页，`/assets/style.css` 走 `sitestatic.Stylesheet()`，`/healthz` 是 `{"ok":true}`，写方法一律 `405`。

### 人类输出（一行一事实 + 缩进）

无 `--json` / 无 `--quiet` 时由 `renderWorkspaceView`（[internal/cli/workspace.go:149-230](file://internal/cli/workspace.go#L149-L230)）生成：

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
| `  hint: run `workloom knowledge refresh` to regenerate affected pages` | `k.Status == KnowledgeStale` | 二级 |
| `  hint: run `workloom knowledge status` to see the underlying error` | `k.Status == KnowledgeUnavailable && trust != pending` | 二级 |
| `  hint: run `workloom doctor` to inspect before `workloom recover`` | `len(m.Trust.Pending) > 0` | 二级 |
| `  hint: configure `knowledge_generator`, then run `workloom knowledge refresh --full`` | `k.Status == KnowledgeMissing` | 二级 |
| `  trust: <state> — <note>` | `m.Trust.State != TrustOK` | 二级 |
| `  pending: <id1>, <id2>, …` | `len(m.Trust.Pending) > 0` | 二级 |
| `  problem: <msg>` | 每个 `m.Problems` 项 | 二级 |
| `  sources: <rel path>, …` | 恒有 | 二级 |

退出码（沿用既有体系）：成功 0；子命令缺失 → stdout 打印用法 + **exit 0**（v0.1.4 起 `familyUsage`），未知子命令（`view | build | serve` 之外）或 `--limit <= 0`（view/build）、`--port` 越界或非 loopback host 且无 `--allow-remote`（serve） → 2；`storage.ErrNotInitialized` → 3；`view.Build` 其它失败 → 1；`serve` 端口占用（EADDRINUSE / Winsock 10048）→ 3（v0.1.4 起 `errPrecondition`）。**`advisory_unlocked` / `pending_transaction` 都是 exit 0**——`trust` 与 `pending` 字段在 JSON / 人类输出里告诉调用方状态不可信，而不是用退出码隐藏语义。10/11 仍专属 `workloom knowledge status`，视图层把 `knowledge.status` 用字符串承载（`KnowledgeFresh / Stale / Missing / Unavailable`），不与退出码耦合。`build --static` 没有 `--static` 也走 2（与 M7.2 "唯一支持的站点形态"约定一致）；`serve` 在 `--json` / `--quiet` 下也走 2（HTTP 服务器不接受这两种输出形态）。

## 与其他层的关系

- [架构设计](./架构设计.md) — 命令分发 + `render` + `toCoded`
- [共享应用与 MCP 错误语义](../共享应用与MCP/错误语义.md) — `*app.Error.Class()` 是双入口错误翻译的源头
- [视图层 · 概述](../视图层/概述.md) — M7.1 `view.Model` 字段序与 Provenance / Trust / Knowledge 状态的完整定义

## M8 sync / archive / repair / dispatch 命令面（M8.1–M8.4）

M8 把「handoff 接力 / 归档 / 冲突标注 / dispatch 门控」四条新命令面并入 `--json` 信封与人类输出协议。下面只写**信封字段与退出码**，tick 内部顺序、Plan/Apply 步骤细节由 [执行命令与 DEVSYS_PROJECT_ROOT](./执行命令与DEVSYS_PROJECT_ROOT.md) 与「对账与修复」卡承载。

### `sync status` —— `--json` 信封与人类输出（M8.1）

`workloom sync status` 不接 flag（[internal/cli/sync.go:36-68](file://internal/cli/sync.go#L36-L68)）；调用 `syncstatus.Check(ctx, svc.Root)`（[internal/syncstatus/syncstatus.go:82-199](file://internal/syncstatus/syncstatus.go#L82-L199)），**只读判定、不写盘、不恢复事务、不 fetch 网络**。`--json` 模式下输出 `{ok: true, ...Status}`（[internal/cli/sync.go:58-63](file://internal/cli/sync.go#L58-L63)）—— `Status` 字段（[internal/syncstatus/syncstatus.go:52-77](file://internal/syncstatus/syncstatus.go#L52-L77)）：

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

- `Blocker.Code` 10 个稳定字符串（[internal/syncstatus/syncstatus.go:24-35](file://internal/syncstatus/syncstatus.go#L24-L35)）：`pending-transactions` / `unmerged-paths` / `merge-in-progress` / `active-leases` / `expired-leases` / `orphan-leases` / `unreadable-leases` / `uncommitted-devsys` / `diverged-from-upstream` / `no-upstream`（脚本按 `code` 分支，不要解析 `detail`）。
- `handoff_ready` 是布尔结论：`len(Blockers) == 0` 时为 `true`（[internal/syncstatus/syncstatus.go:197](file://internal/syncstatus/syncstatus.go#L197)）。
- `pending_transactions` 非空时 `leases_deferred = true`，`leases` 留空，`note` = `lease audit deferred: pending transactions make the lease snapshot partial`（[internal/syncstatus/syncstatus.go:165-171](file://internal/syncstatus/syncstatus.go#L165-L171)）。

人类输出（`renderSyncStatus`，[internal/cli/sync.go:82-126](file://internal/cli/sync.go#L82-L126)）固定顺序：

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
| `handoff: ready — old device already stopped? \`git push\`, then the new device runs \`git fetch\` + \`workloom sync status\`` | `HandoffReady` | 顶层 |
| `blocked [<code>]: <detail>` | 每个 Blocker | 二级 |
| `handoff: NOT ready` | `!HandoffReady` | 顶层 |

退出码：`storage.ErrNotInitialized`（`.devsys/` 缺失或非目录）→ `CodePrecondition = 3`（[internal/cli/sync.go:51-53](file://internal/cli/sync.go#L51-L53)）；其它环境失败（git 缺失、当前不在仓库内）→ `CodePrecondition = 3` + 单行 stderr（`trimSyncErr` 截首行，[internal/cli/sync.go:54-77](file://internal/cli/sync.go#L54-L77)）。**`handoff_ready = false` / `len(Blockers) > 0` / `leases_deferred = true` 全部 `exit 0`**——handoff 不可达是事实标签，调用脚本按 `handoff_ready` 分支而不是退出码。

### `repair --dry-run / --apply` —— 冲突标注与 apply rejected（M8.2）

M8.2 在 `repair` 的人类输出与 `--apply` 路径里加入「git 冲突」的**人类项**与拒绝形态。`--json` 信封与 M2 协议相同：`--dry-run` 走 `{ok: true, plan: reconcile.Plan}`；`--apply` 走 `{ok: true, apply: reconcile.ApplyReport}`（[internal/cli/diagnostics.go:260-265](file://internal/cli/diagnostics.go#L260-L265)、[internal/cli/diagnostics.go:283-288](file://internal/cli/diagnostics.go#L283-L288)）。

人类输出（[internal/cli/diagnostics.go:289-304](file://internal/cli/diagnostics.go#L289-L304)）新增 / 强化：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `note: <text>` | `plan.Note != ""`（含 git 探针不可用 / inspection 无共享锁 / 软失败） | 二级 |
| `<kind>\t<workitem-id>\t<description>` | 每个 `plan.Proposals` | 二级 |
| `  absent  git:<path>` | 每个 `Evidence{Kind: EvidenceAbsent, Path: "git:..."}` 证据（**含 `note_unmerged_paths`**） | 三级 |
| `  file    <path>  <12-hex>` | 每个文件型证据 | 三级 |
| `digest: <hex>` | 恒有 | 顶层 |
| `applied  <workitem-id>` | `--apply` 后的每条成功（`Applied`） | 二级 |
| `rejected <workitem-id>` | `--apply` 后被拒的每条（`Rejected`） | 二级 |

`Plan` 形态（M8.2 三键，[internal/reconcile/reconcile.go:82-87](file://internal/reconcile/reconcile.go#L82-L87)）：`Proposals / Digest / Note`；`Proposal.Kind = ProposalNoteUnmerged = "note_unmerged_paths"`（[internal/reconcile/reconcile.go:45-49](file://internal/reconcile/reconcile.go#L45-L49)）由 `conflictNotes` 注入（[internal/reconcile/reconcile.go:306-314](file://internal/reconcile/reconcile.go#L306-L314)、[internal/reconcile/reconcile.go:327-363](file://internal/reconcile/reconcile.go#L327-L363)），**`RepairApply` 总是将其路由到 `Rejected`，写盘为空**（[internal/reconcile/reconcile.go:501-505](file://internal/reconcile/reconcile.go#L501-L505)）——冲突归 git，devsys 不挑边、不写注解、不删 marker。`--apply` 的 `--confirm` digest 比对失败（`ErrDigestMismatch`）→ `errPrecondition` → `CodePrecondition = 3` + 提示 `run \`workloom repair --dry-run\` again`（[internal/cli/diagnostics.go:254-257](file://internal/cli/diagnostics.go#L254-L257)）。

### `archive events|runs` —— `--json` 信封与人类输出（M8.3）

`workloom archive {events|runs}`（[internal/cli/archive.go:18-30](file://internal/cli/archive.go#L18-L30)）不接共享 `--json` 子命令开关；走各自的 `runArchiveEvents` / `runArchiveRuns`。`--json` 输出 `{ok: true, archive: archive.Report}`（[internal/cli/archive.go:87-93](file://internal/cli/archive.go#L87-L93)），`Report` 字段（[internal/archive/archive.go:65-74](file://internal/archive/archive.go#L65-L74)）：`Archived / Skipped / BytesArchived / BytesBefore / BytesAfter / Manifest{schema_version, entries} / DryRun / Note`。`Spec`（[internal/archive/archive.go:77-86](file://internal/archive/archive.go#L77-L86)）：`BeforeMonth = "YYYY-MM"`、`RunIDs []string`、`Actor / Reason`、`DryRun`、`Now`（默认 `time.Now`）。

人类输出（`renderArchive`，[internal/cli/archive.go:87-106](file://internal/cli/archive.go#L87-L106)）：

| 行 | 触发条件 | 形态 |
|---|---|---|
| `dry-run: nothing was moved` | `DryRun` | 顶层 |
| `archived <rel>` | 每个 `Archived`（如 `events/2020-01.jsonl -> archive/events/2020-01.jsonl`） | 二级 |
| `skipped  <rel>` | 每个 `Skipped` | 二级 |
| `note: <text>` | `Note != ""` | 二级 |
| `live bytes: <before> -> <after> (archived <n>)` | 恒有 | 顶层 |

退出码：`--before`（events）/`--id`（runs）/ `--actor` / `--reason` 缺失 → `errUsage` → `CodeUsage = 2`（[internal/cli/archive.go:48-49](file://internal/cli/archive.go#L48-L49)、[internal/cli/archive.go:67-68](file://internal/cli/archive.go#L67-L68)）；子命令缺失 → stdout 打印用法 + **exit 0**（v0.1.4 起 `familyUsage`）；未知子命令 → `errUsage` → `CodeUsage = 2`（[internal/cli/archive.go:18-30](file://internal/cli/archive.go#L18-L30)）；`archive.Apply` 内部错误（年月份格式、actor/reason 缺失、storage CAS 失败）→ `errInternal` → `CodeInternal = 1`。`--dry-run` 成功仍 exit 0（不是「待确认」信号）。

### `dispatch --dry-run / --watch` 的 merge 门控文案（M8.2）

dispatch tick 在 `recover` **之前**跑 `mergeConflict(root)`（[internal/app/dispatch.go:86-95](file://internal/app/dispatch.go#L86-L95)、[internal/app/dispatch.go:230-263](file://internal/app/dispatch.go#L230-L263)）：发现 `knowledge.PorcelainStatus` 的 unmerged XY 或 `knowledge.MergeHeads` 报出的 merge machinery → `Preconditionf("dispatch blocked: <summary>; resolve the merge with git, then \`workloom repair --dry-run\`", blocked)` → `CodePrecondition = 3`。人类输出走 `renderDispatch`（[internal/cli/dispatch.go:66-137](file://internal/cli/dispatch.go#L66-L137)），错误仍走 M4 stderr JSON 信封，**不在 notice 列表里**—— `Notice` 仅记录「git 探针不可用（`"git conflict probe unavailable (...)"`）」的**降级**，不是冲突本身。

成功路径的人类输出按既有约定（`dry-run: ...` / `recovered: ...` / `in flight: N (global cap N)` / `swept <wi>\t<action>\t...` / `started <wi>\t<run>[ pid=N]\tlog=...` / `planned <wi>\t(not started)` / `skipped (<reason>): N\t<ids>` / `notice: <text>`）逐行落到 stdout。

## P1 / P2 / d726ed4 / b89ffae 命令面增量

**`workloom prime`（P2）** 与 `workloom session start --compact` 输出同源：人类模式打印 `project: <name> (<id>)` / `phase` / `state:` / `workitem:` 行 + `next: <action> <id>: <reason>` + 可选 `command:`；`--json` 信封 `{ok: true, ...SessionView}` 与 `session start` 同字段名。

**`--latest`（c150007）** 与 `--expect` 互斥：`checkLatest` 在 CLI 层升为 `CodeUsage = 2` + `(pass --expect or --latest, not both)`（[internal/cli/cli.go:592-598](file://internal/cli/cli.go#L592-L598)）。所有写命令的 `usage:` 文本同步改为 `[--expect <hash> | --latest]`。

**`mcp serve --tier core|standard`（P1）** 未知 tier → `errUsage("`workloom mcp serve`: unknown tier %q (expected core or standard)")`（[internal/mcp/server.go:47-60](file://internal/mcp/server.go#L47-L60) `ParseTier`）→ `CodeUsage = 2`。`--json` 输出受 `tierLevel` 影响（core 是 daily 子集，standard 暴露所选 profile 下完整工具集）。


**`wire --check` / `--skill` / `--print-mcp`（P1）** 三选一互斥（[internal/cli/cli.go:445-584](file://internal/cli/cli.go#L445-L584)）；`--check` / `--print-mcp` 是只读，exit 0；`--skill` 是写操作。**v0.1.4 起** `--check` 增 `--strict`（任一检查项失败 → `CodePrecondition = 3`），默认 `wire` 在写纪律块同时一并写 skill 三文件（`skill_changed` 字段）。`--print-mcp <harness>` 未知 → `app.Usagef("unknown harness %q (expected codex, claude or opencode)")` → `CodeUsage = 2`；`--check` 八项 `go` / `git` / `.devsys` / `schema` / `registry` / `AGENTS.md` / `skill` / `mcp` → 人类输出 `[v]/[x] <name>: <detail>`；`--skill` 重复调用 → `skill: already installed (no change)`。

**`project blueprint`（b89ffae）** 未声明蓝图 → `svc.ProjectBlueprint` 返回 `(nil, nil)`；CLI 打印 `no blueprint declared (project.yaml blueprint_artifact_id is empty)` 并 `exit 0`（[internal/cli/cli.go:688-713](file://internal/cli/cli.go#L688-L713)）；`--json` 模式 `{ok: true, artifact: null}`。与 `project status` / `next` 同一族只读路径。


**`approval list` / `approval get`（d726ed4）** 第二列由原始 `apr.Status` 改为 `approvalState(apr)`（[internal/cli/cli.go:1510-1519](file://internal/cli/cli.go#L1510-L1519)）：已 `consumed_at` → `consumed`，已 `invalidated_at` → `invalidated`，否则原 `Status`；`--json` 模式仍带原始 `consumed_at` / `invalidated_at` 字段。

**`storage: version conflict`（c150007 + b89ffae）** 经 `app.storeError`（[internal/app/workitem.go:447-484](file://internal/app/workitem.go#L447-L484)）补充出路文本：重新读取后重试，或用 `--latest` 直接基于当前版本写入。`--json` 信封走 M4 `{ok:false, error:{code:4, kind:"invalid", message:"...", problems?}}` 标准格式。

**`next` / `claim` 同源质量门（b89ffae）** 风险 `quality_blocked <id>` 在 `next` 输出里挂载推荐原因（补救命令形如 `workloom workitem update --id <id> --description ...`）；`retry_pending <id>: retry queued; next attempt at <RFC3339>` 让 `next` 不再误报「无事可做」（[internal/next/evaluate.go:46-47](file://internal/next/evaluate.go#L46-L47)）。

**`claim` 未绑定 + 项目无策略文件（b89ffae）** 打印 `warning: gates are not enforced`（`--json` 模式 `Notice` 字段承载），不阻断 `claim`；`claim` 在 `config.yaml` 非法或默认策略缺失时**fail-closed** → `CodeInvalid = 4` + `config.yaml is invalid: ...; run \`workloom config check``。

## 本批（M9 #336/#337 提交 362b637 / 7e08e3d）

### 列表命令空集合：稳定 `[]`，禁止 `null`

- `workitem list --json` / `workflow list --json` 在结果为 `nil` 时显式补 `[]` 再 encode——[internal/cli/cli.go:861-863](file://internal/cli/cli.go#L861-L863) `if items == nil { items = []*domain.WorkItem{} }` 与 [internal/cli/cli.go:1294-1296](file://internal/cli/cli.go#L1294-L1296) `if policies == nil { policies = []app.PolicySummary{} }`。`internal/workitem/workitem.go` 的 `List` 也返回非 nil `[]`。调用方按行解析时不需要为「空 vs null」二态做分支；`jq '.items | length'` 永远返回数字。

### `CodeInvalid = 4` 不可信路径：doctor / next / prime / session / project status

- `workloom doctor` 人类模式打 `INVALID  <path>  <err>` 行（[internal/cli/diagnostics.go:156-158](file://internal/cli/diagnostics.go#L156-L158)）；`--json` 模式 `rep.InvalidFiles` 嵌入信封同时 `exitWithCode(CodeInvalid)`（[internal/cli/diagnostics.go:131-139](file://internal/cli/diagnostics.go#L131-L139)、[internal/cli/diagnostics.go:170-174](file://internal/cli/diagnostics.go#L170-L174)）。`reconcile.InvalidFile{Path, Err}`（[internal/reconcile/reconcile.go:107-114](file://internal/reconcile/reconcile.go#L107-L114)）是 `InspectionReport.InvalidFiles`（json/yaml tag `invalid_files,omitempty`）的载体；`String()` 返回 `path: err`。健康项目（`InvalidFiles == nil`）→ exit 0。
- `workloom next` / `prime` / `session start` / `project status` 在 `len(doc.InvalidFiles) > 0` 时（[internal/app/next.go:28-46](file://internal/app/next.go#L28-L46)）不调 `next.Evaluate`——`Invalidf(KindInvalid, nil, "next: managed state unreadable: <file>: <err>…")` 装配 `next.Report{}` + 消息体（≤3 条 + `+N more`），`Class()` 归一 `KindInvalid` → `CodeInvalid = 4`。理由：基于不完整工作项列表出的 verdict 会推荐错误动作，按方案 §14.1 视为 untrusted state。调用脚本按 `code == 4` 重新 `workloom recover --actor … --reason …`。

### `CodeUsage = 2` 新增：`mcp serve` 0 工具 / `workflow init --template` / `project update --blueprint-artifact`

- `workloom mcp serve` 在构造 cfg 后调 `mcp.VisibleTools(cfg)` 拿到空 slice 走 `errUsage("mcp serve: %s %s %s no tools in tier %s; use --tier standard", word, strings.Join(quoted, ", "), verb, tier)`（单 profile 渲染为 `mcp serve: profile "session" has no tools in tier core; use --tier standard`）（[internal/cli/mcp.go:79-91](file://internal/cli/mcp.go#L79-L91)）——多 profile 时单复数与动词变 `profiles %s have`。零工具的 silent server 是隐藏陷阱（只读 profile + core tier），文案直接给出 `--tier standard` 出路。
- `workloom workflow init --template <id>`（[internal/cli/cli.go:1212-1243](file://internal/cli/cli.go#L1212-L1243)）：usage 文本 `strings.Join(app.WorkflowTemplates(), "|")` 动态生成（`internal/app/workflow_init.go` + 仓库根 `templates.go`）；未知 id / 已存在 → `Usagef`（已存在：`edit it in place`，提示直接编辑已生成的策略文件）。
- `workloom project update --blueprint-artifact <artifact-id>`（fs.Visit 模式，[internal/cli/cli.go:721-747](file://internal/cli/cli.go#L721-L747)）：empty string clears；非空须 `record.KindArtifact.ValidID`（形态错 → `Usagef` / `CodeUsage = 2`），未注册（`GetArtifact` ErrNotFound → `Preconditionf` / `CodePrecondition = 3` + 出路 `workloom artifact register`）。MCP `project_update` 输入增 `blueprint_artifact_id`（[internal/mcp/tools_project.go](file://internal/mcp/tools_project.go)）。

### `workloom init` 人类输出增「next:」四步

- [internal/cli/cli.go:431-437](file://internal/cli/cli.go#L431-L437) 把 init 完成的人类输出从单行路径汇总升级到「next:」四步引导：`workflow init --template quick-fix (also: feature-development, architecture-change, reference-template), then adapt it` → `workloom wire --skill` → `next.CreateWorkitemCommand` (`workloom workitem create --title "…" --actor <you> --reason "first task"`) → `workloom workspace view`。

### `next` 在空项目 + 无蓝图下的双 remedy 文案

## 本批（v0.1.9，ca58d27→19b9149）

### 主命令改名对输出契约的影响

- `usage` 文本首行与用法行改为 `workloom`；`--version` 输出 `workloom <version> (<commit>)`（[internal/cli/cli.go:59-62](file://internal/cli/cli.go#L59-L62)、[internal/cli/cli.go:241](file://internal/cli/cli.go#L241)）。人类模式错误行前缀由 `devsys: <msg>` 改为 `workloom: <msg>`（[internal/cli/cli.go:381-385](file://internal/cli/cli.go#L381-L385)）。
- **错误信封不变**：`--json` 仍是 stderr 上的 `{ok:false, error:{code, kind, message, problems?}}`；退出码仍是 0/1/2/3/4，`knowledge status` 保留 10/11。改名只动前缀与文本，不动字段名、不动 JSON 结构——按字段解析的脚本不需要改。
- `usage` 里 `3  precondition error (...)` 的括注从「not a git repository」改为「nested project, wrong directory, permissions, digest mismatch」（[internal/cli/cli.go:106](file://internal/cli/cli.go#L106)）——git 不再是入场券。

### `workloom setup` 的 `--json` 信封

`workloom setup [--template quick-fix]`（[internal/cli/setup.go:17-58](file://internal/cli/setup.go#L17-L58)）成功时输出 `{ok: true, ready, template, steps[{name, ok, skipped?, detail}], next?}`（[internal/app/setup.go:22-41](file://internal/app/setup.go#L22-L41)）；失败时同一信封带 `error{code, kind, message}` 并 `exitWithCode(code)`（[internal/cli/setup.go:29-45](file://internal/cli/setup.go#L29-L45)）。人类模式每步一行 `[v] <name>: <detail>` / `[x] <name>: <detail>`，全绿追加 `Workloom is ready.` 与 `next: <命令>`（[internal/cli/setup.go:47-61](file://internal/cli/setup.go#L47-L61)）。

### `workloom mcp install` 的 `--json` 信封

`{ok: true, scope, results[{client, status, detail, path}]}`（[internal/app/mcpinstall.go:36-49](file://internal/app/mcpinstall.go#L36-L49)）；`status ∈ installed | planned | skipped | failed | not-detected`——**默认（不写盘）报 `planned`**。人类模式先打 `scope: <scope>`，再每客户端一行 `[<mark>] <client>: <detail> (<path>)`，mark 为 `v` / `=` / `?` / `x`（[internal/cli/mcp.go:108-127](file://internal/cli/mcp.go#L108-L127)）；写盘前另打 `target <client>: <path>` announce 行（[internal/cli/mcp.go:78-85](file://internal/cli/mcp.go#L78-L85)）。`--apply` 与 `--dry-run` 互斥 → `CodeUsage = 2`（[internal/cli/mcp.go:66-68](file://internal/cli/mcp.go#L66-L68)）。两条命令都**不**走 `--jsonl`（不是单条记录流）。

### Git 可选能力在输出层的表现

- `workloom run verify --json` 的 `CompletionCheck` 增 `skipped`（`json:"skipped,omitempty"`）字段：非 Git 项目 / 目录工作区返回 `{advanced: false, skipped: true, reason: "git completion check is not applicable"}`（[internal/app/runverify.go:19-56](file://internal/app/runverify.go#L19-L56)）。
- `workloom wire --check --json` 的 `git` 行在 git 不在 PATH 时是 `{name: "Git", ok: true, detail: "not on PATH (optional: worktree, sync, knowledge freshness unavailable)"}`——**不**把整次检查打成失败（[internal/app/wirecheck.go:49-54](file://internal/app/wirecheck.go#L49-L54)）。
- **本批（#398）**：`workloom --help` 顶层命令表补回 `init` 与 `sync status` 两行（[internal/cli/cli.go:65](file://internal/cli/cli.go#L65)、[internal/cli/cli.go:83](file://internal/cli/cli.go#L83)）——两条命令一直可路由（`setup` 第一步就是 `init`，`sync status` 是 M8.1 只读接力判定），e4f1a9e 重写命令表时漏列；纯文案、无行为变更。本页引用的 `internal/cli/cli.go` 行号已按 +2 位移重算（`init` 之前不变，`init` 与 `sync status` 之间 +1，其后 +2）。
- **本批（v0.1.10）**：补丁版本发布（`--help` 顶层命令表修复，无行为变更）；本页口径不变，`source_commit` 跟进至 `21a9e71`。
