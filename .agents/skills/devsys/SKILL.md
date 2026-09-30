---
name: devsys
description: Devsys/Workloom 项目状态与工作追踪纪律（.devsys/ 是唯一事实来源）。Use when working in a Devsys-tracked project — 触发词：devsys、Workloom、项目状态、领取任务、workitem、run、决策/发现记录、恢复上下文。
---
<!-- devsys-skill -->
# Devsys Skill

When Devsys MCP tools are available and identify the current project correctly, MUST use MCP for all Devsys state reads and writes, including project, blueprint, context, knowledge status, workitems, workflows, artifacts, decisions, findings, events, runs, claims, leases, approvals, and completion.
Use the CLI only when MCP is unavailable, the required operation is not exposed by the active MCP tier, or a local build, test, server, installation, or diagnostic command is required. Before the first MCP write, verify the project identity with agent_session_start or project_get; if MCP and CLI report different project IDs, stop and resolve the working-directory mismatch before writing state. Never edit .devsys/ directly.

## Start

1. Run `workloom prime` (or `workloom session start`) at the start of a new session — one call: project facts, available workflow policies, default policy, and recommended action.
2. Work autonomously by default when the goal is clear and the action is low-risk, reversible, and within the stated scope. Chain reads, diagnosis, tests, routine edits, and other explicitly authorized mechanical steps without asking after each step.
3. Pause only at a decision gate: missing or ambiguous goals, product scope, architecture, inferred blueprint content, approval, destructive or external-impact action, or a choice with materially different outcomes. State the options and the exact decision needed.
4. A missing blueprint is a planning boundary, not a reason to invent one: ask for project goals before drafting it. If the user supplied the goals and explicitly authorized the full onboarding chain, draft/review/activate/bind may continue; otherwise stop after the draft for review. Do not create tasks or implement work merely because a blueprint was created.
5. Before choosing a workflow, inspect the available policies with `workloom workflow list` (or MCP `workflow_list`) and inspect the deterministic task recommendation with MCP `workflow_recommend`. Unknown or low-confidence work must enter `intake` for specification and classification before execution; never use `quick-fix` as a generic fallback. Choose `quick-fix` only for a confirmed small correction, `feature-development` for a normal feature, and `architecture-change` for architectural work.
6. Start a chosen workflow instance explicitly with `workflow start --policy <id>`; never create an instance implicitly. If the user explicitly requested task creation and implementation and the acceptance scope is clear, create and implement without an extra confirmation; otherwise show the plan and pause at the unresolved decision.
7. For a new project, run `workloom setup`: it installs intake, quick-fix, feature-development, and architecture-change; intake is the neutral default, while reference-template is opt-in and does not become the default policy.
8. Read the recommended work item with workitem get or context get --task <id>; if it carries a workflow, read its steps with `workloom workflow get --id <workitem>`.
## Claim

Claim before tracked implementation (`workitem claim --expect <version>`);
reads after a write must re-read (expired hashes are refused, never forced).

## During work

- Record significant findings, decisions and blockers
  (`finding` / `decision` / `event`); keep the run evidence current (`run update`).
- Advance a workflow with `workloom workflow step-complete` when the policy declares steps.
- Blocked: `workloom workitem block`; a gated stage needs `workloom approval request` and a human decision.

## Complete

1. Verify the implementation (tests or equivalent checks).
2. Complete the run (`run complete`; refused without branch evidence unless reviewed).
3. Transition the work item (`workitem transition --expect <version>`).

## Boundaries

- Never edit `.devsys/` files directly (repair via `workloom repair`).
- See `references/cli.md` for the command table and `references/troubleshooting.md` for exit codes and retries.
- Operator-side families (dispatch, approval, archive, workspace) are listed in `workloom --help`.
- Default MCP `--tier core` (20 tools) does not expose `run_update` / `run_fail` / `run_cancel` / `workitem_block`. Use the CLI, or serve `--tier standard`.
