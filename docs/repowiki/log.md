## 2026-09-18 · cf7b256 M3 增量刷新

- 源码基线：`cf7b256`（M3：策略/工作流/审批/next；相对上次基线 1 提交、19 文件受影响；任务 #163）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（116 个扫描文件、多层目录），scope 增补 `internal/{workflow,approval,next}/**`、`scripts/**`、`docs/examples/workflows/**`；两模块与三篇文章的目录与文件路径保持不变；coverage 109/116 = 94.0%（未覆盖 6 个文件同前 + `docs/examples/workflows/.gitignore` 为空占位）。
- 增量范围：durable-storage 模块全部 9 张知识卡刷新覆盖 M3 内容（概述、架构设计、领域记录与事件、事务与恢复、存储错误语义、状态机与调度、对账与修复、编码规范、特殊配置与命令）；project-access 模块全部 5 张知识卡刷新（概述、架构设计、Schema 与错误契约、技术栈、编码规范、特殊配置与命令）。M3 未引入新文章、未引入新模块——把 workflow/approval/next 折入既有模块（避免破坏路径稳定性）。
- 核心内容新增：策略 schema 与 located 校验（`internal/workflow/parse.go`）、条件白名单 7 字段与运算符规则（`condition.go`）、门禁与质量门确定性评分（`gate.go` + `quality.go`）、last-known-good 缓存与 fail-closed 解析（`cache.go`）、模板与环境变量展开（`template.go`）；审批请求/决定/消费/失效完整生命周期（`approval.go`），`scope=stage_gate` 拒绝走 `rejectStageGate` 在转换事务的 Guard 内原子完成决定与状态变更；就绪判定与 §7.4 推荐动作（`next/evaluate.go`）；`workitem.changeStatus` 在转换事务内承担 Guard（`func(*storage.Tx) ([]*domain.Event, error)`）/ 审批失效（`InvalidateForStatusTx` + `Tx.Staged(rel)`）/ 记录传播（`propagateRecordsTx`，决策/发现 `RelatedWorkItems` 命中 → `decision://<id>` / `finding://<id>` 追加到父与直接兄弟 `context_refs`）；`workflow_instance.go` 五类实例操作只动 `workflow` 字段（不动状态/调度/租约）；`events.AppendBatchTx` 按 shard 分组一次 `AppendJSONLRaw`，`Tx.AppendJSONLRaw` 多行 payload 拒绝空行/CR/缺终止符；`record.ListArtifacts` 给门禁证据收集使用。
- 既有项目接入卡：概述增 M3 命令族（`workflow`/`approval`/`next`）、架构设计增三条分发链与依赖边、Schema 与错误契约增 workflow 策略契约与 M3 退出码/JSON 错误扩展、编码规范增 Guard 签名与 next 编码规则、特殊配置与命令增三族命令使用与 `m3helper`。
- 文档校验：`repowiki validate` 报告 20 files、0 errors、0 warnings（路径稳定 + 卡片导航 + 内部链接一致）。
- 保护：起始逐页 hash 比对 20 页全部一致（增量刷新基线 85b0de7 → cf7b256 中无人工修改），无需跳过页；新增 run.json 已写清单记录所有受影响页。
- 收尾：`repowiki state --update` 刷新页面 hash 与 scope；`repowiki status` 报告 Wiki is up to date。


## 2026-09-18 · 85b0de7 M2 增量刷新

- 源码基线：`85b0de7`（M2：状态机、领取租约、对账/修复；相对上次基线 2 提交、33 文件、16 页受影响）；任务 #162。
- 增量范围：两个模块的全部受影响卡刷新；新增 2 张自定义卡（`状态机与调度.md` dimension `task_lifecycle`、`对账与修复.md` dimension `reconciliation`）；3 篇文章与 index 更新。产物共 20 个 Markdown 文件。
- 模块 scope：durable-storage 增补 `internal/reconcile/**`；覆盖 79/85（92.9%），未覆盖 6 个文件同前（规则、Git 配置与辅助文档，见 plan.coverage_check）。
- 保护：起始逐页 hash 比对 18 页全部一致，无人工修改页、无需跳过。
- 文档校验：`repowiki validate` 报告 20 files、0 errors、0 warnings（含新增卡的可达性与 plan 一致性）。
- 修正：全库清理过时名称 `workitem.ReleaseLease` → `workitem.Release`（实际导出名；含 reconcile.go 注释与 5 个页面）；`go build ./...` 通过。
- 验证边界：本轮为文档刷新，未重跑全量 Go 测试（M2 提交前已全量通过；报告中的性能与恢复结论均标注来源）。
- 收尾：`repowiki state --update` 刷新页面 hash 与 scope；`repowiki status` 确认基线对齐。

## 2026-09-17 · 4ad8f9e 增量刷新

- 源码基线：`4ad8f9e`（提交后仅 search 测试与 benchmark 变化；未跑 Go 测试）；任务 #136。
- 增量：六张未受保护的可靠文本存储卡刷新基线并核对引注；两篇未受保护文章更新摘要与 benchmark 链接；index、导航不变。
- 保护：`content/开发与故障诊断.md`、`knowledge/可靠文本存储/特殊配置与命令.md` 与生成基线 hash 不一致，按规则保留原文，未重生成。
- 规模档位：72 个扫描文件、多层目录 → 模块树档位（caps 调整为 8/30/5/2500），两个功能模块与三篇文章路径保持不变；模块 scope 覆盖 66/72。
- 文档校验：`repowiki validate` 18 files、0 errors、0 warnings。
- 收尾：`repowiki state --update` 以当前提交刷新页面 hash 与 scope；`repowiki status` 确认基线对齐 `4ad8f9e`。

## 2026-09-17 · M1 增量刷新

- 源码基线：`2735f62`；任务 #126；保留两个模块与既有文章路径。
- 产物：13 张知识卡、3 篇文章、index 与本日志，共 18 个 Markdown 文件。补齐 M1 领域模型、CAS、事件/Run/记录、Artifact 版本链、search 与冒烟入口。
- 模块 scope 覆盖：66/72 个扫描文件（91.7%）；未覆盖的 6 个文件为规则、Git 配置及辅助文档，见 plan.coverage_check。
- 保护：本轮开始的页面 hash 比对无人工修改、无缺失页；已有生成修改续跑保留。只修改 Wiki 与生成元数据，未修改生产代码。
- 最终文档校验：`repowiki validate` 报告 18 files、0 errors、0 warnings；此前发现的一条相对链接已修复。
- 代码验证：本轮 `go build ./...` 成功；`go test ./...` 失败于 `TestSearchThousandFilesResponsive`，1000 文件扫描耗时 3.4366182 秒，超过 2 秒门限。其余包通过（部分缓存）。未修改门限，也未将历史 0.6–0.7 秒测量当作本轮结果。
- 此刷新不修复搜索性能问题；最新失败已写入开发指南和任务验证记录。文档校验通过不代表代码全量测试通过。
- 收尾：通过 `repowiki state --update` 写入页面 hash、来源 scope 与当前源码基线，再以 `repowiki status` 检查新鲜度。

## 2026-09-17 · 首次整库生成

- 基线提交：`47620c7f214f2bf4edf27a82b5a26e3bfe5eabc9`，分支 `master`。未创建提交。
- 范围：当前 M0.1–M0.4 实现；两个功能模块，11 张知识卡、3 篇文章、index 导航；加本日志共 16 个 Markdown 文件。
- 计划：`.repowiki/plan.json` schema 2，采用两个单层功能模块与最小档文章上限。
- 覆盖：模块 scope 覆盖 36/42 个扫描文件（85.7%）；Go 文件覆盖 33/33。未纳入模块的 6 个文件为仓库规则、Git 配置和辅助文档，详见 plan.coverage_check.uncovered。
- 保护：首次生成，没有既有 Wiki 页面需要跳过；AGENTS.md 仅由 repowiki init 注入受管声明。
- 校验：`repowiki validate` 在生成日志前报告 15 files、0 errors、0 warnings；日志不要求 frontmatter。
- 状态：`repowiki state --update` 已记录源码基线、页面 hash 与 source scopes；`repowiki status` 报告 Wiki is up to date。
- 验证边界：本次为文档生成，未重新运行 Go 测试；文章中历史验证限制均标明来源。
