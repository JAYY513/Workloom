---
id: feature-development
name: 功能开发
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
  - id: clarify
    type: clarify
    required: false
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
    to: clarify
    when: workitem.clarification_needed == true
  - from: inspect
    to: plan
    when: workitem.clarification_needed == false
  - from: clarify
    to: plan
    when: workitem.clarification_needed == false
  - from: plan
    to: implement
  - from: implement
    to: verify
  - from: verify
    to: done

approval_points:
  - architecture_change

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
    verification:
      require_artifacts:
        - test-results
    done:
      require_artifacts:
        - test-results

concurrency:
  global: 4
  per_status:
    in_progress: 2

limits:
  max_attempts: 3
  run_timeout_seconds: 7200
  stall_threshold_seconds: 1800
  backoff_max_seconds: 3600

quality_gate:
  min_score: 55

on_reject: block
---

# 功能开发流程（方案 §13.2）

1. 读取任务，检查需求完整性。
2. 检查相关架构与代码，必要时创建调查任务。
3. 生成实施计划（不含时间估算）。
4. 实施并测试；测试结果作为产物记录。
5. 代码审查与问题修复。
6. 更新知识文档，完成任务。
