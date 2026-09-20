---
status: stable
type: module
dimension: agents_wire
triggers:
  - devsys wire
  - AGENTS.md 管理块
  - 幂等写入
  - 手写保留
  - devsys:begin
  - devsys:end
description: M4 `devsys wire` 把 devsys 纪律块注入 AGENTS.md；`<!-- devsys:begin/end -->` 标记、幂等（三次运行字节一致）、区段外字节保留（含其它工具的管理块）、`--dry-run` 预览；P1 起 `wire --check`（只读环境报告）/ `wire --skill`（写 `.agents/skills/devsys/{SKILL.md,references/cli.md,references/troubleshooting.md}`）/ `wire --print-mcp <codex|claude|opencode>`（生成 MCP 客户端 stdio 片段）
  - --dry-run
generated: true
source_commit: 2b8b8ce
---


# AGENTS.md 写入 · 项目接入与配置

M4.6 起 `devsys wire` 把「devsys 项目状态与读取纪律」以管理块形式注入仓库根的 `AGENTS.md`。该命令**幂等**：连续三次运行后文件字节相同；区段外（包含其它工具的管理块）字节保留。

## 管理块标记

`internal/app/wire.go:14-16`：

```go
const (
    wireBegin = "<!-- devsys:begin | managed by `devsys wire`; edits inside this block are overwritten -->"
    wireEnd   = "<!-- devsys:end -->"
)
```

devsys 只拥有这两个标记**之间**的字节。其它工具的同类管理块（如 `repowiki` 的 `<!-- repowiki:begin/end -->`）与手写内容不会被修改。

## 纪律块内容

`internal/app/wire.go:18-32` 的 `wireBlock`：

```markdown
## devsys（项目状态与读取纪律）

- 项目状态保存在仓库内 `.devsys/`，它是唯一事实来源；不要直接编辑受管文件（修复请用 `devsys repair`）。
- 查询与变更通过 CLI（`devsys …`）或 MCP（`devsys mcp serve`，工作目录 = 项目根）；两者共用同一应用服务，约束一致。
- 先读后写：写操作携带版本哈希（`--expect` / 工具的 `expect`），过期哈希一律拒绝。
- 入口：`devsys session start` 一次给出项目状态与下一步；`devsys next` 给出就绪判定与推荐动作；`devsys project status` 给出计数与风险。
- 结构化输出：`--json`（单文档信封）或 `--jsonl`（列表一行一条记录）；退出码 0/1/2/3/4（10/11 预留给知识层）。
```

纪律描述与 MCP 服务自身描述一致——同一段事实，CLI 与 MCP 给 agent 的入口都一样。

## 写入流程

`internal/app/wire.go:44-77` 的 `Wire(dryRun bool)`：


1. `readAgentsFile(path)` 读现有文件 + mode（保留文件权限位）。
2. `spliceWireBlock(string(existing))` 替换 `<!-- devsys:begin/end -->` 区间：
   - 区间内：替换为 `wireBlock`。
   - 区间外：原样保留。
3. 若无变化 → 返回 `WireView{Changed:false}`；等价于「already wired」消息。
4. `dryRun=true` → 返回 `WireView{Diff: unifiedDiff(...)}`，**不**写盘。
5. 否则 `writeFileAtomic(path, updated, mode)` 原子写。

## P1 增量：`wire --check` / `--skill` / `--print-mcp`

P1（6bd253c）把 `devsys wire` 从「只写纪律块」扩展为「agent 接入三件套」：三种新形态走同一 `runWire`（[internal/cli/cli.go:413-523](../../../internal/cli/cli.go#L413-L523)），与原 `--dry-run` **互斥**。

- `wire --check`（**只读**，exit 0）：调 `svc.WireCheck()`（[internal/app/wirecheck.go:22-39](../../../internal/app/wirecheck.go#L22-L39) `WireCheckView{Lines []WireCheckLine}`）报告八项环境就绪状态：`go` / `git` / `.devsys` / `schema` / `registry` / `AGENTS.md` / `skill` / `mcp`。人类输出 `[v] <name>: <detail>` 或 `[x] <name>: <detail>`；`--json` 信封 `{ok, ...WireCheckView}`。
- `wire --skill`（**写操作**）：调 `svc.WriteSkill()`（[internal/app/skill.go](../../../internal/app/skill.go)）写 `.agents/skills/devsys/{SKILL.md, references/cli.md, references/troubleshooting.md}` 三文件，每文件带 `<!-- devsys-skill -->` 标记。**手写无 marker 的文件不覆盖**（已存在且无 marker → 跳过，报告 `hand-written`）；重复运行 → `skill: already installed (no change)`。
- `wire --print-mcp <codex|claude|opencode>`：调 `app.MCPSnippet(name, os.Args[0])`（[internal/app/mcpsnippets.go:12-47](../../../internal/app/mcpsnippets.go#L12-L47)）stdout 打印 stdio MCP 客户端片段。codex 走 `-c mcp_servers.devsys.*` 注入；claude 走 `.mcp.json` 的 `mcpServers.devsys`；opencode 走 `opencode.json` 的 `mcp.devsys`。`--json` 信封 `{ok, harness, snippet}`。未知 harness → `app.Usagef("unknown harness %q (expected codex, claude or opencode)")` → `CodeUsage = 2`。

互斥矩阵：`--check` / `--skill` / `--print-mcp` 三选一；`--check` 与 `--print-mcp` 不接受任何其它 flag（`--dry-run` / `--skill`）；`--skill` 与 `--dry-run` 互斥。互斥规则在 `runWire` 顶部集中校验（[internal/cli/cli.go:441-447](../../../internal/cli/cli.go#L441-L447)）。
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
- 错误码：`CodePrecondition = 3`，`kind="precondition"`，提示「devsys wire block is malformed」。

行为：拒绝猜测，避免在破损的块上做半截替换。

## `WireView` 视图

`internal/app/wire.go:34-42`：

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

## 与其他层的关系

- [架构设计](./架构设计.md) — `wire` 命令在 CLI 层的入口（`runWire`，[internal/cli/cli.go:371-408](file://internal/cli/cli.go#L371-L408)）
- [共享应用与 MCP 概述](../共享应用与MCP/概述.md) — `app.Wire` 是 MCP 不暴露的工具之一（CLI-only operator 行为；`方案 §8.2` 无 MCP 对应）
