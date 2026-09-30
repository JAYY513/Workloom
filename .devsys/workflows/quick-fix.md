---
id: quick-fix
name: 快速修复
version: 1

input:
  required:
    - workitem
  optional:
    - project_context

steps:
  - id: inspect
    type: inspect
    required: true
  - id: implement
    type: execute
    required: true
  - id: verify
    type: verify
    required: true

transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: implement
    to: verify
  - from: verify
    to: done

gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    review:
      require_comment: true
    done:
      require_artifacts:
        - test-results

limits:
  max_attempts: 2
  run_timeout_seconds: 3600

quality_gate:
  min_score: 40

on_reject: block
---

# 快速修复流程（方案 §13.1）

适用于简单 Bug、小范围修改、文档修正与小型配置调整。

1. 读取任务与相关上下文，确认现象与范围。
2. 实施最小改动；不重构无关代码，不顺手扩大范围。
3. 运行必要验证（复现用例或等价检查）。
4. 记录结果并完成任务；完成前逐条确认验收条件。
