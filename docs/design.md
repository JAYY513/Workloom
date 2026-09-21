# Workloom 设计文档

> **活文档**：随代码演进——架构行为变化与代码变更走同一个 PR 更新本文件。
> 历史设计推演（M0–M9 的实施方案、实施计划与验收报告）保留在仓库 Git 历史中（这些文件曾位于 `docs/原始文档/`、`docs/开发记录.md` 等路径），本文件不复述里程碑过程。

## 1. 定位

Workloom（二进制名 `devsys`）是独立于具体编码 Harness 的 Agent 开发基础设施：把任务、工作流、审批、运行记录、决策、发现、事件与知识上下文保存为项目内 `.devsys/` 下的可读文本，由同一套 CLI 与 MCP 应用服务实施校验、并发控制和恢复。

更换 Codex、Claude Code、OpenCode 或其他 Agent 时，项目状态不丢失；状态随 Git clone/pull/checkout 一起迁移；没有跨项目数据库，也没有常驻服务。

## 2. 设计原则

1. **项目自治**：状态属于项目，而不是某台机器、某个 Agent 或某个 SaaS。
2. **先证据，后状态**：没有可验证证据，就不能把任务标记为完成；完成校验比对工作区 HEAD，证据不足时转入人工复核。
3. **默认拒绝**：版本冲突、策略损坏、审批缺失和恢复未完成时 fail closed。
4. **文本优先**：数据可以被人阅读、Git 审查，并由简单工具交换。
5. **投影不是事实**：Dashboard、静态站、看板与 Contrabass 只是执行或观察后端，`.devsys/` 始终是唯一事实来源。
6. **单写者接力**：当前跨设备模型是明确交接（`sync status` 只读判定就绪），不支持多设备同时写入。

从原设计稿继承的三条边界约束：

- **Skill 不是核心系统**：`.agents/skills/devsys/` 只是使用说明与行为约束，不保存核心状态；核心能力全部由独立服务提供。
- **记录与推理分离**：Agent 的建议、已确认事实、已批准决策与已验证结果在记录系统里是不同状态，不混为一谈。
- **无用户级公共数据区**：任务与运行状态不写入用户级数据库；用户级目录只允许项目路径注册表、凭证引用和与项目语义无关的缓存。

## 3. 总体架构

```text
  Codex / Claude Code / OpenCode / 其他 Agent
        │
        │ stdio MCP / CLI（另有只读 HTTP 视图 workspace serve）
        ▼
┌──────────────────────────────────────────────────────┐
│ 接入层   internal/mcp（官方 MCP Go SDK，profile ×    │
│          tier 过滤 60+ 工具；--tier core|standard）   │
│          internal/cli（渲染薄层：flag 解析 + 渲染 +   │
│          toCoded 错误归一；P1 子目录根解析）          │
├──────────────────────────────────────────────────────┤
│ 应用服务  internal/app（Service，30+ 方法：Session/  │
│          Context/Project/Workitem/Workflow/Approval/ │
│          Decision/Finding/Artifact/Event/Run/Next/   │
│          Wire/KnowledgeStatus）                      │
│          错误模型 *app.Error{Kind,Message,Problems}   │
│          → Class() 四类归一；CLI 与 MCP 共用，无特例  │
├──────────────────────────────────────────────────────┤
│ 领域与存储                                            │
│   workitem（九状态机 / 文件级领取 / 租约 / Guard /    │
│             记录传播 / 工作流实例）                   │
│   workflow（严格 located 策略解析 / 条件白名单 / 门禁 │
│             / 质量门 / last-known-good 缓存 / 模板）  │
│   approval（请求 → 决定 → 消费；离开状态同事务失效）  │
│   next（就绪判定，与 claim 同源质量门）               │
│   run / record / events（按月 JSONL）/ reconcile /   │
│   search（无索引扫描）/ config（受管元数据校验）      │
│   storage（原子写 / 版本守卫 / 文件锁 / 可恢复事务）  │
├──────────────────────────────────────────────────────┤
│ 执行层   harness（Shell / Codex / OpenCode /         │
│          ClaudeCode 适配器：流式输出、64KiB 行缓冲、  │
│          进程树终止）                                 │
│          workspace（git worktree 隔离执行环境）       │
│          dispatch（纯函数计划）+ retry（确定性退避）  │
│          + prompt（轮次模板）                         │
├──────────────────────────────────────────────────────┤
│ 知识层   knowledge（扫描 / 快照 / 新鲜度三层基线 /    │
│          人工保护 / 生成器契约 / 上下文装配）         │
│ 视图层   view（只读聚合 Build）+ sitestatic（离线静态 │
│          站 / workspace serve，运行期零写入）         │
└──────────────────────────────────────────────────────┘
        │
        ▼
  .devsys/（YAML / Markdown / JSONL，随 Git 同步）
```

代码级细节与逐条引注见 [RepoWiki](repowiki/index.md)（自动生成、按页可算新鲜度）。

## 4. 状态布局（.devsys/）

| 内容 | 位置 | 提交 Git |
|---|---|---|
| 项目元数据 | `.devsys/project.yaml`、`config.yaml` | 是 |
| 任务 | `.devsys/workitems/<id>.yaml` | 是（一任务一文件，降低合并冲突） |
| 任务规格 | `.devsys/specs/<id>.md` | 是 |
| 工作流策略 | `.devsys/workflows/<id>.md` | 是 |
| 运行记录 | `.devsys/runs/<run-id>.yaml` + `.jsonl` | 是（事件流可归档裁剪） |
| 事件流 | `.devsys/events/<YYYY-MM>.jsonl` | 是（按月分片，可裁剪） |
| 决策 / 发现 / 审批 / 产物元数据 | `.devsys/{decisions,findings,approvals,artifacts}/` | 是（一记录一文件） |
| 领取租约 | `.devsys/scheduling/` | 是（token 不入库，见 §6） |
| 代码知识 | `.devsys/knowledge/` | 是 |
| 检索缓存 | `.devsys/.cache/` | 否（可删除重建） |
| 日志 / 事务恢复材料 / 租约 token | `.devsys/local/` | 否 |

原则：结构化数据用 YAML，正文知识用 Markdown，追加型数据用 JSONL；所有受管状态文件顶层带 `schema_version`，未知版本拒绝写入、只读诊断仍可用，迁移必须显式（见[迁移指南](迁移指南.md)）。

## 5. 并发与一致性

- **乐观并发 + 版本守卫**：写入前校验期望版本/哈希，旧版本更新被拒绝并给出重试出路（重新读取 / `--latest`）。
- **单文件原子写 + 可恢复事务**：临时文件后替换，禁止就地截断；任务、事件、租约的关联变更在同一事务内落盘，中断后按事务日志确定性恢复；恢复材料存于 `.devsys/local/txn/`，不可当作可丢弃缓存清理。
- **文件级领取 + 租约 + 心跳 + 超时释放**：孤儿恢复先完成事务恢复，再识别「已领取但无运行记录」的业务孤儿；`doctor` / `repair --dry-run` 只报告不自动改写。
- **读路径不升级为写路径**：视图与 `next` 在锁不可用时降级 advisory 读取并如实标注；存在待恢复事务时不渲染业务事实。
- **Git 不是锁**：跨设备只有单写者接力；租约分叉保留双方并阻止相关任务派发，禁止按时间戳较新者自动覆盖。

## 6. 安全

- 凭证与密钥只存环境变量 / 系统钥匙串；状态文件只保存引用名，模板在运行时展开。
- 租约 token 落本机 `.devsys/local/leases/`（gitignore）；随仓库提交的租约只含元数据，clone 副本无凭证。
- `.devsys/local/` 与 `.devsys/.cache/` 不提交；归档保守（不删只移，保留指针）。

## 7. 非目标

- 不提供远程中心协调服务：核心路径是本地 CLI、stdio MCP 与 Git。
- 不支持跨设备同时写入或实时同步（单写者接力 + 人工合并）。
- 不使用数据库；任何缓存必须可删除并自动重建。
- 不让看板、静态站或 Contrabass 成为第二事实来源。

## 8. 延伸阅读

- [使用手册](使用手册.md)：15 分钟上手、日常操作与排障速查
- [迁移指南](迁移指南.md)：`schema_version` 升级与二进制回退
- [发布流程](发布流程.md)：tag / Actions 触发、产物校验与回退 runbook
- [RepoWiki](repowiki/index.md)：自动生成的模块级项目知识
- 历史设计稿：仓库 Git 历史中检索曾位于 `docs/原始文档/` 的文件
