---
name: devsys
description: Devsys/Workloom 项目状态与工作追踪纪律（.devsys/ 是唯一事实来源）。Use when working in a Devsys-tracked project — 触发词：devsys、Workloom、项目状态、领取任务、workitem、run、决策/发现记录、恢复上下文。
---
<!-- devsys-skill -->
# Devsys Skill

This project uses Devsys for tracked work. Prefer Devsys MCP tools when
available; otherwise use `devsys --json` through the shell.

## Start

1. Run `devsys prime` (or `devsys session start`) — one call: project facts, work in flight, recommended action.
2. Read the recommended work item (`workitem get` / `context get --task <id>`).
3. If the item carries a workflow, read its steps: `devsys workflow get --id <workitem>`.

## Claim

Claim before tracked implementation (`workitem claim --expect <version>`);
reads after a write must re-read (expired hashes are refused, never forced).

## During work

- Record significant findings, decisions and blockers
  (`finding` / `decision` / `event`); keep the run evidence current (`run update`).
- Advance a workflow with `devsys workflow step-complete` when the policy declares steps.
- Blocked: `devsys workitem block`; a gated stage needs `devsys approval request` and a human decision.

## Complete

1. Verify the implementation (tests or equivalent checks).
2. Complete the run (`run complete`; refused without branch evidence unless reviewed).
3. Transition the work item (`workitem transition --expect <version>`).

## Boundaries

- Never edit `.devsys/` files directly (repair via `devsys repair`).
- See `references/cli.md` for the command table and `references/troubleshooting.md` for exit codes and retries.
- Operator-side families (dispatch, approval, archive, workspace) are listed in `devsys --help`.
- Default MCP `--tier core` (19 tools) does not expose `run_update` / `run_fail` / `workitem_block`. Use the CLI, or serve `--tier standard`.
