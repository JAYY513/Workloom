---
id: intake
name: 任务分流
version: 1

input:
  required:
    - workitem
  optional:
    - project_context
    - architecture_context

steps:
  - id: inspect
    type: inspect
    required: true
  - id: specify
    type: clarify
    required: true
  - id: classify
    type: plan
    required: true
  - id: done
    type: verify
    required: true

transitions:
  - from: inspect
    to: specify
  - from: specify
    to: classify
    when: workitem.clarification_needed == false
  - from: classify
    to: done

gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    done:
      require_artifacts:
        - classification
      require_comment: true

limits:
  max_attempts: 2
  run_timeout_seconds: 1800
  stall_threshold_seconds: 900

quality_gate:
  min_score: 40

on_reject: block
---

# 任务分流流程

这是未知或低置信度任务的中性入口，不执行代码，也不替代具体执行策略。

1. 读取任务、项目上下文和已有决策，确认目标、范围、验收条件和依赖。
2. 补充缺失的规格信息；无法安全补充时停在 `specify`。
3. 在 `classification` 产物中记录任务类型、风险、推荐策略和理由。
4. 分流到 `quick-fix`、`feature-development`、`architecture-change` 或受控后续任务；完成分流前不得进入实施。
