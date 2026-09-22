---
id: reference-template
name: 参考级流程模板
version: 1

input:
  required:
    - workitem
    - project_context
  optional:
    - architecture_context

steps:
  - id: inspect
    type: inspect
    required: true
  - id: plan
    type: plan
    required: true
  - id: implement
    type: execute
    required: true
    status: in_progress
  - id: verify
    type: verify
    required: true

transitions:
  - from: inspect
    to: plan
  - from: plan
    to: implement
    when: workitem.clarification_needed == false
  - from: implement
    to: verify
  - from: verify
    to: done

completion_rules:
  - acceptance_criteria_verified
  - no_unresolved_blocker

gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    review:
      require_artifacts:
        - plan
      require_comment: true
    done:
      require_artifacts:
        - test-results

# hooks:                      # 生命周期钩子（可选）：after_create / before_run / after_run / before_remove
#   before_run:
#     command: git fetch --prune

limits:
  max_attempts: 3
  run_timeout_seconds: 7200
  stall_threshold_seconds: 1800
  backoff_max_seconds: 3600

concurrency:
  global: 4
  per_status:
    in_progress: 2

quality_gate:
  min_score: 55

on_reject: block
---

# 参考级流程模板

这是一份「照抄后按项目改写」的模板：正文就是派发给 Agent 的提示词，写什么，Agent 就按什么执行。示例三份（快修 / 功能开发 / 架构变更）演示最小写法；本模板演示完整写法——姿态、步骤、证据、停止条件、上报协议。改写时保留结构、替换内容。

## 执行姿态

1. 这是一次无人值守的执行轮次：不要停下来等人回答问题；遇到真实外部阻塞（缺工具、缺权限、缺凭据）按「停止条件」处理。
2. 只在本工作区（worktree）内改动；不要触碰工作区之外的路径，也不要改动 `.devsys/` 受管文件。
3. 先复现、后修改：动手前确认当前行为与问题现象，把复现命令与输出写进运行证据。
4. 优先最小改动：不重构无关代码，不顺手扩大范围；发现的范围外改进记成新任务或 finding，不在本轮实现。
5. 用证据说话：没有命令输出、测试结果或可复现步骤的结论，不要写进完成汇报。

## 步骤

### 1. inspect（调查）

- 读任务简报、验收标准、约束与依赖；读相关决策与发现（上下文引用里的指针）。
- 定位相关代码与配置，确认改动面；不确定的假设要写成 finding 并标注验证方式。
- 产出：改动面清单 + 复现证据（命令与输出）。

### 2. plan（计划）

- 给出可执行的分步计划：每步说明改哪个文件、验证什么、失败时怎么办。
- 计划不含时间估算；不确定项写清楚「需要确认什么」。
- 产出：计划文本（作为 artifact 登记）。

### 3. implement（实施）

- 按计划实施；每完成一个可验证的小步，就记一次 `run update --log`。
- 同步维护证据：命令、测试、改动文件列表（`run update --command/--test/--changed-files`）。
- 遇到与计划不符的现实（接口变了、测试基线是红的），先记录，再决定是调整计划还是停止。

### 4. verify（验证）

- 逐条核对验收标准，给出对应证据（命令 + 输出 + 结果）。
- 跑与改动面相称的验证：单测、目标脚本、可复现的手工步骤；无法验证的项要显式说明原因。
- 产出：验证结果（作为 artifact 登记）。

## 证据要求

| 环节 | 最小证据 |
|---|---|
| 复现 | 命令 + 修改前输出 |
| 实施 | 改动文件列表 + 每步日志 |
| 验证 | 命令 + 修改后输出 + 与验收标准的对应关系 |

## 停止条件

- 完成验收标准且验证通过 → 按上报协议收尾。
- 真实外部阻塞（缺少必要工具 / 权限 / 凭据，且无法在会话内解决）→ 记录阻塞说明（缺什么、为什么阻塞、需要谁做什么），把工作项置为 blocked，然后结束本轮。
- 发现任务描述与代码现实矛盾、或需要范围外决策 → 记 decision 或 finding，把工作项置为 blocked 并说明需要哪种决策，不要自行扩大范围。
- 达到轮次上限或超时 → 保持证据最新，让下一次尝试从断点继续。

## 上报协议

- 进度：`workloom run update --id $DEVSYS_RUN_ID --log <一行说明>`（可多次）。
- 产物：`workloom artifact register --name <名称> --path <路径> --run $DEVSYS_RUN_ID --related $DEVSYS_WORKITEM`。
- 决策 / 发现：`workloom decision create --title <标题> --decision <结论> --by <身份>`、`workloom finding create --title <标题> --description <说明>`。
- 完成 / 失败：`workloom run complete|fail --id $DEVSYS_RUN_ID --actor <身份> --reason <原因>`。
- 步骤推进：`workloom workflow step-complete --id $DEVSYS_WORKITEM --actor <身份> --reason <原因>`。

## 完成条

- 验收标准逐条有证据；验证命令通过；证据已登记且工作项状态与之一致。
- 未解决的风险、偏差与后续事项已写成 finding 或新任务。
