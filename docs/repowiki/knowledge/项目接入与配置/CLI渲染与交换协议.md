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
description: M4 CLI 的 `--json` / `--jsonl` 两种结构化输出形态、`writeJSONL` 列表行流、退出码与 MCP 工具错误的对应；知识层保留的 10/11 约定
generated: true
source_commit: 997c5f8
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

`--quiet` 抑制这些文本；`--json` / `--jsonl` 取代。

## 与其他层的关系

- [架构设计](./架构设计.md) — 命令分发 + `render` + `toCoded`
- [共享应用与 MCP 错误语义](../共享应用与MCP/错误语义.md) — `*app.Error.Class()` 是双入口错误翻译的源头
