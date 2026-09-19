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
  - visible 规则
description: MCP 服务端的四种 profile（session / executor / reviewer / admin）暴露规则、默认组合与 `visible` 的注册期过滤语义。
generated: true
source_commit: 463c2d8
generator: repowiki-gen
---

# 工具与 Profile · 共享应用与 MCP

M4 把 MCP 工具的暴露面拆成四种 profile。`DefaultProfiles = [session, executor]`，未经 `--profile` 显式指定时启用这一组合；`ParseProfiles` 拒绝任何未知名字（拼错不能静默放行），并在解析时去重。

## Profile 默认组合

`internal/mcp/server.go:33-38`：

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

`internal/mcp/tools.go:24-119` 的 `allTools()` 是唯一的工具清单；每一个工具的 `profiles` 字段决定它在哪些 profile 下被注册。

```go
type toolSpec struct {
    name     string
    profiles []string
    register func(*mcpsdk.Server, Config)
}
```

注册逻辑（`internal/mcp/server.go:91-105`）：

```go
for _, spec := range allTools() {
    if !visible(spec.profiles, cfg.Profiles) {
        continue
    }
    spec.register(server, cfg)
}
```

`visible(specProfiles, activeProfiles)`（[internal/mcp/server.go:113-130](file://internal/mcp/server.go#L113-L130)）的语义：

- 任一参数为空 → `false`（空 `spec.profiles` = 永不可见；空 `cfg.Profiles` = 没有任何工具）。
- `spec.profiles ∩ cfg.Profiles` 非空 → `true`。
- 否则 `false`。

未注册的工具：

1. `tools/list` 不出现；
2. `tools/call` 用名字调用 → SDK 标准 `unknown tool` 错误；
3. 业务层面等价于「权限不足」——但因为过滤发生在注册期而不是 handler 内，权限语义被钉死。

## 工具清单速查（默认 `session + executor`）

| 族 | session 暴露 | executor 额外暴露 | reviewer 额外暴露 | admin 额外暴露 |
|---|---|---|---|---|
| health | `health` | — | — | — |
| project | `project_list/get/status/blueprint_get` | — | — | `project_create/update/state_update` |
| workitem | `workitem_list/get/next` | `workitem_create/update/transition/claim/release/start/block/complete/comment/add_dependency/remove_dependency` | — | — |
| workflow | `workflow_list/get/step_next` | `workflow_start/step_complete/pause/resume/cancel` | — | — |
| approval | `approval_list/get` | `approval_request` | `approval_decide` | — |
| decision | `decision_list/get` | `decision_create` | `decision_approve` | — |
| finding | `finding_list/get` | `finding_create/resolve` | — | — |
| event | `event_list` | `event_record` | — | — |
| artifact | `artifact_list/get/history` | `artifact_register/update` | — | — |
| run | `run_list/get/log` | `run_create/update/heartbeat` | — | — |
| context | `context_get/workitem/refresh/compact` | — | — | — |
| session | `agent_session_start` | — | — | — |
| knowledge | `knowledge_status` | — | — | — |

冒烟覆盖（`scripts/m4helper/main.go:69-82`）：

```go
for _, want := range []string{"health", "workitem_list", "workitem_get", "decision_get",
    "agent_session_start", "knowledge_status", "run_heartbeat"} {
    check(names[want], "default profile exposes %s", want)
}
for _, forbidden := range []string{"project_update", "project_create", "approval_decide"} {
    check(!names[forbidden], "default profile hides %s", forbidden)
}
```

## `ParseProfiles` 拒绝策略

`internal/mcp/server.go:43-72`：

- 空字符串 → `DefaultProfiles()`。
- 拆分 `,` → 逐项 `TrimSpace`、空项报错、未知名字报错、去重后返回。
- 错误原文：`profile list %q contains an empty name` / `unknown profile %q (expected session, executor, reviewer or admin)`。

调用方在 CLI 层 (`devsys mcp serve --profile session,executor`) 直接看到错误；MCP 端 `Run` 返回 error 并被 `runMCPServe` 转译为 `errInternal`。

## 与其他层的关系

- [架构设计](架构设计.md) — 注册期 `visible` 的语义与依赖方向
- [错误语义](错误语义.md) — `fail / failNotice / usageFail` 渲染 `toolError` 的契约
