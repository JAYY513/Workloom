---
status: stable
type: module
dimension: agents_wire
triggers:
  - workloom wire
  - AGENTS.md 管理块
  - 幂等写入
  - 手写保留
  - devsys:begin
  - devsys:end
  - --dry-run
  - MCPSnippet 三参
  - projectRoot
  - Windows 转义
  - jsonString
  - workloom setup
  - wire 默认写 skill
  - wire --check git 能力缺失
description: "M4 `workloom wire` 把 devsys 纪律块注入 AGENTS.md；`<!-- devsys:begin/end -->` 标记、幂等（三次运行字节一致）、区段外字节保留（含其它工具的管理块）、`--dry-run` 预览；P1 起 `wire --check`（只读环境报告）/ `wire --skill`（写 `.agents/skills/devsys/{SKILL.md,references/cli.md,references/troubleshooting.md}`）/ `wire --print-mcp <codex|claude|opencode>`（生成 MCP 客户端 stdio 片段）；本轮（ca58d27→19b9149，v0.1.9 发布链 + 主命令改名 workloom + Git 可选能力）：纪律块标题与示例命令改名 `workloom`（标记常量仍为 `<!-- devsys:begin/end -->`）、`wire --check` 的 git 项记为可选能力缺失（`OK: true`）、`setup` 默认调用 `wire` + `WriteSkill`"
generated: true
source_commit: 705cb18
---


# AGENTS.md 写入 · 项目接入与配置

M4.6 起 `workloom wire` 把「devsys 项目状态与读取纪律」以管理块形式注入仓库根的 `AGENTS.md`。该命令**幂等**：连续三次运行后文件字节相同；区段外（包含其它工具的管理块）字节保留。

## 管理块标记

`internal/app/wire.go:13-16`：

```go
const (
    wireBegin = "<!-- devsys:begin | managed by `devsys wire`; edits inside this block are overwritten -->"
    wireEnd   = "<!-- devsys:end -->"
)
```

**本批修正**：标记文案里的命令名仍是 `devsys wire`（[internal/app/wire.go:14](file://internal/app/wire.go#L14)）——`BEGIN`/`END` 标记本身是本轮**刻意保留原名**的部分，改名只发生在纪律块**内容**与 CLI 输出。

devsys 只拥有这两个标记**之间**的字节。其它工具的同类管理块（如 `repowiki` 的 `<!-- repowiki:begin/end -->`）与手写内容不会被修改。

## 纪律块内容

`internal/app/wire.go:22-30` 的 `wireBlock`（**本批**：标题与示例命令随主命令改名同步为 `workloom`）：

```markdown
## workloom（项目状态与读取纪律）

- 项目状态保存在仓库内 `.devsys/`，它是唯一事实来源；不要直接编辑受管文件（修复请用 `workloom repair`）。
- 查询与变更通过 CLI（`workloom …`）或 MCP（`workloom mcp serve`，工作目录 = 项目根）；两者共用同一应用服务，约束一致。
- 先读后写：写操作携带版本哈希（`--expect` / 工具的 `expect`），过期哈希一律拒绝。
- 入口：`workloom session start` 一次给出项目状态与下一步；`workloom next` 给出就绪判定与推荐动作；`workloom project status` 给出计数与风险。
- 详细工作流见 `.agents/skills/devsys/SKILL.md`（MCP 不可用时用 `workloom --json`）。
```

纪律描述与 MCP 服务自身描述一致——同一段事实，CLI 与 MCP 给 agent 的入口都一样。

## 写入流程

`internal/app/wire.go:44-74` 的 `Wire(ctx, dryRun)`：


1. `readAgentsFile(path)` 读现有文件 + mode（保留文件权限位）。
2. `spliceWireBlock(string(existing))` 替换 `<!-- devsys:begin/end -->` 区间：
   - 区间内：替换为 `wireBlock`。
   - 区间外：原样保留。
3. 若无变化 → 返回 `WireView{Changed:false}`；等价于「already wired」消息。
4. `dryRun=true` → 返回 `WireView{Diff: unifiedDiff(...)}`，**不**写盘。
5. 否则 `writeFileAtomic(path, updated, mode)` 原子写。

## P1 增量：`wire --check` / `--skill` / `--print-mcp`

P1（6bd253c）把 `workloom wire` 从「只写纪律块」扩展为「agent 接入三件套」：三种新形态走同一 `runWire`（[internal/cli/cli.go:445-584](file://internal/cli/cli.go#L445-L584)），与原 `--dry-run` **互斥**。

- `wire --check`（**只读**，exit 0）：调 `svc.WireCheck()`（[internal/app/wirecheck.go:22-40](file://internal/app/wirecheck.go#L22-L40) `WireCheckView{Lines []WireCheckLine}`）报告八项环境就绪状态：`go` / `git` / `.devsys` / `schema` / `registry` / `AGENTS.md` / `skill` / `mcp`。人类输出 `[v] <name>: <detail>` 或 `[x] <name>: <detail>`；`--json` 信封 `{ok, ...WireCheckView}`。
- `wire --skill`（**写操作**）：调 `svc.WriteSkill()`（[internal/app/skill.go](file://internal/app/skill.go)）写 `.agents/skills/devsys/{SKILL.md, references/cli.md, references/troubleshooting.md}` 三文件，每文件带 `<!-- devsys-skill -->` 标记。**手写无 marker 的文件不覆盖**（已存在且无 marker → 跳过，报告 `hand-written`）；重复运行 → `skill: already installed (no change)`。
- `wire --print-mcp <codex|claude|opencode>`：调 `app.MCPSnippet(name, app.MCPCommandFor(os.Args[0]), svc.Root)`（[internal/app/mcpsnippets.go:21-88](file://internal/app/mcpsnippets.go#L21-L88)）stdout 打印 stdio MCP 客户端片段。codex 走 `-c mcp_servers.devsys.*` 注入；claude 走 `.mcp.json` 的 `mcpServers.devsys`；opencode 走 `opencode.json` 的 `mcp.devsys`。`--json` 信封 `{ok, harness, snippet}`。未知 harness → `app.Usagef("unknown harness %q (expected codex, claude or opencode)")` → `CodeUsage = 2`。**v0.1.12 起** 第二参是解析后的 `app.MCPCommand`（[internal/cli/cli.go:468](file://internal/cli/cli.go#L468)）：npm 安装树内片段写 `command = "node"` + `args = ["<wrapper>/bin/workloom.js", "mcp", "serve", …]`，源码/Release/`go install` 装机仍是二进制绝对路径（[internal/app/mcpcommand.go:53-74](file://internal/app/mcpcommand.go#L53-L74)）；args 统一由 `cmd.ServeArgs(...)` 生成，`Optional npx form` 注释行保留（[internal/app/mcpsnippets.go:26](file://internal/app/mcpsnippets.go#L26)、[internal/app/mcpsnippets.go:24](file://internal/app/mcpsnippets.go#L24)）。

互斥矩阵：`--check` / `--skill` / `--print-mcp` 三选一；`--check` 与 `--print-mcp` 不接受任何其它 flag（`--dry-run` / `--skill`）；`--skill` 与 `--dry-run` 互斥。互斥规则在 `runWire` 顶部集中校验（[internal/cli/cli.go:453-462](file://internal/cli/cli.go#L453-L462)）。

**v0.1.4（#338/#342）增量**：`wire --check` 增 `--strict`——任一检查项失败时 exit `CodePrecondition = 3`（缺省 `--check` 恒 exit 0）；`--strict` 不带 `--check` → `errUsage`。默认 `workloom wire`（无 flag）在写纪律块的同时**一并写 skill 三文件**（`skillChanged` 并入 `WireView` JSON 的 `skill_changed` 字段与人类输出 `skill: wrote …` 行）——AGENTS.md 块指向缺失的 SKILL.md 是破损安装（#338 C1），默认形态必须落到块所声称的状态；`--dry-run` 仍只预览纪律块 diff。

## 幂等保证

测试 `TestWireIsIdempotentAndPreservesHandwritten`（`internal/cli/wire_test.go:27-71`）：

1. 写入手写 AGENTS.md（含 `repowiki:begin/end` 块与「保留我」段落）。
2. 第一次 `wire` → 成功，含 `devsys:begin/end` 块；手写内容与 `repowiki` 块**逐字**保留。
3. 第二/三次 `wire` → 返回 `already wired`；文件**字节级**一致（`got != first` 即失败）。

## 拒绝形态

`TestWireRejectsMalformedBlock`（`internal/cli/wire_test.go:89-122`）：

- 仅存在 `begin` 没有 `end`：失败。
- 仅存在 `end` 没有 `begin`：失败。
- 块嵌套（`begin ... begin ... end ... end`）：失败。
- 错误码：`CodePrecondition = 3`，`kind="precondition"`，提示「workloom wire block is malformed」。

行为：拒绝猜测，避免在破损的块上做半截替换。

## `WireView` 视图

`internal/app/wire.go:33-38`：

```go
type WireView struct {
    Path    string `json:"path"`
    Changed bool   `json:"changed"`
    Created bool   `json:"created"`
    Diff    string `json:"diff,omitempty"`
}
```

`--json` 模式输出（无 `Changed` 时不含 `diff`）：

```json
{
  "path": "C:/repo/AGENTS.md",
  "changed": true,
  "created": false,
  "diff": "--- AGENTS.md\n+++ AGENTS.md\n@@ ...\n+<!-- devsys:begin ... -->..."
}
```

## 本批（M9 #336/#337 提交 362b637 / 7e08e3d）

- `wire --print-mcp <codex|claude|opencode>` 调用的 `app.MCPSnippet` 从双参签名改为三参 `MCPSnippet(name, devsysBin, projectRoot)`（[internal/app/mcpsnippets.go:21](file://internal/app/mcpsnippets.go#L21)）：新增 `projectRoot string` 参数（`internal/cli/cli.go` 传入 `svc.Root`）；`bin` 与 `cwd` 都走 `filepath.ToSlash(strings.TrimSpace(…))` 归一 Windows `\` 为 `/`（[internal/app/mcpsnippets.go:25-32](file://internal/app/mcpsnippets.go#L25-L32)）；`jsonString` 内部统一走 `encoding/json` 编字符串（不再手写 `\\"` 转义，避免 Windows 路径里偶发的 `\"` 截断）。codex 走 `-c mcp_servers.devsys.*` 注入；claude 走 `.mcp.json` 的 `mcpServers.devsys`；opencode 走 `opencode.json` 的 `mcp.devsys`；`cwd` 填真实项目根（不是 `<project-root>` 占位），agent 拉起时不需再二次改 snippet。
- `DEVSYS_CONFIG_DIR` 已设时三片段（codex / claude / opencode）都带 `env`：`codex` 走 `-c mcp_servers.devsys.env.DEVSYS_CONFIG_DIR=…`；`claude` 走 `.mcp.json` 的 `env.DEVSYS_CONFIG_DIR`；`opencode` 走 `opencode.json` 的 `environment.DEVSYS_CONFIG_DIR`。`internal/cli/cli.go` 的 `wire --print-mcp` 仍是只读、不写盘、不发 RPC；agent 复制到客户端配置文件即生效。

## 与其他层的关系

- [架构设计](./架构设计.md) — `wire` 命令在 CLI 层的入口（`runWire`，[internal/cli/cli.go:445-584](file://internal/cli/cli.go#L445-L584)）
- [共享应用与 MCP 概述](../共享应用与MCP/概述.md) — `app.Wire` 是 MCP 不暴露的工具之一（CLI-only operator 行为；`方案 §8.2` 无 MCP 对应）

## 本批（v0.1.9，ca58d27→19b9149）

- **纪律块文案随主命令改名**：`wireBlock` 的标题从 `## devsys（项目状态与读取纪律）` 改为 `## workloom（项目状态与读取纪律）`，块内示例命令（`workloom repair` / `workloom …` / `workloom mcp serve` / `workloom session start` / `workloom next` / `workloom project status` / `workloom --json`）全部同步（[internal/app/wire.go:22-30](file://internal/app/wire.go#L22-L30)）。**标记常量不变**：`wireBegin` 仍是 `<!-- devsys:begin | managed by \`devsys wire\`; … -->`、`wireEnd` 仍是 `<!-- devsys:end -->`（[internal/app/wire.go:13-16](file://internal/app/wire.go#L13-L16)）——既有 AGENTS.md 不需要重写，幂等性不受影响。
- **拒绝形态的文案带新命令名**：`AGENTS.md carries more than one devsys begin marker; … rerun \`workloom wire\``、`AGENTS.md carries a malformed devsys block …`、`AGENTS.md has the devsys end marker before its begin marker …`（[internal/app/wire.go:110-146](file://internal/app/wire.go#L110-L146)）；错误码仍是 `CodePrecondition = 3` / `kind="precondition"`。
- **`wire --check` 的 git 项改为能力缺失而非失败**：新增 `checkGit()`——`git` 不在 PATH 时返回 `OK: true` + detail `not on PATH (optional: worktree, sync, knowledge freshness unavailable)`；其余项文案中的出路命令也改成 `workloom init` / `workloom wire --skill` / `workloom wire --print-mcp …`（[internal/app/wirecheck.go:49-54](file://internal/app/wirecheck.go#L49-L54)、[internal/app/wirecheck.go:64](file://internal/app/wirecheck.go#L64)、[internal/app/wirecheck.go:114-134](file://internal/app/wirecheck.go#L114-L134)）。
- **`workloom setup` 复用同一写路径**：setup 的 wire 步骤调 `Service.Wire(ctx, false)` + `Service.WriteSkill()`，detail 形如 `created AGENTS.md; skill wrote N files` / `already wired; skill already installed`（[internal/app/setup.go:84-106](file://internal/app/setup.go#L84-L106)）；skill 三文件内容同步改写为 `workloom`（[internal/app/skill.go:26-104](file://internal/app/skill.go#L26-L104)，`.agents/skills/devsys/` 路径与 `<!-- devsys-skill -->` 标记不变）。
- **本批（#398）**：`workloom --help` 顶层命令表补回 `init` 与 `sync status` 两行（[internal/cli/cli.go:65](file://internal/cli/cli.go#L65)、[internal/cli/cli.go:83](file://internal/cli/cli.go#L83)）——两条命令一直可路由（`setup` 第一步就是 `init`，`sync status` 是 M8.1 只读接力判定），e4f1a9e 重写命令表时漏列；纯文案、无行为变更。本页引用的 `internal/cli/cli.go` 行号已按 +2 位移重算（`init` 之前不变，`init` 与 `sync status` 之间 +1，其后 +2）。
- **本批（v0.1.10）**：补丁版本发布（`--help` 顶层命令表修复，无行为变更）；本页口径不变，`source_commit` 跟进至 `21a9e71`。
- **本批（v0.1.11 / npm 首发）**：`wire --print-mcp` 生成的片段里，npx 可选注释行的包名同步为 `@kaki317/workloom`（[internal/app/mcpsnippets.go:24](file://internal/app/mcpsnippets.go#L24)）——本地二进制行不变，npx 仍只是可选补充（冷启动要联网、版本随缓存漂移）；本页其余口径不变，`source_commit` 跟进至 `238e30c`。
- **本批（v0.1.12 / MCP 接入闭环）**：`wire --print-mcp` 的第二参从「二进制路径字符串」变成解析后的 `app.MCPCommand`——CLI 传 `app.MCPCommandFor(os.Args[0])`（[internal/cli/cli.go:468](file://internal/cli/cli.go#L468)），`ResolveMCPCommand` 在 npm 安装树内返回 `node` + `<wrapper>/bin/workloom.js`、其余装机形态原样返回二进制路径（[internal/app/mcpcommand.go:53-74](file://internal/app/mcpcommand.go#L53-L74)）；片段里的 `command` / `args` 因此随安装渠道变化，`args` 统一由 `cmd.ServeArgs(...)` 生成（[internal/app/mcpsnippets.go:26](file://internal/app/mcpsnippets.go#L26)）。`Optional npx form` 注释行仍保留。`mcp install` 写出的三个客户端条目与片段同源（[internal/app/mcpinstall.go:85](file://internal/app/mcpinstall.go#L85)），且写入后会现场探一次服务（`probe:` 行，`--apply` 失败 exit 3、配置保留）。`source_commit` 跟进至 `7cdd918`。
- **本批（v0.1.13 / #414+#416）**：`wire --print-mcp` 生成的片段里 core 档说明去掉过期的「19-tool」硬编码计数（实际 20 项），改为不含数字的描述——`Default --tier core is the daily subset. run_update / run_fail / workitem_block need the CLI or --tier standard.`（[internal/app/mcpsnippets.go:22-23](file://internal/app/mcpsnippets.go#L22-L23)）；三形态片段结构、npx 可选注释行、`mcp install` 同源条目，以及纪律块标记 / 幂等性均不变。
