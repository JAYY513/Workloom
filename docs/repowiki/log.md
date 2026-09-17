# Repowiki 生成日志

## 2026-09-17 · 首次整库生成

- 基线提交：`47620c7f214f2bf4edf27a82b5a26e3bfe5eabc9`，分支 `master`。未创建提交。
- 范围：当前 M0.1–M0.4 实现；两个功能模块，11 张知识卡、3 篇文章、index 导航；加本日志共 16 个 Markdown 文件。
- 计划：`.repowiki/plan.json` schema 2，采用两个单层功能模块与最小档文章上限。
- 覆盖：模块 scope 覆盖 36/42 个扫描文件（85.7%）；Go 文件覆盖 33/33。未纳入模块的 6 个文件为仓库规则、Git 配置和辅助文档，详见 plan.coverage_check.uncovered。
- 保护：首次生成，没有既有 Wiki 页面需要跳过；AGENTS.md 仅由 repowiki init 注入受管声明。
- 校验：`repowiki validate` 在生成日志前报告 15 files、0 errors、0 warnings；日志不要求 frontmatter。
- 状态：`repowiki state --update` 已记录源码基线、页面 hash 与 source scopes；`repowiki status` 报告 Wiki is up to date。
- 验证边界：本次为文档生成，未重新运行 Go 测试；文章中历史验证限制均标明来源。
