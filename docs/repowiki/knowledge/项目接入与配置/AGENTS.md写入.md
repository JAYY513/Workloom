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
  - 块标记
  - --dry-run
description: M4 `devsys wire` 把 devsys 纪律块注入 AGENTS.md；`<!-- devsys:begin/end -->` 标记、幂等（三次运行字节一致）、区段外字节保留（含其它工具的管理块）、`--dry-run` 预览
generated: true
source_commit: 997c5f8
generator: repowiki-gen
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
