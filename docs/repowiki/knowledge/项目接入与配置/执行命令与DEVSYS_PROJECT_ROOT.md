---
status: stable
type: module
dimension: execution_wiring
triggers:
  - DEVSYS_PROJECT_ROOT
  - 工作区根注入
  - 工作区路径
  - 适配器注册
  - ByName
  - Names
  - run exec 接线
  - run prompt 接线
  - run verify 接线
  - run complete 接线
  - run fail 接线
  - run cancel 接线
  - dispatch 接线
  - worktree 接线
  - MCP run_prompt
  - MCP run_verify
  - MCP run_complete
  - MCP run_fail
  - MCP run_cancel
  - HookEnv
  - DEVSYS_WORKSPACE
  - DEVSYS_WORKSPACE_BRANCH
  - DEVSYS_WORKSPACE_IDENTIFIER
  - DEVSYS_HOOK_NAME
  - workspace_root 解析
  - dispatch_command 解析
  - M6 接线
description: M6 执行层（harness/workspace/dispatch/retry/prompt）到 CLI/MCP 的接线：`devsys worktree/dispatch/run exec|prompt|verify|complete|fail|cancel` 与 MCP `run_prompt/verify/complete/fail/cancel` 的对应；`DEVSYS_PROJECT_ROOT` 与 `HookEnv` 注入规则；`config.workspace_root` / `config.dispatch_command` 解析与默认
generated: true
source_commit: 997c5f8
generator: repowiki-gen
---

# 执行命令与 DEVSYS_PROJECT_ROOT

本卡描述 M6 执行层（[执行层](../执行层/概述.md)）到 CLI 与 MCP 入口的**接线规则**——每条 CLI / MCP 工具对应到 `internal/app` 的哪个方法、再调哪个执行层子包。这是「CLI 暴露什么、MCP 暴露什么、谁负责语义」的唯一参考。

## 命令族到 Service 方法的映射

| CLI 命令 | MCP 工具 | profile | `internal/app` 方法 | 调用的执行层 |
|---|---|---|---|---|
| `worktree prepare` | `workspace_prepare`（隐含在 `dispatch`） | admin / executor | `Service.WorkspacePrepare(ctx, WorkspacePrepareRequest{WorkItemID, RunID, Branch, Actor, Reason})` | `internal/workspace.Ensure` + `internal/workspace.RunHook(after_create)` |
| `worktree remove` | — | admin | `Service.WorkspaceRemove(ctx, WorkspaceRemoveRequest{WorkItemID, Path, Force, Actor, Reason})` | `internal/workspace.RunHook(before_remove)` + `internal/workspace.Remove` |
| `worktree list` | — | session | `Service.WorkspaceList(ctx, workItemID)` | `internal/workspace.List` |
| `dispatch --once\|--watch\|--dry-run\|--max` | — | admin | `Service.Dispatch(ctx, DispatchRequest{...})` | `internal/dispatch.Plan` + `internal/workspace.Ensure` + `internal/app.dispatchOne`（包含 `internal/prompt.Assemble`） |
| `run exec` | `run_exec` | executor | `Service.RunExec(ctx, RunExecRequest{...})` | `internal/harness.ByName` + `internal/prompt.Assemble` + `internal/harness.Session.Wait` |
| `run prompt` | `run_prompt` | session | `Service.RunPrompt(ctx, RunPromptRequest{...})` | `internal/prompt.Assemble` |
| `run verify` | `run_verify` | session | `Service.RunVerify(ctx, runID)` | `internal/workspace.HeadSHA` / `BranchSHA` + `git rev-parse refs/heads/<branch>` |
| `run complete` | `run_complete` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunSucceeded, ...})` | `internal/app.verifyCompletion` 守卫 + `internal/app.recordExecOutcome` |
| `run fail` | `run_fail` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunFailed, ...})` | `internal/app.finishAttempt` |
| `run cancel` | `run_cancel` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunCanceled, ...})` | `internal/app.finishAttempt` |

`run update --status <terminal>` 仍走 `Service.RunUpdate`（M4 引入），但 M6 起 CLI 文档推荐用 `run complete\|fail\|cancel`（语义对应 §4.8 终态）。

## 配置：`config.yaml` 新增键

```yaml
schema_version: 1
workspace_root: /var/workloom/workspaces       # 可选；默认 <project>/.devsys/workspaces
dispatch_command: "devsys run exec --id {run_id} --actor dispatch --reason tick"  # 可选；M6.4 默认；M6.7 后由 harness 取代
```

字段定义见 [`internal/config/config.go:85-92`](file://internal/config/config.go#L85-L92)；白名单见 [`internal/config/validate.go:60-66`](file://internal/config/validate.go#L60-L66)。`init` 写 `config.yaml` 时**不**主动写这两个字段（占位文件保持 `schema_version: 1` 唯一键）；用户按需追加。

| 字段 | 默认 | 触发时机 | 解析函数 |
|---|---|---|---|
| `workspace_root` | `<project>/.devsys/workspaces`（`internal/workspace.Root` 推导） | 每次 `app.RunExec` / `app.Dispatch` / `app.WorkspacePrepare` 调用 | `internal/workspace.Root(projectRoot, configured)` |
| `dispatch_command` | `"devsys run exec --id {run_id} --actor dispatch --reason tick"`（由 `app.dispatchCommandLine` 渲染） | `app.dispatchOne` 选 `spawn` 策略时；任何 workitem 的 `assigned_harness` 为空时使用 | `internal/app.dispatchCommandLine` |

## DEVSYS_PROJECT_ROOT

`internal/cli/cli.go:178-196` 的 `appService()` 读取顺序：

```text
1. 命令行 --project-root <abs path>   (CLI 显式开关，未来扩展)
2. 环境变量 DEVSYS_PROJECT_ROOT
3. 当前工作目录
```

`DEVSYS_PROJECT_ROOT` 的语义：

- **agent 在 worktree 内运行时**，worktree 自带一份 `.devsys/`（init 在主仓库根创建过）。如果不指定，agent 会把 attempt 的报告写回 worktree 自带的副本，主仓库 `git pull` 之后看不到任何东西。
- **指定 `DEVSYS_PROJECT_ROOT=<主仓库根>`**，agent 的报告路径解析到主仓库的 `.devsys/`，与 dispatch / 主 CLI 的报告落在同一份文件。
- **`internal/workspace.HookEnv`**（[internal/workspace/hook.go:317-334](file://internal/workspace/hook.go#L317-L334)）自动注入 `DEVSYS_PROJECT_ROOT=<主仓库根>`——agent 在 hook 脚本与 attempt 进程里看到的环境变量都是同一个值。

```sh
# 一个 worktree 内运行的 agent 想把 attempt 报告写回主仓库
export DEVSYS_PROJECT_ROOT=/path/to/main/repo
devsys --json run exec --id run-... --harness codex --actor me --reason accept
```

## HookEnv 注入

每次 hook（after_create / before_run / after_run / before_remove）与 attempt 启动前，`app.HookEnv` 注入：

| 变量 | 含义 | 来源 |
|---|---|---|
| `DEVSYS_PROJECT_ROOT` | 主仓库根（agent 报告目标） | `app.HookEnv` 由 `appService()` 持有 |
| `DEVSYS_WORKSPACE` | 当前 worktree 路径 | `workspace.Ensure` 写入 |
| `DEVSYS_WORKSPACE_BRANCH` | `devsys/<key>` 分支名 | `workspace.branch` |
| `DEVSYS_WORKSPACE_IDENTIFIER` | 工作项标识符 | `WorkspacePrepareRequest.WorkItemID` |
| `DEVSYS_HOOK_NAME` | 触发的 hook 名 | `workspace.RunHook` 显式传入 |

`HookEnv` 在 `internal/harness.shell.Start` 之前与 `internal/workspace.RunHook` 之前调用——任何 hook 脚本与 attempt 都看到同一份环境。

## Harness 注册与 `ByName`

`internal/harness/cli.go:164-179`：

```go
func Names() []string { return []string{"shell", "codex", "opencode", "claude"} }

func ByName(name string) (Adapter, bool) {
    switch name {
    case "shell":    return NewShell(), true
    case "codex":    return Codex(), true
    case "opencode": return OpenCode(), true
    case "claude":   return ClaudeCode(), true
    }
    return nil, false
}
```

- `internal/app.RunExec` 收到 `RunExecRequest.Harness == ""` 时默认用 `NewShell()`；非空时 `ByName` 解析；未知名字 → `*app.Error{Kind: usage}`（exit 2）。
- `internal/app.dispatchOne` 选 spawn 策略：① `WorkItem.AssignedHarness` 非空 → 调 `ByName`；② 否则 `config.DispatchCommand` 模板渲染（默认模板即 CLI 调 `devsys run exec`）——这是 M6.4 默认路径。
- `Probe` 由 cliAdapter 跑 `codex --version` / `opencode --version` / `claude --version`，超时 10s（`probeTimeout`）；不静默替换为其它 harness。

## 与其他卡的关系

- [架构设计](./架构设计.md) — M6 CLI 进一步承载执行层命令；MCP 与 CLI 共用同一 Service
- [特殊配置与命令](./特殊配置与命令.md) — CLI 命令族详细剧本（含 M6 七族）
- [Schema 与错误契约](./Schema与错误契约.md) — `workspace_root` / `dispatch_command` 字段白名单与错误语义
- [共享应用与 MCP](../共享应用与MCP/工具与Profile.md) — MCP `run_prompt` / `run_verify` 是 session profile；`run_complete` / `run_fail` / `run_cancel` 是 executor profile
- [执行层 · 架构设计](../执行层/架构设计.md) — 五子包依赖方向 + Plan 纯函数 + Retry/Prompt 可复现
