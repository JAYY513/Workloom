## 2026-09-20 · ba342b0 源链接归一 + core 档 19 项同步（任务 #328 / #327）

- 源码基线：`2b8b8ce` → `ba342b0`（MCP `registerRunFinish` 拆成 Complete/Fail/Cancel，core 严格 19 项；#313-B 文档边界；清理 v0.1.1-rc1 记录）。
- 链接纪律（output-spec §5）：把 bundle 外相对源链接（`../../../internal|cmd|scripts|docs|…`）归一为 `file://<仓库相对路径>`（保留 `#L` 片段）；bundle 内互链保持相对。转换 **382** 处（含 3 处围栏内引注），剩余 out-of-bundle 相对链接 **0**。
- 引注审计覆盖面因此从 1194 扩到 **1493** 条 `file://…#L`：顺带修 5 处此前相对形态下未扫到的越界——`archive.go:87-110`/`19-110` → 文件止于 106；`cli.go:1396-407` 倒序笔误 → `1396-1407`（`approvalState`）。
- wiki 正文（#327 已在 `ba342b0` 提交）：`工具与Profile.md` 去掉「实测 21」、登记 1:1 register；`index.md` 同步；`快速开始.md` 更正「tier 默认 standard」为 core。
- 本轮只改 `docs/repowiki/**` 与随后的 `.repowiki/state.json`；产品代码已在 `ba342b0`。

## 2026-09-20 · 2b8b8ce 增量刷新（P1/P2 + Release 自动化 + 外部审查两批修复）

- 源码基线：`c5b5533` → `2b8b8ce`（17 提交：发布矩阵/`release.yml`/runbook、P1 `roots.go`+`wire` 三态+技能、P2 `prime`/`--latest`/MCP tier、提示词修复、安装链修复（`98adab3`/`d726ed4`）、使用修复两批（`d726ed4`/`b89ffae`）、v0.1.0/v0.1.1 发布记录）；任务 #326。
- 增量范围：受影响 5 模块（项目接入与配置 9、可靠文本存储 11、共享应用与MCP 6、执行层 6、视图层 7 = 39 卡）+ 3 篇文章 + 本 index；知识层无源码变更，仅 `source_commit` 未动（其卡仍属受管页面）。
- 手段：8 个子代理并行（每模块一批 + 每篇文章一篇），主代理逐文件复核并修复（见下）。**偏离记录**：技能增量模式写「受影响模块的全部卡重生成」，本轮采用**证据驱动的外科更新**——只有 delta 真正触及的卡改写正文，未受影响的卡只更新 `source_commit` 并核对引注；理由是 17 个提交的功能增量集中在少数卡，整卡重写会把既有已验证内容重新生成一遍、风险大于收益。受影响模块的**判定口径未变**（仍按 plan scope 与 git diff 计算）。
- 主代理复核修复（子代理产物缺陷）：2 处 frontmatter 丢 `description`（`共享应用与MCP/概述.md`、`项目接入与配置/Schema与错误契约.md`，后者并补回 `generated: true`）；1 处未闭合引注（`可靠文本存储/架构设计.md` 的 `[internal/workitem/claim.go:1-1019](…` → 补全为 `1-1025`）；2 处丢小节标题导致内容悬空（`视图层/编码规范.md` 第 6 条、`项目接入与配置/特殊配置与命令.md` 的「与其他层的关系”）并各删一处游离重复行；3 个文件被写成 CRLF（`项目接入与配置/{概述,架构设计,CLI渲染与交换协议}.md`）→ 归一 LF；4 处跨 bundle 或失效链接（`项目总览.md` 的 `../knowledge/协作层/概述.md`、`../发布流程.md`，`特殊配置与命令.md` 的 `../../发布流程.md`，`可靠文本存储/架构设计.md` 的破损表格行）；13 处行号越界/陈旧引注（`tree.go:42-94`→`42-93`、`template.go:51-313`→`54-233`、`release.yml:1-160`→`1-131`、`build-release.sh:1-66`→`1-50` 与 `59-65`→`42-49`、`install.sh:1-94`→`1-80`、`install.ps1:1-86`→`1-79`、`harness.go:1-130`→`1-126`、`record/update.go:1-200`→`1-189`、`reconcile_io.go:1-68`→`1-67`、`workspace/hook.go:317-334`→`workspace/workspace.go:319-334`（`HookEnv` 实际位置，3 页同修）。
- 一致性发现（新）：实测 `devsys mcp serve`（默认 profile+tier core）`tools/list` 返回 **21** 项，而 `allTools()` 标定 `TierCore` 仅 19 项——`run_fail`/`run_cancel`（tier `""`）与 `run_complete`（TierCore）共用 `registerRunFinish`，tier 门按 spec 判定而注册函数一次注册三名，故二者随 `run_complete` 进入 core 档。wiki 记录偏离（`共享应用与MCP/工具与Profile.md`），产品决策另立任务 #327。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**；主代理自建引注审计（`file://` + 行号）**1194 条 0 越界 / 0 缺失**。
- 保护：D4 逐页 hash 比对 55 页，仅 `log.md` mismatch（finalize 自身重写，豁免），无人工修改跳过。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与源码基线 `2b8b8ce`；`repowiki status` 复核。
- 验证边界：本轮只改 `docs/repowiki/**` 与 `.repowiki/**`，未触碰产品代码；全量 `go test ./...` 结论沿用 v0.1.1 发版前跑批（27 包绿、gofmt/vet 净）。

## 2026-09-19 · c5b5533 M9 状态同步（state-only，无内容重生成）

- 源码基线：`c5b5533`（相对上次 wiki 基线 `cde3320` 仅 1 提交，即前轮 `state.json` 记录提交本身；`repowiki status --json` 报 `1 new commits, 0 files changed`、`affected_pages: []`）；增量模式运行，无受影响页即跳过 4a/4b。
- 计划：`.repowiki/plan.json` schema 2 不变，模块树档位（6 模块 / 3 文章，caps 内）；`coverage_check` 248/255，uncovered 10 项不变（含 M9 三文档，bundle 外用户文档）。
- 增量范围：无。`repowiki scan` 刷新快照（258 files，go:209；`docs/开发记录.md` 等 size/hash 跟进）；受管 55 页零改动，D4 比对不触发，人工保护无跳过。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**（含可达性与 plan 一致性）。
- 收尾：`repowiki state --update` 刷新基线 `cde3320` → `c5b5533`（55 页、coverage 248/255、phase finalize success）；`repowiki status` 报告 **fresh**。
- 验证边界：本轮仅同步 state/snapshot，不碰 Go 源码与 wiki 内容；全量测试结论沿用 M9 收尾（25 包绿、gofmt/vet 净）。

## 2026-09-19 · def1735 M9 基线（M8-close 修正 + M9 文档增量）

- 源码基线：`def1735`（相对上次 wiki 基线 `52294b0` 共 7 提交：M8 `f7653a8` smoke-m8 双平台剧本 + `471aa33` M8 基线刷新 + M8-close `1ac4d56` 评审 4 项 + M9 `d5423fb` 三文档 + `6214330` README 行 + 本轮 `def1735`；Go 源码自 M8 基线后零变更，仅 `scripts/smoke-m8.ps1` +20/-10）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，模块树档位不变，scope 零改动；`coverage_check.uncovered` 增 M9 三文档（`docs/M9-验收报告.md`、`docs/使用手册.md`、`docs/迁移指南.md`，bundle 外用户文档，与 `docs/M6-一致性自检.md` 同类）；coverage 248/255。
- 增量范围：**project-access** 特殊配置与命令卡增「M8 收尾剧本」节（四段口径 + 双平台命令 + M8-close 三修复行号）；**三篇文章**各增 M9 条目 + `source_commit` → `6214330`（项目总览版本边界同步扩展 M9 句）；**开发与故障诊断**未知版本行指向 `docs/迁移指南.md`（bundle 外，不做跨包链接）+ BOM 行引注修正（写卡 `smoke-m8.ps1:205`，增 clash `104-116` / dry-run `163-165` 两行）；** cites 修正**（runstream `87-112`→`87-118` 两处、`smoke-m8.ps1:1-242`→`1-245`、`198-202`→`203-205`）。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**。
- 保护：D4 逐页 hash 比对 55 页，仅 2 页 mismatch（`执行命令族.md`、`特殊配置与命令.md`，均为 `1ac4d56` M8-close 评审修正本身，无人工修改）；2 子代理配额失败（429），主代理直写 5 页（3 文章 + 1 卡 + plan）。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与源码基线 `def1735`（55 页、coverage 248/255、phase finalize success）；`repowiki status` 报告 **fresh**。
- 验证边界：Go 源码未动（全量 `go test ./...` 25 包绿 wall 365s、`gofmt -l` 无输出、`go vet ./...` 干净均在本轮 test 阶段完成，见任务 artifact）；M9 三文档命令全部隔离实跑（MANUAL-ALL-GREEN + CHAIN-GREEN + ROLLBACK-TAG-GREEN）。

## 2026-09-19 · bae7e28 M7 收尾（smoke 剧本 + wiki 补齐）

- 源码基线：`bae7e28`（M7 收尾：`scripts/smoke-m7.{sh,ps1}` 双平台实跑 exit 0 + README 收尾行 + tag `m7`；相对上次 wiki 基线 `b7855fb` 3 提交、6 文件、23 affected pages）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（237 个扫描文件）。`project-access` scope 补 `scripts/smoke-m7.{sh,ps1}`（M6 轮同款归属）；`view-layer` scope 不变（`internal/view/**` + `internal/sitestatic/**`）；三篇文章 `modules` 不变；coverage 228/235（未覆盖 7 个均为源码主干外，与上轮同）。
- 增量范围：**view-layer** 特殊配置与命令卡增「端到端验证」节（五段断言口径 + 双平台命令）；**project-access** 特殊配置与命令卡增「M7 收尾剧本」节（与视图卡互链）+ 技术栈卡 description 补 `scripts/smoke-m7.*`；三篇文章各一行（项目总览版本边界 + 快速开始 M7 节尾 + 开发与故障诊断测试节尾，均链向视图卡端到端验证节）。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**。中途修 1 项（视图卡新增跨目录链接少一层 `../`，phantom，沿用 #259 轮教训即时纠正）。
- 保护：`repowiki scan` 先刷新快照（235→237 文件）；D4 适用页均与 state 一致，无人工修改；主代理直写 6 页，无子代理。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与源码基线 `bae7e28`（55 页、coverage 228/235、phase finalize success）；`repowiki status` 报告 **fresh**。
- 验证边界：Go 源码未动（终验全量 `go test ./...` 25 包绿、`gofmt -l` 无输出、`go vet` 干净均在本卡 test 阶段完成，见任务 artifact）；双脚本实跑 exit 0（Git Bash 14.9s / PowerShell 10.9s）。

## 2026-09-19 · fa2a87d M7.2–M7.4 增量刷新

- 源码基线：`fa2a87d`（M7.2 静态构建 `internal/sitestatic` + M7.3 本地只读服务 + M7.4 新鲜度提示；相对上次 wiki 基线 `463c2d8` 4 提交、50 文件、12 affected pages）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（235 个扫描文件、多层目录）。`view-layer` scope 补 `internal/sitestatic/**`（M7.1 时已预告此归属）；三篇文章 `modules` 不变；coverage 228/235（未覆盖 7 个均为源码主干外：`.gitattributes/.gitignore/AGENTS.md/docs×4`）。
- 增量范围：**view-layer** 7 张全重写（概述/架构设计/技术栈/编码规范/特殊配置与命令/视图数据模型/读取纪律与信任语义：M7.2 `Build` 落 8 文件 + `DefaultOutRel`/`ResolveOut`，M7.3 `RenderPage/ModelJSON` 逐字节一致 + serve 每请求现装 + 127.0.0.1:8080/`--allow-remote`/405，M7.4 `freshnessHint` 纯渲染 + 全站第二横幅 + CLI hint 行，纠正“TS/React/计划中”旧表述）；**project-access** 9 张复核（概述/架构设计/CLI 渲染与交换协议/Schema 与错误契约/执行命令与 DEVSYS_PROJECT_ROOT/技术栈/编码规范/特殊配置与命令覆盖 build/serve/hint，AGENTS.md写入仅刷 commit；修正 workspace.go 行号漂移与 `{view}`→`{view,build,serve}`）；三篇文章增量（项目总览/快速开始/开发与故障诊断：综述段 + `## 更新摘要` + build/serve/hint 示例与诊断行）；`index.md` 两行（M7.1→M7）。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**。中途修 3 项（两卡 `description:` 冒号键丢失致 missing-field、视图 setup 卡一条指向不存在知识页的跨目录链接并入既有纪律卡）。
- 保护：D4 逐页 hash 比对 12 affected 页与 state 一致，无人工修改；16 子代理分写（视图 7 + 接入 9）各守一卡，无越界。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与源码基线 `fa2a87d`（55 页、coverage 228/235、phase finalize success）；`repowiki status` 报告 **Wiki is up to date**。
- 验证边界：本轮为文档刷新，未触动 Go 源码或脚本；`gofmt -l cmd internal` 无输出，`go vet`（sitestatic/view）干净；全量 Go 测试未重跑（M7.4 提交 `fa2a87d` 前已绿：四包回归 + 12 新例 + scratch 实跑）。

## 2026-09-19 · 463c2d8 M7.1 增量刷新

- 源码基线：`463c2d8`（M7.1：只读聚合层 `internal/view` + `devsys workspace view` + 触发器匹配抽到知识层；相对上次 wiki 基线 `75998a8` 2 提交、16 文件受影响）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（226 个扫描文件、多层目录）。新增第六个模块 `view-layer`（scope `internal/view/**`）；三篇文章的 `modules` 补 `view-layer`；coverage 219/226 = 96.9%（未覆盖 7 个文件同前）。
- 增量范围：**view-layer** 新建 7 张知识卡（概述 / 架构设计 / 技术栈 / 编码规范 / 特殊配置与命令 + 自定义卡「视图数据模型」dimension `view_model`、「读取纪律与信任语义」dimension `read_discipline`）；**project-access** 9 张全部刷新覆盖 M7.1（概述增 `workspace view` 命令行与视图域边界；架构设计新增「M7.1 命令族详解」+ 依赖关系行 + 源码位置行；CLI渲染与交换协议增 `workspace view` 协议节；Schema与错误契约增 M7.1 退出码语义扩展；特殊配置与命令增 `workspace view` 节；执行命令与DEVSYS_PROJECT_ROOT 增视图域 vs §4.8 `worktree` 边界；技术栈 / 编码规范 / AGENTS.md写入 保持既有形态）；**knowledge-layer** 11 张刷新（概述 / 架构设计 / 知识服务接线 / 上下文装配 覆盖 `TriggerHit` / `WorkItemText` 归属，其余 7 张刷新 `source_commit`）；**shared-app-mcp** 6 张刷新（概述 / 架构设计 覆盖 `context.go` 改走知识层匹配，其余 4 张保持既有形态）；三篇文章全部覆盖 M7.1（项目总览增视图层行 + 架构图节点 + 版本边界 + 快速链接；快速开始新增「M7.1 视图域（只读）」小节 + 退出码表行 + 修正章节目录；开发与故障诊断新增「M7.1 视图层只读契约」与「M7.1 视图层诊断」两节 + 测试口径 + 更新摘要）。
- 核心内容新增：`internal/view`（`view.go` 数据契约；`collect.go` 共享锁 → `InspectUnlocked` 降级 + reader / `sortedUnique` / problems 收集；`build.go` `Build` → `reconcile.Doctor` → git 基线 → pending 提前返回（`emptyBusiness` + `pendingReadiness` 走 `next.Evaluate`）→ 各节装配 + `capList` 先定序后截断 + `collectSources`；`view_test.go` 10 例覆盖确定性 / 零写入 / 只读副本 / pending / 降级 / 截断 / 断尾）；`internal/cli/workspace.go`（`runWorkspace` 路由 + `runWorkspaceView` + `renderWorkspaceView` 一行一事实）；`internal/knowledge/trigger.go`（`TriggerHit` / `WorkItemText`，`context get --task` 与视图层共用）。
- 文档校验：`repowiki validate` 报告 **55 files、0 errors、0 warnings**（含新模块目录锚与可达性）。本轮同时修复：两篇文章缺失的 frontmatter 结束 `---`、`快速开始.md` 两处未闭合代码围栏与失效章节目录、6 页 description 含 `: ` 未加引号；并修正增量改写中丢失的 3 处标题与 1 处被截断的目录树。
- 保护：起始工作区干净（逐页与 `HEAD` 一致，无人工修改），无需跳过页；`.repowiki/run.json` 记录已写清单（view-layer 7 张新卡 + 既有模块卡 26 张 + 三篇文章）。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与源码基线 `463c2d8`（55 页、coverage 219/226、phase finalize success）；`repowiki status` 报告 **Wiki is up to date**。
- 验证边界：本轮为文档刷新，未触动 Go 源码或脚本，未重跑全量 Go 测试（M7.1 提交 `463c2d8` 前已通过 24 包全量测试、gofmt/vet 与只读副本实跑）。

## 2026-09-19 · 75998a8 M5.6 增量刷新

- 源码基线：`75998a8`（M5.6：分层任务上下文、生成器适配层、上下文快照；相对上次 wiki 基线 `7c3fcde` 4 个提交、23 文件受影响）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位。模块 scope 不变；本轮**新增 1 张知识卡** `knowledge/知识层/上下文快照.md`（dimension `context_snapshot`），覆盖 `internal/prompt/prompt.go` 的 `Snapshot` 类型 + `internal/app/runstream.go` 的 `contextSnapshot` 字段 + RunPromptView.Snapshot + 「知识基线」section。
- 增量范围（45 页受影响，其中 1 页新建）：**知识层**模块 11 张知识卡（10 张刷新 + 1 张新建）——`概述`/`架构设计`/`知识服务接线`/`生成器契约与适配器`/`上下文装配`/`新鲜度与基线`/`人工保护`/`断点续跑与锁`/`索引快照与匹配器`/`页面契约与校验` 全部覆盖 M5.6；**shared-app-mcp** 模块 6 张全部刷新（`概述` 增 Adapter/Finalized/AwaitingGeneration/knowledgeState 行；`工具与Profile` 增 knowledge_validate/knowledge_refresh/context_for_workitem paths；`架构设计` 增 context 三层装配行；`执行命令族` 增 runContextTask；`完成校验与人工复核` 维持 M6）；**project-access** 模块 9 张全部刷新（`概述` 增 Adapter 三字段 + await generation + context --task/--path；`架构设计` 增 Service.knowledgeState + Service.generate + TaskContext；`CLI渲染与交换协议` 增 context --task/--path 行 + awaiting generation 行；`Schema与错误契约` 增 Adapter* 三字段 + Finalized/AwaitingGeneration + paths/splitPaths；`执行命令与DEVSYS_PROJECT_ROOT` 维持 M6）；**durable-storage** 模块 11 张维持 M5（无 M5.6 变更）；**execution-layer** 模块 6 张维持 M6（无 M5.6 变更）；**3 篇文章**（项目总览 / 快速开始 / 开发与故障诊断）刷新覆盖 M5.6。
- 核心内容新增：`internal/knowledge/adapter.go`（326 行）= `Adapter` 接口（Name/Probe/Import/Generate）+ `Availability{Installed,Detail}` + `ProbeTimeout=10s` + `GenerateRequest/Root/ScopeFile/Pages` + `GenerateResult/Output/Finalized/AwaitingGeneration/Note` + `Adapters()`（仅 Repowiki）+ `AdapterByName(name)` + `MergeStates(local, imported)` 字段级合并（firstNonEmpty/firstNonEmptyList）+ `repowikiAdapter.Import` 把 `.repowiki/state.json` 映射到层 State + `repowikiAdapter.Generate`（**先 Import 取参考记录** → `init`/`scan` → `pendingPages` 比对 → 仅当所有 scoped 页面与 state 一致才 `state --update`）+ `runRepowiki` + `adapterOutputLimit=256 KiB`；`internal/knowledge/freshness.go` `recordedMatches(root, pagePath, recorded)` 当磁盘 hash 与 state 记录的 ContentHash 一致时把基线晋升为 state.Baseline.Commit（避免刚生成完未改的页面被误判 stale）；`internal/app/context.go` `TaskContext{Summary,Task}` 三层装配 + `Service.TaskContext(ctx,id,paths,limit,refresh)` + `ContextForWorkitem` 第二参 `paths` + `knowledgeContext` 接收 paths 走 `MatchSources`；`internal/app/knowledge.go` `Service.knowledgeState` 复用层（LoadState + Adapter.Import + MergeStates 三步合一）+ `Service.generate` 分派 argv/Adapter + `KnowledgeStatusView` 增 `Adapter/AdapterInstalled/AdapterDetail` + `KnowledgeRefreshView` 增 `Finalized/AwaitingGeneration`；`internal/app/prompt.go` 组装 `prompt.Snapshot{ProjectStateVersion,WorkitemVersion,ArtifactVersions,KnowledgeRevision,KnowledgePages,DecisionIDs,WorkspaceHead,KnowledgeBehind,KnowledgeDegraded}` + `RunPromptView.Snapshot` 字段 + helper（projectStateVersion / artifactVersions / decisionIDs / knowledgePagePaths / knowledgeNotes / knowledgePromptRefs / knowledgeBehind）；`internal/prompt/prompt.go` `Snapshot` 类型 + `Empty()` 方法 + `knowledgeSection` 三态（Degraded/Behind/空）+ `Prompt.Snapshot` 字段；`internal/app/runstream.go` `contextSnapshot`（与 `prompt.Snapshot` 同形，独立类型以保护 on-disk 契约）+ `snapshotRecord(snapshot)`（Empty 时返回 nil 不入流）+ `streamRecord.Context` 字段；`internal/cli/records.go` `context get --task <id> [--path a,b]` 与 `context refresh --task <id> [--path a,b]` 双入口 + `context workitem --id <id> [--path a,b]` + `runContextTask` 渲染 project/task/decision/finding/artifact/notice/knowledge 块（[stale] 标签）+ `splitPaths(list)`（逗号切 + trim 空）+ `reportRefresh` 增 `awaiting generation: <path>` 行；`internal/mcp/tools_context.go` `contextForWorkitemInput` 增 `Paths []string` + 工具描述更新。
- 文档校验：`repowiki validate` 报告 **48 files、0 errors**（含新建 `上下文快照.md`）；YAML front matter 修复 2 处（`知识服务接线.md`、`共享应用与MCP/概述.md` description 含 `: ` 未加引号 → 加双引号包裹）；triggers 全部加引号合规。
- 保护：起始逐页 hash 比对 47 页全部一致（增量刷新基线 `7c3fcde` → `75998a8` 中无人工修改），无需跳过页；新增页面 `上下文快照.md` 显式加入 run.json 已写清单。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与当前源码基线 `75998a8`；`repowiki status` 报告 **Wiki is up to date**。
- 验证边界：本轮为文档刷新，未触动 Go 源码或脚本；未重跑全量 Go 测试（smoke-m5.sh 的十段 + smoke-m6.sh 的九段在前轮已端到端通过）。M5.6 前端到端已通过 `internal/cli/knowledge_test.go`（245 行新增）+ `internal/cli/runround_test.go`（39 行新增）+ `internal/knowledge/adapter_test.go`（234 行新增）+ `internal/knowledge/freshness_test.go`（78 行新增）。

## 2026-09-18 · da99ec2 M6 增量刷新

## 2026-09-19 · 7c3fcde M5 增量刷新

- 源码基线：`7c3fcde`（M5：知识层 + 0/10/11 退出码 + 人工保护 + 续跑 + 上下文装配 + 生成器契约；相对上次基线 2 提交、48 文件受影响）；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（218 个扫描文件、多层目录）。新增第五个模块 `knowledge-layer`（internal/knowledge + .devsys/knowledge 状态），scope 独立于 project-access / durable-storage / shared-app-mcp / execution-layer；三篇文章的目录与文件路径保持不变；coverage 211/218 = 96.8%（未覆盖 7 个文件同前：`.gitattributes`、`.gitignore`、`AGENTS.md`、`docs/原始文档/实施计划.md`、`docs/原始文档/独立于Harness的Agent开发基础设施方案.md`、`docs/开发记录.md`、`docs/M6-一致性自检.md`）。
- 增量范围：knowledge-layer 模块新建 10 张知识卡（概述 / 架构设计 / 页面契约与校验 dimension `page_contract` / 索引快照与匹配器 dimension `index_snapshot` / 新鲜度与基线 dimension `freshness` / 人工保护 dimension `protection` / 生成器契约与适配器 dimension `generator_contract` / 断点续跑与锁 dimension `checkpoint_lock` / 知识服务接线 dimension `knowledge_wiring` / 上下文装配 dimension `context_assembly`）；project-access 2 张刷新（概述增 M5 命令族 + 0/10/11 退出码 + `codedExit{code}` + `Problem.Severity` + `kindStrings`/`kindMilestones` + knowledge_pages/knowledge_generator；Schema 与错误契约增 M5 配置键）；shared-app-mcp 1 张刷新（概述增 知识服务 / 任务上下文装配 行 + M5 卡片导航）；durable-storage 2 张刷新（概述 + 架构设计增 `internal/storage.LockFile` 行；存储错误语义增 `ErrLocked` sentinel + M5 知识层复用说明）；三篇文章全部覆盖 M5（项目总览增 M5 增量刷新段 + 知识层模块 + storage.LockFile 行 + 修正版本边界；快速开始增 M5 知识层命令族 + M5 退出码语义 + smoke-m5.sh；开发与故障诊断增 M5 调试表 + M5 知识层诊断 + M5 编码规范 + M5 测试回归）；index 导航同步刷新（5 个模块列表 + knowledge-layer 接入）。
- 核心内容新增：`internal/knowledge` 八子文件（page.go Parse + 5 必填 + KnownKeys/AuthoredStatuses/KnownTypes + sources 路径形态 + AuthoredStatuses 显式排除手写 stale/invalid；scan.go Scan + ExistingRoots；snapshot.go BuildSnapshot + git check-ignore + DefaultExcludedDirectories + SecretPatterns + DefaultMaxFileSize=1MiB + ErrNoGit + languageOf；match.go Match `.gitignore` 风格语法 + matchCacheSize=4096；freshness.go Evaluate 三层基线 + recordedMatches 晋升 + Unverifiable + Head detached "HEAD"；protection.go Skips 双闸 + content_hash 整文件 vs body hash；generator.go Scope + GeneratorCommand strings.Fields 不走 shell + FormatVersion=1；run.go Checkpoint + LockRefresh + ResumePending；state.go State + LoadState + MissingState）；`internal/storage.LockFile`（OS 独占锁原语，被 LockRefresh 复用，方案 §15.2 同源）；`internal/app.knowledge.go` 四方法 Status/Scan/Validate/Refresh + 四 View 字段 + pageRoots 合并 + checkPageRoot + runGenerator（limitWriter 256 KiB）+ refreshHolder；`internal/app.context.go.knowledgeContext` 三阶段选页（trigger → sources → per-page fresh）；`internal/cli.cli.go` 新增 CodeStale=10 / CodeMissing=11 + codedExit{code} 错误类型；`internal/cli.records.go` runKnowledge{Scan,Validate,Status,Refresh} 路由 + reportRefresh + humanBytes/shortSHA/indexSummary；`internal/mcp.tools.go` knowledge_status/validate/refresh 工具 + registerContextForWorkitem paths[]string 参数；`internal/config` SeverityWarning + KnowledgePages/KnowledgeGenerator 字段 + validate.go 拆 kindStrings 分支。
- 文档校验：`repowiki validate` 报告 47 files、0 errors、1 warnings（仅 `knowledge/可靠文本存储/编码规范.md: page not reachable from index.md` —— 沿袭既有 pre-existing warning，非本次刷新引入）。
- 保护：起始逐页 hash 比对 27 页全部一致（增量刷新基线 da99ec2 → 7c3fcde 中无人工修改），无需跳过页；新增 `.repowiki/run.json` 已写清单记录所有受影响页（知识层 10 张新卡 + 既有模块卡全部覆盖 M5 + 三篇文章 = 27 + 10 = 47 页面）。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与当前源码基线 `7c3fcde`；`repowiki status` 报告 Wiki is fresh。
- 验证边界：本轮为文档刷新，未重跑全量 Go 测试（M5 提交前已端到端通过 smoke-m5.sh 的十段 PASS：page 契约 → 索引扫描 → 新鲜度 0/10/11 → 生成器契约 → 人工保护 → 续跑 → 并发拒绝 → 上下文装配 → 降级）。未触动 Go 源码或脚本。


- 源码基线：`da99ec2`（M6：执行层 + dispatch + 完成校验；相对上次基线 13 提交、60 文件受影响）；任务 #M6；增量模式运行。
- 计划：`.repowiki/plan.json` schema 2，沿用模块树档位（198 个扫描文件、多层目录）。新增第四个模块 `execution-layer`（internal/harness + internal/workspace + internal/dispatch + internal/retry + internal/prompt），scope 独立于 project-access / durable-storage / shared-app-mcp；三篇文章的目录与文件路径保持不变；coverage 191/198 = 96.5%（未覆盖 7 个文件：`.gitattributes`、`.gitignore`、`AGENTS.md`、`docs/原始文档/实施计划.md`、`docs/原始文档/独立于Harness的Agent开发基础设施方案.md`、`docs/开发记录.md`、`docs/M6-一致性自检.md`）。
- 增量范围：execution-layer 模块新建 6 张知识卡（概述 / 架构设计 / 技术栈 / 编码规范 / 特殊配置与命令 + 自定义 dimension `adapter_protocol` 适配器协议契约）；project-access 模块 8 张全部刷新覆盖 M6（概述增 worktree/dispatch/run exec|prompt|verify|complete|fail|cancel 命令族与 DEVSYS_PROJECT_ROOT + workspace_root + dispatch_command；架构设计新增 M6 命令族详解 + 内部分层加执行层；Schema 与错误契约增 config.workspace_root / config.dispatch_command 字段 + M6 退出码语义扩展；技术栈增 golang.org/x/term；编码规范 + 特殊配置与命令增 M6 命令与剧本；新建自定义卡 执行命令与DEVSYS_PROJECT_ROOT，dimension `execution_wiring`）；shared-app-mcp 模块 6 张全部刷新（概述增 派发/工作区/运行执行/运行验证/运行事件流/提示词装配 行；架构设计加 M6 调度 tick mermaid + 工具表加 run_prompt/run_verify/run_complete/run_fail/run_cancel；新建自定义卡 执行命令族 dimension `execution_commands`；新建自定义卡 完成校验与人工复核 dimension `completion_review`）；durable-storage 模块 11 张全部刷新（概述增 .devsys/runs/<id>.jsonl 流；新建自定义卡 运行事件流 dimension `run_event_stream`）；3 篇文章（项目总览 / 快速开始 / 开发与故障诊断）全部刷新，新增 M5/M6 工作区与执行调度章节；index 同步更新。最终产物 37 个 Markdown 文件（29 知识卡 + 3 文章 + index + log + 5 模块概述 → 共 11 + 6 + 5 + 3 + 1 + 1 = 27 已有 + 1 新模块 6 + 1 新 module 1 = 调整后 38；实际 index/log 与三文章不变，+5 张卡 = 32 → 36 文件 + index = 37）。
- 核心内容新增：执行层五子包（harness / workspace / dispatch / retry / prompt）落到 internal/ 下，五子包之间只允许 `harness.shell` 被 `workspace.RunHook` 复用，其它互不依赖；`internal/app` 新增 Service.WorkspacePrepare/Remove/List + Dispatch（recover → retrySweep → ResolveCaps → dispatch.Plan → claim → spawn）+ RunExec（before_run + prompt.Assemble + spawn + 行级 stream + 终止守卫 + 阶段事件）+ RunPrompt（首轮/续轮 + writePromptFile）+ RunVerify（claim_head vs current_head）+ RunFinish（succeeded 走 verify 守卫 + `--force --by reviewer` 绕过）；`internal/harness` 暴露 §9.2 Adapter/Session 接口 + §9.3 CLI 形态（Shell + Codex/OpenCode/ClaudeCode）+ proc_unix/proc_windows 平台 split；`internal/workspace` 提供 Key（净化 + sha256[:16] hex 后缀）+ Root/Validate（根内不变量 + symlink 逃逸）+ Ensure/Remove/List（git worktree 生命周期）+ RunHook + HookEnv；`internal/dispatch.Plan` 是纯函数（priority desc → created_at asc → id asc + 状态白名单 + blocked_by + 全局/状态并发）；`internal/retry.Delay` 是 base/max/jitter 的可复现退避（seed=工作项 ID）；`internal/prompt.Assemble` 首轮全 brief、续轮 delta + remaining，sectioned 渲染 + sha256(text) Hash；`.devsys/runs/<id>.jsonl` 行级 sync（syncEvery=128 + 控制记录 + 末次）+ 断尾重建 + 阶段事件 run_exec_started/run_finished/record_prompt/review_routed/review_accepted；MCP `tools_run` 增 run_prompt/run_verify（session profile）+ run_complete/run_fail/run_cancel（executor profile）；`config.yaml` 增可选 `workspace_root`（默认 `<project>/.devsys/workspaces`）+ `dispatch_command`（默认 `devsys run exec --id {run_id} --actor dispatch --reason tick`）；`DEVSYS_PROJECT_ROOT` 让 agent 在 git worktree 内运行时把报告写回主仓库的 `.devsys/`。
- 既有模块卡：project-access 概述增 M5 保留位 + M6 七族命令；架构设计新增 M6 命令族详解 + 内部分层加 execution-layer；Schema 与错误契约新增 workspace_root/dispatch_command 字段表 + M6 退出码语义扩展；技术栈新增 golang.org/x/term 与 proc_unix/proc_windows 平台 split；编码规范 + 特殊配置与命令增 M6 命令与剧本；新建自定义卡 执行命令与DEVSYS_PROJECT_ROOT。shared-app-mcp 概述增 派发/工作区/运行执行/运行验证/运行事件流/提示词装配 行；架构设计加 M6 调度 tick mermaid + 工具表加 run_prompt/run_verify/run_complete/run_fail/run_cancel；新建自定义卡 执行命令族 + 完成校验与人工复核。durable-storage 概述增 .devsys/runs/<id>.jsonl 流；新建自定义卡 运行事件流。
- 三篇文章：项目总览更新摘要新增 M6 增量刷新段（含执行层五子包 + CLI 七族命令 + MCP 五工具 + 配置 + 环境变量 + review 状态 + smoke-m6 九段 PASS），并修正版本边界段（去掉「知识（M5）与执行调度（M6）未实现」措辞）；快速开始新增 M5/M6 工作区与执行调度章节（含完整剧本与九种 M6 退出码语义）；开发与故障诊断同步引用 M6 一致性自检与 smoke-m6；index 导航同步刷新。
- 文档校验：`repowiki validate` 报告 37 files、0 errors、0 warnings（路径稳定 + 卡片导航 + 内部链接一致 + plan 一致性 + 5 张自定义卡维度唯一）。
- 保护：起始逐页 hash 比对 25 页全部一致（增量刷新基线 997c5f8 → da99ec2 中无人工修改），无需跳过页；新增 run.json 已写清单记录所有受影响页（执行层 6 张新卡 + project-access 1 张新卡 + shared-app-mcp 2 张新卡 + durable-storage 1 张新卡 = 10 张新页 + 既有模块卡全部覆盖 M6 + 三篇文章 = 27 + 10 = 37 页面）。
- 收尾：`repowiki state --update` 刷新页面 hash、scope 与当前源码基线 `da99ec2`；`repowiki status` 报告 Wiki is fresh。
- 验证边界：本轮为文档刷新，未重跑全量 Go 测试（M6 提交前已端到端通过 smoke-m6.sh/ps1 的九段 PASS：workspace invariants / dispatch tick / harness adapter chain / completion check / retry sweep / unavailable harness / read-only never dispatch / §17 SPEC 一致性自检 / 真实集成画像）。codex 0.144.1 与 opencode 1.18.21 各启动一次命令行（被真实 CLI 接受、JSONL 入流）；两次均因提供方后端不可用失败（codex 代理 503、opencode 本地网关 socket closed）；claude 本机未安装（探测/拒绝路径已验）——按 SPEC 要求，未通过的真实集成明确记为未通过/跳过，确定性链路用 stub harness 覆盖（stdin 投递 → 工作区 → 提交 → 完成校验通过）。


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
