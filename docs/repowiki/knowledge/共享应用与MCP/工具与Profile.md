---
status: stable
type: module
dimension: tool_profiles
triggers:
  - Profile 默认组合
  - session profile
  - executor profile
  - reviewer profile
  - admin profile
  - 工具过滤
  - 拼错 profile
  - TierCore
  - TierStandard
  - core 档
  - standard 档
  - visibleTier
  - tierLevel
  - ParseTier
  - 默认 tier
  - --tier core|standard
  - project_blueprint_get
  - no blueprint declared
  - tier 默认
  - health tier
  - toolSpec.tier
  - project_update blueprint_artifact_id
  - empty tools refuse
  - mcp serve 0 tools
  - workloom mcp install
  - 注册名 devsys
  - mcp serve --profile session,executor --tier core
  - run_verify skipped
  - workloom setup 不注册 MCP
description: "MCP 服务端的四种 profile（session / executor / reviewer / admin）暴露规则、默认组合与 `visible` 的注册期过滤语义；本批（#301）在 profile 之上叠加 **tier 档**——`TierCore` 是默认 CLI `--tier core` / 缺省值（20 项「日常子集」工具），`TierStandard` 含 profile 全量（66 项工具面），二者与 profile 合取（visible(spec.profiles, cfg.Profiles) && visibleTier(spec.tier, tier)）。`toolSpec` 新增 `tier` 字段（空 = standard，显式 `TierCore` 才进 core 档）；`server.go` 新增 `ParseTier`（拒绝未知名）/ `tierLevel`（core=0 / 其它=1）/ `visibleTier`；`tools_health.go` 的 `health` 响应回传当前 `tier`；CLI `workloom mcp serve --tier core|standard` 直接转 `mcp.ParseTier`。`project_blueprint_get`（session 档 / standard tier）补注册入口由 `tools.go` 与 `tools_project.go:56` 共同组成，未声明蓝图时 `artifact: null`。本批（#336/#337，project_update 蓝图字段+mcp 启动前过滤）：project_update 输入增 blueprint_artifact_id（empty string clears，id must already be registered）；internal/mcp/server.go 新导出 VisibleTools(cfg) []string，让 CLI mcp serve 在 0 工具时拒绝启动（exit 2，提示 --tier standard），是注册期过滤之外的\"启用前\"二次检查。；本轮（ca58d27→19b9149，setup/mcp install 与注册名）：workloom mcp install 写入的条目统一调用 mcp serve --profile session,executor --tier core（与 DefaultProfiles / DefaultTier 默认一致），目标客户端 codex / claude / opencode 支持 user / project scope；注册名保持 devsys（二进制改名 workloom，配置键仍是 mcp_servers.devsys / mcpServers.devsys / mcp.devsys）；workloom setup 不注册 MCP（只报告 mcp 行）；MCP run_verify 的返回体随 CompletionCheck 增 skipped 字段（非 Git 项目 / 目录工作区），Advanced 仍为 false。"
generated: true
source_commit: 19b9149
generator: repowiki-gen
---

# 工具与 Profile · 共享应用与 MCP

M4 把 MCP 工具的暴露面拆成四种 profile。`DefaultProfiles = [session, executor]`，未经 `--profile` 显式指定时启用这一组合；`ParseProfiles` 拒绝任何未知名字（拼错不能静默放行），并在解析时去重。

## Profile 默认组合

`internal/mcp/server.go:32-37`：

```go
const (
    ProfileSession  = "session"
    ProfileExecutor = "executor"
    ProfileReviewer = "reviewer"
    ProfileAdmin    = "admin"
)

func DefaultProfiles() []string { return []string{ProfileSession, ProfileExecutor} }
```

| Profile | 风险等级 | 暴露意图 |
|---|---|---|
| `session` | 只读 | 项目元数据、查询类工具、`health`、`agent_session_start`、`context_*`、`knowledge_status`、workflow `next`、`run_log`、`workflow_list/get`、`approval_list/get` |
| `executor` | 写 | `workitem_*`（除 `claim/release` 在 `session` 已列出外）、`workflow_start/step_complete/pause/resume/cancel`、`approval_request/decide`（reviewer 也覆盖）、`decision_create/approve`、`finding_create/resolve`、`event_record`、`artifact_register/update`、`run_create/update/heartbeat` |
| `reviewer` | 治理 | `approval_decide`、`decision_approve`、`finding_resolve`（与 executor 重复时取并集） |
| `admin` | 系统配置 | `project_create/update/state_update` 等根级变更；M4 未把 init / repair 暴露给 MCP——这些仍是 CLI-only（`方案 §8.2` 无对应） |

默认组合 `session + executor` 覆盖了「读取 + 派发 + 推进」闭环；`reviewer` / `admin` 必须**显式** `--profile reviewer,admin` 才能拿到——任何隐式放行都违背 `方案 §8.6`「危险条目必须显式」。

## `toolSpec` 注册表

`internal/mcp/tools.go:28-115` 的 `allTools()` 是唯一的工具清单；每一个工具的 `profiles` 字段决定它在哪些 profile 下被注册，本批新增的 `tier` 字段决定它出现在哪个 tier 档里（**显式 `TierCore` 才进 core 档**，空 = standard）。

```go
type toolSpec struct {
    name     string
    profiles []string
    // tier: an empty tier means standard (opt in to core explicitly, never by omission).
    tier     string
    register func(*mcpsdk.Server, Config)
}
```

`tier` 字段值域：`""`（= standard）或 `mcp.TierCore`；`mcp.TierStandard` 仅作 CLI `--tier standard` 字面量（不在 `toolSpec` 里出现，空字符串已隐式等价）。

注册逻辑（`internal/mcp/server.go:137-145`，本批把 tier 维度的过滤也并入同一循环）：

```go
tier := cfg.Tier
if tier == "" {
    tier = DefaultTier()
}
for _, spec := range allTools() {
    if !visible(spec.profiles, cfg.Profiles) || !visibleTier(spec.tier, tier) {
        continue
    }
    spec.register(server, cfg)
}
```

`visible(specProfiles, activeProfiles)`（[internal/mcp/server.go](file://internal/mcp/server.go)）的语义：

- 任一参数为空 → `false`（空 `spec.profiles` = 永不可见；空 `cfg.Profiles` = 没有任何工具）。
- `spec.profiles ∩ cfg.Profiles` 非空 → `true`。
- 否则 `false`。

`visibleTier(toolTier, selected)`（[internal/mcp/server.go:196-198](file://internal/mcp/server.go#L196-L198)）的语义：

- 通过 `tierLevel` 把 tier 排序成整数：`TierCore = 0`，其它（含 `""`、`standard`）= `1`。
- `tierLevel(toolTier) <= tierLevel(selected)` 才暴露——core 在 core+standard 都暴露，standard 仅在 standard 暴露。
- 工具的 tier 字段为空 = standard（与「显式 `TierStandard`」等价）；想让工具进 core 档，必须显式标 `TierCore`。

未注册的工具：

1. `tools/list` 不出现；
2. `tools/call` 用名字调用 → SDK 标准 `unknown tool` 错误；
3. 业务层面等价于「权限不足」——但因为过滤发生在注册期而不是 handler 内，权限语义被钉死。

## 工具清单速查（默认 `session + executor`，tier 默认 `core`）

| 族 | session 暴露 | executor 额外暴露 | reviewer 额外暴露 | admin 额外暴露 |
|---|---|---|---|---|
| health | `health` | — | — | — |
| project | `project_list` / `project_get` / `project_status` / `project_blueprint_get`（standard tier） | — | — | `project_create` / `project_update` / `project_state_update` |
| workitem | `workitem_list` / `workitem_get` / `workitem_next` | `workitem_create` / `workitem_update` / `workitem_transition` / `workitem_claim` / `workitem_release` / `workitem_start` / `workitem_block` / `workitem_complete` / `workitem_comment` / `workitem_add_dependency` / `workitem_remove_dependency` | — | — |
| workflow | `workflow_list` / `workflow_get` / `workflow_step_next` | `workflow_start` / `workflow_step_complete` / `workflow_pause` / `workflow_resume` / `workflow_cancel` | — | — |
| approval | `approval_list` / `approval_get` | `approval_request` | `approval_decide` | — |
| decision | `decision_list` / `decision_get` | `decision_create` | `decision_approve` | — |
| finding | `finding_list` / `finding_get` | `finding_create` / `finding_resolve` | — | — |
| event | `event_list` | `event_record` | — | — |
| artifact | `artifact_list` / `artifact_get` / `artifact_history` | `artifact_register` / `artifact_update` | — | — |
| run | `run_list` / `run_get` / `run_log` / `run_verify` | `run_create` / `run_update` / `run_heartbeat` / `run_complete` / `run_fail` / `run_cancel` | — | — |
| context | `context_get` / `context_for_workitem` / `context_refresh` / `context_compact` | — | — | — |
| session | `agent_session_start` | — | — | — |
| knowledge | `knowledge_status` | — | — | — |

说明：

- `project_blueprint_get`（session + standard tier）走 `internal/mcp/tools_project.go:56 registerProjectBlueprint`；未声明蓝图时返回 `artifact: null`（`ProjectBlueprint` 在 `internal/app/project.go:204` 返回 `(nil, nil)`），CLI `project blueprint` 与 MCP 都按 exit 0 处理。
- `workitem_update` / `workflow_step_complete` / `run_update` / `run_fail` / `run_cancel` / `workitem_block` / `context_for_workitem` / `context_refresh` / `context_compact` 等「日常以外的扩展项」均为 **standard tier**（`toolSpec.tier = ""`）——core 档不暴露。`event_record` 是 TierCore（日常子集）。
- `health` 在两种 tier 都暴露（`TierCore`），并把当前 `tier` 字段回写到响应里（[internal/mcp/tools_health.go:52-72](file://internal/mcp/tools_health.go#L52-L72)）。
- 工具总数：`allTools()` 共 **66 项**（含 20 项 `TierCore` + 46 项 `TierStandard`）；默认 profile（`session + executor`）+ core 档暴露 **恰好 20 项**。`run_complete` / `run_fail` / `run_cancel` 各有自己的 register（`registerRunComplete` / `registerRunFail` / `registerRunCancel`；后两者共用 `registerRunFinish` helper，每次只登记一个名字），因此 tier 门不会把 standard 条目带进 core（v0.1.2 / ba342b0 拆 `registerRunFinish` 的修复；v0.1.4 / 9aab68f 把 `workitem_release` 补进 core 档，19→20——claim 与 release 成对，agent 能 claim 就必须能在 core 档 let go）。`--tier standard` 时补齐当前 profile 的全量（四 profile 合计 66）。进度/失败/受阻（`run_update` / `run_fail` / `run_cancel` / `workitem_block`）留在 standard，MCP 优先路径用 CLI 或 `--tier standard`。

### Core 档（20 项）

| 族 | TierCore 工具 |
|---|---|
| health | `health` |
| project | `project_get` |
| workitem | `workitem_list` / `workitem_get` / `workitem_create` / `workitem_transition` / `workitem_claim` / `workitem_release` / `workitem_comment` |
| approval | `approval_request` |
| decision | `decision_create` |
| finding | `finding_create` |
| event | `event_record` |
| artifact | `artifact_register` |
| run | `run_create` / `run_verify` / `run_complete` |
| context | `context_get` |
| session | `agent_session_start` |
| knowledge | `knowledge_status` |

冒烟覆盖（`scripts/m4helper/main.go:69-82`；脚本以 `--tier standard` 起服务，因此 want 名单里可以含 standard 档的 `run_heartbeat`，断言文案仍为 "default profile …"）：

```go
// --- M4.1/M4.2: profile-filtered tool surface -------------------------
list, err := session.ListTools(ctx, nil)
...
for _, want := range []string{"health", "workitem_list", "workitem_get", "decision_get",
    "agent_session_start", "knowledge_status", "run_heartbeat"} {
    check(names[want], "default profile exposes %s", want)
}
for _, forbidden := range []string{"project_update", "project_create", "approval_decide"} {
    check(!names[forbidden], "default profile hides %s", forbidden)
}
```

tier 维度的矩阵（core 藏 `workitem_update` / `workflow_step_complete` / `run_fail`，standard 全量恢复）由 `internal/mcp/server_test.go` 守住：`TestToolSurfaceMatchesSpecFilter`（profile×tier 矩阵与 spec 过滤一致）、`TestDefaultCoreIsTheNamedDailySubset`（20 名冻结名单，`coreDailySubset`）、`TestEachToolRegistersAlone`（禁止多工具共用 register 指针）、`TestCoreHidesRunFail`、`TestTierStandardRestoresFullSessionExecutor` / `TestParseTier` / `TestProjectBlueprintToolAnswersWithoutADeclaration`。

## `ParseProfiles` 拒绝策略

`internal/mcp/server.go:76-121`：

- 空字符串 → `DefaultProfiles()`。
- 拆分 `,` → 逐项 `TrimSpace`、空项报错、未知名字报错、去重后返回。
- 错误原文：`profile list %q contains an empty name` / `unknown profile %q (expected session, executor, reviewer or admin)`。

调用方在 CLI 层 (`workloom mcp serve --profile session,executor`) 直接看到错误；MCP 端 `Run` 返回 error 并被 `runMCPServe` 转译为 `errInternal`。

## `ParseTier` 拒绝策略（本批新增）

`internal/mcp/server.go:49-61`：

- 空字符串 → `DefaultTier()`（即 `TierCore`）。
- 字面量只接受 `core` / `standard`，未知名直接报错。
- 错误原文：`unknown tier %q (expected core or standard)`。

CLI 入口：`workloom mcp serve --tier core` / `--tier standard`（[internal/cli/mcp.go:133-160](file://internal/cli/mcp.go#L133-L160)）。CLI 层只把字面量原样传给 `mcp.ParseTier`，不重复实现 tier 解析。

`DefaultTier()` 与 `DefaultProfiles()` 互相独立——`DefaultProfiles()` 是 `session + executor`，`DefaultTier()` 是 `core`（最小可用子集）。CLI 不显式 `--tier` 也不显式 `--profile` 时，两层默认同时生效，结果就是「core 档 + session/executor 全暴露」的常用组合。


### `project_update` 蓝图字段（本批新增）

`internal/mcp/tools_project.go:127-150` 的 `projectUpdateInput` 增 `BlueprintArtifactID *string`（`json:"blueprint_artifact_id,omitempty"`），jsonschema 注明 `artifact id to declare as the project blueprint (empty string clears; the id must already be registered)`。handler 直接转发到 `cfg.service().ProjectUpdate`，业务校验全在 `internal/app/project.go:62-75`：

- 非空字符串必须 `record.KindArtifact.ValidID(id)` 通过——形态错 `Usagef`（CLI exit 2 / MCP `code=usage`）；
- `record.New(s.Root).GetArtifact(ctx, id)` 返回 `ErrNotFound` → ``Preconditionf("artifact %q not found; register it first (`workloom artifact register --path <file> --name <name>`)", id)``（CLI exit 3 / MCP `code=precondition`）；
- 其它错误走 `storeError`。

### `VisibleTools` 与 0 工具拒绝（本批新增）

`internal/mcp/server.go:149-163` 新导出 `VisibleTools(cfg Config) []string`，沿用 `NewServer` 内同一 `visible(...) && visibleTier(...)` 过滤——返回值为「当前 cfg 启动时会注册的工具名集合」。`internal/cli/mcp.go:173-186` 的 `runMCPServe` 在 `len(VisibleTools(cfg)) == 0` 时返回 `errUsage("mcp serve: %s %s %s no tools in tier %s; use --tier standard", …)`：

```text
mcp serve: profile "reviewer" has no tools in tier core; use --tier standard
mcp serve: profiles "a", "b" have no tools in tier core; use --tier standard
```

把"启动一个静默 no-op server"拦在 `mcp.Run` 之前——常见场景是「read-only profile + core tier」。这与注册期过滤是同一语义的 CLI 端表达：注册期过滤让 `tools/list` 不暴露；`VisibleTools` + 0 工具拒绝让 CLI 在 spawn 之前就拒绝启动。

### `mcp install` 写出的注册条目（本轮新增）

`workloom mcp install` 与 `mcp serve` 共用同一份默认值——条目 argv 由 `mcpServeArgs()`（[internal/app/mcpinstall.go:55-57](file://internal/app/mcpinstall.go#L55-L57)）给出：

```text
["mcp", "serve", "--profile", "session,executor", "--tier", "core"]
```

- profile / tier 与 `DefaultProfiles()` / `DefaultTier()` 完全一致：任何客户端注册出来的 server 就是「core 档 20 项 + session/executor」的默认面，不存在第二套默认。
- **条目名仍是 `devsys`**（`mcp_servers.devsys` / `mcpServers.devsys` / `mcp.devsys`），而命令里的二进制名是 `workloom`——本轮改名只动命令面，不动注册名与配置键：user scope 路径仍在 `mcpClientPath`（[internal/app/mcpinstall.go:167-182](file://internal/app/mcpinstall.go#L167-L182)）里，Claude 成员形状 `{"type":"stdio","command":<bin>,"args":mcpServeArgs()}`（[:375-385](file://internal/app/mcpinstall.go#L375-L385)）与 OpenCode 的 `{"type":"local","command":[<bin>, …]}`（[:389-398](file://internal/app/mcpinstall.go#L389-L398)）键名与结构不变。
- 工具面不受 `mcp install` 影响：注册期过滤仍在 `mcp serve` 进程内生效；`install` 只写客户端配置，`setup` **完全不注册**（只报告 `wire --check` 的 `mcp` 行）。
- `run_verify` 的返回体随 `app.CompletionCheck` 增 `skipped` 字段（非 Git 项目 / 目录工作区，`advanced` 仍为 false，`reason` 为 `git completion check is not applicable`）——MCP 客户端与 CLI 看到同一份判定。

## 与其他层的关系

- [架构设计](架构设计.md) — 注册期 `visible` + `visibleTier` 的语义与依赖方向
- [错误语义](错误语义.md) — `fail / failNotice / usageFail` 渲染 `toolError` 的契约；`health` 响应里的 `tier` 字段配合 `toolError.code` 让客户端既能感知档位又能按 `code` 分支
