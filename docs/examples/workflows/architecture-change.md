---
id: architecture-change
name: 架构变更
version: 1

input:
  required:
    - workitem
    - architecture_context
  optional:
    - project_context

steps:
  - id: analyze
    type: inspect
    required: true
  - id: compare
    type: plan
    required: true
  - id: decide
    type: clarify
    required: true
  - id: implement
    type: execute
    required: true
    status: in_progress
  - id: verify
    type: verify
    required: true

transitions:
  - from: analyze
    to: compare
  - from: compare
    to: decide
  - from: decide
    to: implement
    when: workitem.approval_required == false
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
    in_progress:
      require_approval: true
      require_artifacts:
        - decision
    review:
      require_comment: true
    done:
      require_artifacts:
        - test-results
        - decision

hooks:
  before_run:
    command: git fetch --prune

limits:
  max_attempts: 3
  run_timeout_seconds: 10800

quality_gate:
  min_score: 70

on_reject: regress:ready
---

# 架构变更流程（方案 §13.5）

1. 提出变更，分析影响范围。
2. 读取架构与代码知识，比较候选方案。
3. 创建 Decision 记录；涉及架构变更时请求审批（approval_point: architecture_change）。
4. 审批通过后更新架构产物并创建实施任务。
5. 实施与验证。
6. 刷新知识文档。
