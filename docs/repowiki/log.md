# Repowiki 生成日志

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
