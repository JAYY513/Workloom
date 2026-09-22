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
  - workflow init --template
  - VisibleTools
  - BlueprintArtifactID
  - CreateWorkitemCommand
  - 目录工作区
  - Git 可选能力
  - run verify skipped
description: M6 执行层（harness/workspace/dispatch/retry/prompt）到 CLI/MCP 的接线：`workloom worktree/dispatch/run exec|prompt|verify|complete|fail|cancel` 与 MCP `run_prompt/verify/complete/fail/cancel` 的对应；`DEVSYS_PROJECT_ROOT` 与 `HookEnv` 注入规则；`config.workspace_root` / `config.dispatch_command` 解析与默认；M7.1 视图域 `workloom workspace view` 的接线（不经 internal/app、调 internal/view.Build 与 storage.Inspect）与 §4.8 `worktree`（执行工作区）严格分开的边界。；本轮（ca58d27→19b9149，v0.1.9 发布链 + 主命令改名 workloom + Git 可选能力）：非 Git 项目走目录工作区（`ensureDirectory`，`Workspace.Branch` 为空）、`HookEnv` 的 `DEVSYS_BRANCH` 仅在 worktree 形态注入、`run verify` / `run complete` 对非 Git 路径 `Skipped=true` 跳过 Git 证据、`workspace.ErrGitMissing` 只留给 worktree 操作
source_commit: 98c291c
generated: true
generator: repowiki-gen
---

# 执行命令与 DEVSYS_PROJECT_ROOT

本卡描述 M6 执行层（[执行层](../执行层/概述.md)）到 CLI 与 MCP 入口的**接线规则**——每条 CLI / MCP 工具对应到 `internal/app` 的哪个方法、再调哪个执行层子包。这是「CLI 暴露什么、MCP 暴露什么、谁负责语义」的唯一参考。

## 命令族到 Service 方法的映射

| CLI 命令 | MCP 工具 | profile | `internal/app` 方法 | 调用的执行层 |
|---|---|---|---|---|
| `worktree prepare` | `workspace_prepare`（隐含在 `dispatch`） | admin / executor | `Service.WorkspacePrepare(ctx, WorkspacePrepareRequest{WorkItemID, RunID, Branch, Actor, Reason})` | `internal/workspace.Ensure` + `internal/workspace.RunHook(after_create)`；**v0.1.9 起** Git 项目走 worktree、非 Git 项目走目录工作区（`ensureDirectory`，无 branch） |
| `worktree remove` | — | admin | `Service.WorkspaceRemove(ctx, WorkspaceRemoveRequest{WorkItemID, Path, Force, Actor, Reason})` | `internal/workspace.RunHook(before_remove)` + `internal/workspace.Remove` |
| `worktree list` | — | session | `Service.WorkspaceList(ctx, workItemID)` | `internal/workspace.List` |
| `dispatch --once\|--watch\|--dry-run\|--max` | — | admin | `Service.Dispatch(ctx, DispatchRequest{...})` | `internal/dispatch.Plan` + `internal/workspace.Ensure` + `internal/app.dispatchOne`（包含 `internal/prompt.Assemble`） |
| `run exec` | `run_exec` | executor | `Service.RunExec(ctx, RunExecRequest{...})` | `internal/harness.ByName` + `internal/prompt.Assemble` + `internal/harness.Session.Wait` |
| `run prompt` | `run_prompt` | session | `Service.RunPrompt(ctx, RunPromptRequest{...})` | `internal/prompt.Assemble` |
| `run verify` | `run_verify` | session | `Service.RunVerify(ctx, runID)` | `internal/workspace.HeadSHA` / `BranchSHA` + `git rev-parse refs/heads/<branch>`；**v0.1.9 起**非 Git 项目 / 目录工作区返回 `Skipped=true`，不做 Git 比较 |
| `run complete` | `run_complete` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunSucceeded, ...})` | `internal/app.verifyCompletion` 守卫 + `internal/app.recordExecOutcome`；**v0.1.9 起**非 Git 路径跳过 Git 证据（`Skipped`，不把 `Advanced` 标 true） |
| `run fail` | `run_fail` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunFailed, ...})` | `internal/app.finishAttempt` |
| `run cancel` | `run_cancel` | executor | `Service.RunFinish(ctx, RunFinishRequest{Outcome: RunCanceled, ...})` | `internal/app.finishAttempt` |

| `workloom workspace view [--limit N]` | — | — | 直接调 `internal/view.Build(ctx, root, view.Options{Limit: *limit})`（不经 `internal/app`） | `internal/view.Build` + `storage.Inspect` / `storage.InspectUnlocked`（[internal/cli/workspace.go:129-132](file://internal/cli/workspace.go#L129-L132)） |
| `workloom workspace build --static [--out DIR] [--limit N]` | — | — | 直接调 `internal/view.Build` + `sitestatic.Build` 落盘到 `--out`（默认 `.devsys/dist/site/`，不经 `internal/app`） | `internal/view.Build` + `internal/sitestatic.Build`（[internal/cli/workspace.go:44-108](file://internal/cli/workspace.go#L44-L108)） |
| `workloom workspace serve [--host 127.0.0.1] [--port N] [--allow-remote] [--limit N]` | — | — | 直接调 `internal/view.Build` 每请求一次 + `sitestatic.RenderPage` 内存渲染，不写盘（不经 `internal/app`） | `internal/view.Build` + `internal/sitestatic.RenderPage`（[internal/cli/workspace_serve.go:151](file://internal/cli/workspace_serve.go#L151)） |

`run update --status <terminal>` 仍走 `Service.RunUpdate`（M4 引入），但 M6 起 CLI 文档推荐用 `run complete\|fail\|cancel`（语义对应 §4.8 终态）。
## 配置：`config.yaml` 新增键

```yaml
schema_version: 1
workspace_root: /var/workloom/workspaces       # 可选；默认 <project>/.devsys/workspaces
dispatch_command: "workloom run exec --id {run_id} --actor dispatch --reason tick"  # 可选；M6.4 默认；M6.7 后由 harness 取代
```

字段定义见 [`internal/config/config.go:85-92`](file://internal/config/config.go#L85-L92)；白名单见 [`internal/config/validate.go:60-66`](file://internal/config/validate.go#L60-L66)。`init` 写 `config.yaml` 时**不**主动写这两个字段（占位文件保持 `schema_version: 1` 唯一键）；用户按需追加。

| 字段 | 默认 | 触发时机 | 解析函数 |
|---|---|---|---|
| `workspace_root` | `<project>/.devsys/workspaces`（`internal/workspace.Root` 推导） | 每次 `app.RunExec` / `app.Dispatch` / `app.WorkspacePrepare` 调用 | `internal/workspace.Root(projectRoot, configured)` |
| `dispatch_command` | `"workloom run exec --id {run_id} --actor dispatch --reason tick"`（由 `app.dispatchCommandLine` 渲染） | `app.dispatchOne` 选 `spawn` 策略时；任何 workitem 的 `assigned_harness` 为空时使用 | `internal/app.dispatchCommandLine` |

## DEVSYS_PROJECT_ROOT
`internal/cli/cli.go:196-198` 的 `appService()` 读取顺序（**P1 起** 委托 `resolveService`，[internal/cli/roots.go:44-50](file://internal/cli/roots.go#L44-L50)）：

```text
1. 命令行 --project-root <abs path>   (CLI 显式开关，未来扩展)
2. 环境变量 DEVSYS_PROJECT_ROOT（绝对路径）
3. 从当前工作目录向上找最近的 .devsys/（P1 起；用 project.DevsysDirName 探测；找不到回退 cwd）
```

`DEVSYS_PROJECT_ROOT` 在 agent 在 git worktree 内运行时显式指定主仓库根——避免 attempt 的报告写回 worktree 自带的 `.devsys/`。`app.HookEnv` 在 hook 与 attempt 启动前把同一变量注入子进程。**P1 起**第 3 条让「在子目录里跑 `devsys ...`」也能找到项目根（[internal/cli/roots.go:17-42](file://internal/cli/roots.go#L17-L42) `resolveRoot`）：从当前工作目录向上递归找 `project.DevsysDirName`（`.devsys/`），找到就用；走到文件系统根仍没找到时回退 cwd，让 `requireProjectRoot()` 的前置条件错误继续指出操作者所在目录。`archive` / `search` / `config check` / `doctor` / `recover` / `repair` 等命令随之也能从子目录跑。
`DEVSYS_PROJECT_ROOT` 的语义：

- **agent 在 worktree 内运行时**，worktree 自带一份 `.devsys/`（init 在主仓库根创建过）。如果不指定，agent 会把 attempt 的报告写回 worktree 自带的副本，主仓库 `git pull` 之后看不到任何东西。
- **指定 `DEVSYS_PROJECT_ROOT=<主仓库根>`**，agent 的报告路径解析到主仓库的 `.devsys/`，与 dispatch / 主 CLI 的报告落在同一份文件。
- **`internal/workspace.HookEnv`**（[internal/workspace/workspace.go:319-334](file://internal/workspace/workspace.go#L319-L334)）自动注入 `DEVSYS_PROJECT_ROOT=<主仓库根>`——agent 在 hook 脚本与 attempt 进程里看到的环境变量都是同一个值。

```sh
# 一个 worktree 内运行的 agent 想把 attempt 报告写回主仓库
export DEVSYS_PROJECT_ROOT=/path/to/main/repo
workloom --json run exec --id run-... --harness codex --actor me --reason accept
```

## HookEnv 注入

每次 hook（after_create / before_run / after_run / before_remove）与 attempt 启动前，`app.HookEnv` 注入：

| 变量 | 含义 | 来源 |
|---|---|---|
| `DEVSYS_PROJECT_ROOT` | 主仓库根（agent 报告目标） | `app.HookEnv` 由 `appService()` 持有 |
| `DEVSYS_WORKSPACE` | 当前工作区路径（worktree 或目录工作区） | `workspace.Ensure` 写入 |
| `DEVSYS_BRANCH` | `devsys/<key>` 分支名；**仅 worktree 形态**，目录工作区不注入 | `workspace.branch`（[internal/workspace/workspace.go:356-370](file://internal/workspace/workspace.go#L356-L370)） |
| `DEVSYS_WORKITEM` | 工作项标识符 | `WorkspacePrepareRequest.WorkItemID` |
| `DEVSYS_HOOK` | 触发的 hook 名 | `workspace.RunHook` 显式传入 |

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
- `internal/app.dispatchOne` 选 spawn 策略：① `WorkItem.AssignedHarness` 非空 → 调 `ByName`；② 否则 `config.DispatchCommand` 模板渲染（默认模板即 CLI 调 `workloom run exec`）——这是 M6.4 默认路径。
- `Probe` 由 cliAdapter 跑 `codex --version` / `opencode --version` / `claude --version`，超时 10s（`probeTimeout`）；不静默替换为其它 harness。

## M7.1 视图域：与 §4.8 `worktree` 的严格边界

M7.1 把方案 §17「视图域」落在 `internal/view` 包 + `internal/cli/workspace.go`，与 M6 §4.8 `worktree`（执行工作区）共享 **`workspace` 这个英文名**但语义不同——`runWorkspace` 路由 `view` 子命令调 `internal/view.Build`，**不**走 `internal/app`；`runWorktree` 路由 `prepare|remove|list` 走 `app.WorkspacePrepare/Remove/List`，跑 git worktree + 生命周期钩子 + claim_head。

| 维度 | `worktree`（执行工作区，§4.8） | `workspace view`（视图域，§17） | `workspace build` / `workspace serve`（视图域，§17） |
|---|---|---|---|
| 写盘？ | 是（`prepare` / `remove`） | **否**（只读聚合） | `build` 仅 `--out` 下（默认 `.devsys/dist/site/`），`serve` 否 |
| 锁策略 | `app.WorkspacePrepare` 走完整事务路径（`storage.Recover` + 写） | `storage.Inspect`（共享锁，**绝不创建**）→ `storage.InspectUnlocked` 退路（advisory） | 同 `view`（共享锁，**绝不创建**） |
| 触发 lifecycle hook | 是（`after_create` / `before_run` / `after_run` / `before_remove`） | 否 | 否 |
| 写 `.devsys/.cache/` | 间接（workflow LKG 缓存路径） | **绝不** | **绝不** |
| 写 `.devsys/local/` | 是（hook 输出、attempt 输出、prompt round 落盘） | **绝不读**，更不写 | **绝不读**，更不写 |
| 恢复事务 / 修断尾 | 否（M6 留给 `dispatch --once`） | **绝不**（视图只读路径） | **绝不** |
| 退出码 | `app.Error.Class()` 翻译（0/2/3/1/4） | 沿用 0/2/3/1（10/11 仍专属 `knowledge status`） | 同 `view` |
| 是否依赖 `DEVSYS_PROJECT_ROOT` | 是（attempt 内 agent 报告目标） | 否（视图是只读聚合，无需注入） | 否 |
| Git 是否必需 | **v0.1.9 起否**：Git 项目 → worktree；非 Git → 目录工作区（`Workspace.Branch` 为空） | 否（只读聚合） | 否 |
| 是否走 `internal/app` | 是 | **否**（直调 `view.Build`） | **否**（直调 `view.Build` + `sitestatic.Build`/`RenderPage`） |

视图域独有的契约：

- `view.Build(ctx, root, opts)` 只读；`opts.Now` 仅用于 readiness 求值，不进模型。
- `view.Model` 无墙钟字段；同一状态两次构建**逐字节一致**。
- `trust.state ∈ {ok, advisory_unlocked, pending_transaction}`，`advisory_unlocked` / `pending_transaction` 都算 exit 0（语义通过 `trust` / `pending` 字段告诉调用方）。
- `knowledge.status ∈ {fresh, stale, missing, unavailable}`，与 `workloom knowledge status` 的 0/10/11 退出码**不**映射——退出码表里 10/11 仍只承载 knowledge status。

开发期不变量（PR review 会驳回的几种形态）：

- 把 `runWorkspace` 重构进 `internal/app`：拒绝。视图只读，应保持独立包、可静态分析不依赖业务包。
- 把 `runWorktree` 接到 `view.Model` 或反过来：拒绝。`worktree` 操作需要事务与钩子；视图是只读聚合。
- 把 `view.Model` 的 `advisory_unlocked` 升级为 exit 3：拒绝。`advisory` 是事实标签而非错误，让调用脚本按 `trust.state` 分支。

## 与其他卡的关系

- [架构设计](./架构设计.md) — M6 CLI 进一步承载执行层命令；MCP 与 CLI 共用同一 Service
- [特殊配置与命令](./特殊配置与命令.md) — CLI 命令族详细剧本（含 M6 七族与 M7.1 workspace view）
- [Schema 与错误契约](./Schema与错误契约.md) — `workspace_root` / `dispatch_command` 字段白名单与错误语义
- [共享应用与 MCP](../共享应用与MCP/工具与Profile.md) — MCP `run_prompt` / `run_verify` 是 session profile；`run_complete` / `run_fail` / `run_cancel` 是 executor profile
- [执行层 · 架构设计](../执行层/架构设计.md) — 五子包依赖方向 + Plan 纯函数 + Retry/Prompt 可复现
- [视图层 · 概述](../视图层/概述.md) — M7.1 视图域只读入口：`workspace view` 与 §4.8 `worktree`（执行工作区）的严格边界

## 本批（M9 #336/#337 提交 362b637 / 7e08e3d）

- `workloom workflow init --template <id>`（[internal/cli/cli.go:1212-1243](file://internal/cli/cli.go#L1212-L1243)）：usage 文本由 `strings.Join(app.WorkflowTemplates(), "|")` 动态生成（`internal/app/workflow_init.go` + 仓库根 `templates.go`）；CLI 路由调用 `svc.WorkflowInitTemplate(ctx, id)`——存储 `Write` 的 `ExpectAbsent` 保证不覆盖已有策略文件；返回值 `WorkflowInitView{Template, Path}` 暴露写到的相对路径。
- `workloom project update --blueprint-artifact <artifact-id>`（[internal/cli/cli.go:721-747](file://internal/cli/cli.go#L721-L747)）fs.Visit 模式 flag：empty string clears；非空须 `record.KindArtifact.ValidID`（形态错 → `Usagef` / `CodeUsage = 2`），未注册（`GetArtifact` ErrNotFound → `Preconditionf` / `CodePrecondition = 3` + 出路 `workloom artifact register`）。MCP `project_update` 输入增 `blueprint_artifact_id`（[internal/mcp/tools_project.go](file://internal/mcp/tools_project.go)）。接线链：CLI flag → `app.ProjectUpdate` → `UpdateProjectRequest.BlueprintArtifactID *string` → storage 写事务。
- `workloom mcp serve` 0 工具分支（[internal/cli/mcp.go:79-91](file://internal/cli/mcp.go#L79-L91)）：构造 cfg 后调 `mcp.VisibleTools(cfg)`，拿到空 slice 走 `errUsage("mcp serve: %s %s %s no tools in tier %s; use --tier standard", word, strings.Join(quoted, ", "), verb, tier)`（单 profile 渲染为 `mcp serve: profile "session" has no tools in tier core; use --tier standard`） → `CodeUsage = 2`。多 profile 时单复数与动词变 `profiles %s have`。避免 silent server 的隐藏陷阱。
- `app.MCPSnippet` 三参签名（[internal/app/mcpsnippets.go:19](file://internal/app/mcpsnippets.go#L19)）：`MCPSnippet(name, devsysBin, projectRoot)`；`bin` 与 `cwd` 都 `filepath.ToSlash(strings.TrimSpace(…))`；`jsonString` 走 `encoding/json` 编字符串。`internal/cli/cli.go` 旧 `MCPSnippet(name, os.Args[0])` 替换为 `(name, os.Args[0], svc.Root)`。
- `workloom next` / `prime` / `session start` / `project status` 在 `len(doc.InvalidFiles) > 0` 时（[internal/app/next.go:28-46](file://internal/app/next.go#L28-L46)）→ `Invalidf(KindInvalid, nil, "next: managed state unreadable: <file>: <err>…")`，调用 `next.Evaluate` 之前短路。`next` 调度入口（`runNext` [internal/cli/cli.go:1679-1728](file://internal/cli/cli.go#L1679-L1728)）的 `verdict` / `reasons` / `risks` 在不可信路径下整体被替换为 untrusted state 消息，调用脚本按 `code == 4` 分支触发 `workloom recover --actor … --reason …`。
- `RiskEmptyProject = "empty_project"`（[internal/next/evaluate.go:52](file://internal/next/evaluate.go#L52)）+ `CreateWorkitemCommand` 常量（[internal/next/evaluate.go:64-66](file://internal/next/evaluate.go#L64-L66)）：空项目 + `!HasBlueprint` 时 `next` 推荐两条 remedy——先 `workloom workitem create --title "…" --actor <you> --reason "first task"`，再 `workloom project update --blueprint-artifact <artifact-id>`。同一文案被 `workloom init` 在 `runInit` 人类输出的「next:」四步里复用（[internal/cli/cli.go:431-437](file://internal/cli/cli.go#L431-L437)）。

## 本批（v0.1.9，ca58d27→19b9149）

### Git 可选能力：执行工作区两种形态

- **Git 项目**：`workspace.Ensure` 走 `ensureWorktree`——建/复用 `devsys/<key>` 分支的 git worktree，`after_create` 钩子只在创建时跑（[internal/workspace/workspace.go:79-145](file://internal/workspace/workspace.go#L79-L145)）。
- **非 Git 项目**：走 `ensureDirectory`——在 `workspace_root` 下建普通目录，`Workspace.Branch` 为空、不建分支、不做 worktree 反注册；`after_create` 失败会把目录删掉（不留半成品）（[internal/workspace/workspace.go:147-176](file://internal/workspace/workspace.go#L147-L176)）。
- `workspace.List` 在非 Git 项目下 branch 表为空，`Remove` 跳过 `removeWorktree` / `pruneWorktrees`（[internal/workspace/workspace.go:288-330](file://internal/workspace/workspace.go#L288-L330)）。
- **`DEVSYS_BRANCH` 只在 worktree 形态注入**：`workspace.HookEnv(projectRoot, workspacePath, branch, identifier, hookName)` 在 `branch == ""` 时不追加该变量（[internal/workspace/workspace.go:356-370](file://internal/workspace/workspace.go#L356-L370)）；`DEVSYS_PROJECT_ROOT` / `DEVSYS_WORKSPACE` 恒注入。目录工作区的 attempt 不该指望 `DEVSYS_BRANCH` 存在。
- `workspace.ErrGitMissing`（`git is not available on PATH`）现在只在 worktree 路径上有意义；`init` 与目录工作区都不需要 git（[internal/workspace/git.go:12-14](file://internal/workspace/git.go#L12-L14)）。

### `run verify` / `run complete` 的跳过语义

`verifyCompletion` 先判定「目录工作区（`Workspace.Path != "" && Branch == ""`）或项目不在 git work tree 内」，命中即返回 `CompletionCheck{Skipped: true, Advanced: false, Reason: "git completion check is not applicable"}`——**skip 不是 verified advance**（[internal/app/runverify.go:45-56](file://internal/app/runverify.go#L45-L56)）。`run complete`（`Outcome == RunSucceeded`）的守卫分支是 `case check.Advanced, req.Force, check.Skipped:`——**`Skipped` 放行**，所以非 Git 项目 / 目录工作区可以正常完成 attempt（[internal/app/run.go:318-325](file://internal/app/run.go#L318-L325)）。放行时刻意留痕：记录里 `verification.advanced = false`、`head_sha_at_complete` 为空，事件类型仍是普通 `run_finished`（只在真正 verified 时才是 `completion_verified`），事件内容后缀是 `(git check not applicable, head none)`（[internal/app/run.go:356-372](file://internal/app/run.go#L356-L372)）。Git + 已绑定 worktree 的完成门禁**完全不变**：未 advance 且无 `--force` → exit 4 + run `failed` + workitem 进 `review`。
- **本批（#398）**：`workloom --help` 顶层命令表补回 `init` 与 `sync status` 两行（[internal/cli/cli.go:65](file://internal/cli/cli.go#L65)、[internal/cli/cli.go:83](file://internal/cli/cli.go#L83)）——两条命令一直可路由（`setup` 第一步就是 `init`，`sync status` 是 M8.1 只读接力判定），e4f1a9e 重写命令表时漏列；纯文案、无行为变更。本页引用的 `internal/cli/cli.go` 行号已按 +2 位移重算（`init` 之前不变，`init` 与 `sync status` 之间 +1，其后 +2）。
