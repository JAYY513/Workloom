<div align="center">
  <img src="assets/workloom-mark.svg" width="104" alt="Workloom logo">
  <h1>Workloom</h1>
  <p><strong>Weave your AI coding agents' project state back into Git.</strong></p>
  <p>Harness-independent, Git-native development infrastructure for multiple agents.</p>

  <p>
    <a href="#option-a-let-your-agent-set-it-up-recommended"><strong>Agent Quick Start</strong></a>
    ·
    <a href="../README.md">简体中文</a>
    ·
    <a href="使用手册.md">User guide (Chinese)</a>
    ·
    <a href="design.md">Design doc (Chinese)</a>
  </p>

  <p>
    <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white">
    <img alt="MCP" src="https://img.shields.io/badge/MCP-native-8B5CF6?style=flat-square">
    <img alt="Git native" src="https://img.shields.io/badge/state-Git--native-2563EB?style=flat-square&logo=git&logoColor=white">
    <img alt="Status" src="https://img.shields.io/badge/status-pre--release-F59E0B?style=flat-square">
  </p>
</div>

---

Workloom is harness-independent infrastructure for AI-assisted software development. It stores tasks, workflows, approvals, run records, decisions, findings, and knowledge context as readable files under the repository-local `.devsys/` directory. A shared application layer enforces validation, concurrency control, and recovery for both CLI and MCP clients.

You can switch between Codex, Claude Code, OpenCode, or another agent without losing project state, and without operating a cross-project database or resident daemon.

## Why Workloom

| Common problem | Workloom's approach |
|---|---|
| Project state is locked inside one harness | The state model is independent; agents connect through MCP or CLI |
| Context is difficult to recover on another device | `.devsys/` travels with Git clone, pull, and checkout |
| Automation edits files and bypasses project rules | Writes pass through application services, version guards, gates, and transactions |
| An agent reports completion without code evidence | Completion checks the worktree HEAD and routes insufficient evidence to human review |
| Project knowledge becomes stale quickly | Knowledge freshness is measurable, with protected pages and incremental refresh |
| A dashboard becomes a second source of truth | Dashboards and static sites are projections; `.devsys/` remains authoritative |

## Core Capabilities

- **Git-native state**: readable, auditable, and diffable YAML, JSONL, and Markdown.
- **Harness independence**: built-in adapters for Codex, Claude Code, OpenCode, and Shell.
- **Project planning context**: project blueprint references, goals, scope, constraints, milestones, and current state.
- **Dynamic task system**: agents can create tasks and subtasks from the plan, then maintain priorities and dependencies.
- **Complete workflows**: steps, conditions, gates, approvals, claims, leases, retries, and recovery.
- **Native MCP integration**: capabilities scoped by `session`, `executor`, `reviewer`, and `admin` profiles.
- **Reliable writes**: atomic replacement, optimistic concurrency, file locks, cross-file transactions, and crash recovery.
- **Context and knowledge**: task context assembly, scanning, validation, freshness, and incremental refresh.
- **Verifiable execution**: isolated worktrees, run event streams, completion checks, and explicit human overrides.
- **Read-only workspace views**: terminal summaries, offline static sites, and a local read-only server.
- **Cross-device handoff**: divergence checks, lease audits, conservative archives, and deterministic repair.

## From Project Blueprint to Delivery

Workloom manages more than an individual agent run. It preserves the complete project context from planning through delivery:

| Layer | What Workloom manages | What an agent can do |
|---|---|---|
| **Project blueprint** | Goals, scope, constraints, technology stack, and a blueprint Artifact reference | Read project direction and ask the user for clarification when no blueprint exists |
| **Milestones and state** | Current phase, milestones, risks, blockers, and next priorities | Summarize progress and decide whether to continue delivery or return to planning |
| **Dynamic tasks** | Tasks, subtasks, priorities, acceptance criteria, and dependencies | Break goals into work and create follow-up tasks discovered during execution |
| **Workflows** | Steps, conditions, stage gates, quality gates, and approval policies | Select an appropriate workflow and advance through its rules |
| **Execution and verification** | Agent claims, worktrees, Runs, logs, retries, and completion evidence | Make code changes and prove completion with Git and test evidence |
| **Project memory** | Decisions, findings, artifacts, events, and knowledge context | Leave traceable, recoverable context for the next agent |

```text
Project blueprint
   ↓
Goals / scope / milestones
   ↓
Dynamic tasks ── subtasks ── dependencies
   ↓
Workflows ── gates ── approvals
   ↓
Agent execution ── verification ── human review
   ↓
Project state and knowledge ──→ next planning cycle
```

## Architecture

<div align="center">
  <img src="assets/workloom-architecture.svg" width="100%" alt="Workloom architecture: coding agents connect through MCP, CLI, or file exchange to one core; state lives under .devsys and is synchronized by Git">
</div>

Every interface shares the business rules in `internal/app`. CLI and MCP are not separate implementations, so an agent cannot bypass gates, approvals, version guards, or transaction recovery by changing interfaces.

```text
Coding Agents
    │
    ├── MCP ──┐
    ├── CLI ──┼── Workloom Core ── .devsys/ ── Git
    └── Files ┘      │
                     ├── Workflows & approvals
                     ├── Dispatch & worktrees
                     ├── Context & knowledge
                     └── Transactions & recovery
```

## Quick Start

### Option A: Let Your Agent Set It Up (Recommended)

You do not need to learn the Workloom commands first. Open Codex, Claude Code, OpenCode, or another coding agent that can run terminal commands, then paste this prompt:

```text
Help me set up Workloom (https://github.com/JAYY513/Workloom) in the current project:

1. If workloom is not installed (`workloom --version` prints nothing): try
   `npm install -g @kaki317/workloom` (Node.js >= 18) first; if the platform
   is unsupported or it fails, install it per §1 of the repository's
   INSTALL.md, then confirm the version.
2. Run init in the target project directory (if it is already a git
   repository, prefer the repository root; a non-git prototype may init
   directly). Then complete onboarding per §2 of INSTALL.md (init →
   starter workflow → wire → wire --check → prime → blueprint check).

Stop and report on any failure; never skip the hash verification when you
take the script install path; never edit managed files under .devsys/ directly
(all writes go through the workloom CLI or MCP). If one step genuinely needs
me, name that step and continue with the rest.
```

After setup, you can work with your agent in plain language:

```text
Use Workloom to read the project blueprint, goals, scope, milestones, and
current state first. Then plan and implement user login: break the work into
tasks and subtasks with acceptance criteria, maintain their dependencies,
follow the appropriate workflow, and pause when you need my decision or approval.
```

```text
Continue the previous work. First load the current milestone, unfinished tasks,
related decisions, and risks from Workloom. Create follow-up tasks and connect
their dependencies when new work is discovered. Record verification evidence,
update project state, and summarize the result and remaining risks.
```

The agent learns the operating rules from `AGENTS.md` and `.agents/skills/devsys/`, then restores context through `workloom prime`. You can still review every state change in Git.

### Option B: Manual Setup

### 1. Install

The full install-and-onboard flow (verification, fallback, troubleshooting)
lives in [INSTALL.md](../INSTALL.md); the shortest path is below.

```bash
npm install -g @kaki317/workloom   # Node.js >= 18
workloom --version                 # confirm
```

That is the whole install: upgrading is running the same command again, and
npm handles the platform binary and integrity. The platform package is
currently published for Windows x64 only; for macOS / Linux / Windows ARM64,
the release scripts, `go install`, and building from source, see
[INSTALL.md](../INSTALL.md) §1.

> Renaming compatibility: the old primary command `devsys` still works as an
> alias for the same binary, and the state directory stays `.devsys/`. The npm
> package ships only the `workloom` bin; the install scripts ship both names.

### 2. Initialize a Project

Run one command in any directory (a Git repository root is best; a non-Git directory works too):

```bash
workloom setup
```

It runs `init` (idempotent) → installs the `quick-fix` starter workflow (an
existing file is never overwritten) → `wire` (AGENTS.md discipline block +
agent skill) → config validation → environment checks → `prime` → blueprint
check (a missing blueprint is reported, never guessed) → `doctor`, and stops
at the first hard failure. Re-running is safe: existing files always win.

Once the binary is in place, `workloom mcp install` registers devsys with the
detected MCP clients (Codex / Claude Code / OpenCode; `--client` forces one).
The default is a dry-run. `--apply` writes; an existing `devsys` entry is left
alone unless `--force` replaces that entry. JSONC and an inline Codex
`mcp_servers` table are refused, not rewritten; use `wire --print-mcp` to paste
the entry by hand. After `--apply`, `mcp install` starts the registered server
once as a startup check (MCP handshake + tool list) and reports
`probe: ok (N tools in Tms)`; if it cannot start, the report says so and the
command exits 3 — the config is written, but nothing would connect. Use
`DEVSYS_MCP_PROBE=0` to skip the check where child processes are not allowed
(the report then reads `skipped`, never a pass).

If a client is not detected automatically, or you are using another Agent that supports stdio MCP, print the client-specific snippet:

```bash
workloom wire --print-mcp codex
workloom wire --print-mcp claude
workloom wire --print-mcp opencode
```

Paste the output into that Agent's MCP configuration. The generic MCP command is
`workloom mcp serve --profile session,executor --tier core`, with the server name
`devsys`; do not use `npx` as a long-lived MCP command. If the client cannot find
`workloom`, use the absolute path to `workloom.exe`, or use the npm wrapper command
printed by Workloom.

### Generic stdio MCP configuration

For an Agent that accepts the common `mcpServers` JSON shape, copy this configuration directly:

```json
{
  "mcpServers": {
    "devsys": {
      "type": "stdio",
      "command": "workloom",
      "args": [
        "mcp", "serve",
        "--profile", "session,executor",
        "--tier", "core"
      ]
    }
  }
}
```

This assumes `npm install -g @kaki317/workloom` has been run and `workloom` is
on `PATH`. Put the configuration in the target project's Agent configuration,
or launch the Agent from the project root; Workloom searches upward for
`.devsys/`. If the Agent does not inherit `PATH`, replace `command` with the
absolute path to `workloom.exe` on that machine.

The underlying commands remain for step-by-step use or a different template:

```bash
workloom init
workloom config check
workloom wire
workloom session start
workloom setup --template reference-template   # or workflow init --template <id> for the workflow alone
workloom workflow check                        # validates every policy file
```

Template ids are single-sourced: for `setup --template` and
`workflow init --template`, the full list: the command's own usage output
(embedded in the binary).

`workloom init` creates `.devsys/` idempotently. `wire` adds concise collaboration rules to `AGENTS.md` while preserving hand-written content.
A fresh project has an empty `.devsys/workflows/`. Tasks run without a policy, but the round prompt handed to an agent then carries no process rules.

### 3. Use It

Day-to-day usage — creating and advancing work, quality and stage gates,
connecting an MCP client, inspecting project state — lives in the handbook
(`docs/使用手册.md`). The three most used commands:

```bash
workloom next            # what to do now (read-only)
workloom prime           # session starter: project facts, in-flight work, next action
workloom workitem create # entry point for new work
```

## A Typical Loop

```text
Define workflow → Create work item → Readiness check → Agent claim
       ↑                                             ↓
Human approval ← Review & verify ← Record evidence ← Isolated worktree
```

1. A workflow defines steps, conditions, gates, approvals, and execution limits.
2. `next` evaluates risk without writing and recommends a deterministic next action.
3. `dispatch` recovers interrupted state, plans capacity, claims work, and starts the selected harness.
4. The agent runs in an isolated worktree; logs, commands, and results enter the Run event stream.
5. `run complete` verifies that Git HEAD actually advanced; insufficient evidence fails closed.
6. Decisions and findings propagate by reference so later agents can recover context.

## State Model

```text
.devsys/
├── project.yaml          # Project identity and metadata
├── config.yaml           # Dispatch, knowledge, workspace, and default-policy settings
├── workitems/            # Work items and workflow instances
├── workflows/            # Markdown workflow policies
├── approvals/            # Approval requests and consumption state
├── runs/                 # Run summaries and JSONL event streams
├── decisions/            # Architecture and product decisions
├── findings/             # Research, risks, and findings
├── artifacts/            # Artifacts and immutable version chains
├── events/               # Monthly project event streams
├── knowledge/            # Knowledge snapshots and freshness state
└── archive/              # Auditable conservative archives
```

`.devsys/` is the only source of truth. Read-only tools may consume these files directly; all runtime writes should go through the CLI or MCP to retain concurrency and consistency guarantees.

## Design Principles

1. **Project autonomy**: state belongs to the project, not to one machine, agent, or SaaS.
2. **Evidence before status**: work cannot be marked complete without verifiable evidence.
3. **Fail closed**: version conflicts, broken policies, missing approvals, and incomplete recovery stop writes.
4. **Text first**: humans can read the data, Git can review it, and simple tools can exchange it.
5. **Projections are not facts**: dashboards, static sites, and Contrabass are execution or observation backends.
6. **Single-writer handoff**: cross-device operation uses explicit handoff, not concurrent multi-device writes.

## Documentation

The detailed project documentation is currently maintained in Chinese.

| Document | Purpose |
|---|---|
| [User guide](使用手册.md) | 15-minute setup, daily operations, and complete command paths |
| [Release runbook](发布流程.md) | Cutting a release: tag/Actions triggers, artifact verification, rollback, failure modes |
| [Migration guide](迁移指南.md) | Schema upgrades, binary replacement, and rollback |
| [Design doc](design.md) | Architecture principles, data model, and trade-offs (living doc, in Chinese) |
| [Contributing](../CONTRIBUTING.md) | Environment, commit conventions, documentation discipline |
| [Changelog](../CHANGELOG.md) | Version history and user-visible changes |
| [RepoWiki](repowiki/index.md) | Generated module-level project knowledge |

## Development

Go 1.26 and Git are required:

```bash
make build
go test ./...
go vet ./...
```

The project includes layered unit tests, CLI/MCP integration tests, and `scripts/smoke-m*.{sh,ps1}` end-to-end scenarios. Tests prioritize behavior, boundaries, and regressions over coverage as a vanity metric.

## Status and Boundaries

Workloom has completed milestones M0–M9 and passed all 17 documented success criteria. It is still **pre-release** software.

- Core operation is local-first through the CLI, stdio MCP, and Git; no remote coordination service is provided.
- Cross-device collaboration uses a single-writer handoff. Concurrent writes are resolved manually through Git.
- The Contrabass fixture loop is verified, while fields from a real deployment remain marked `[UNVERIFIED]`.
- Licensed under MIT (see [../LICENSE](../LICENSE)); contribution conventions in [../CONTRIBUTING.md](../CONTRIBUTING.md), version history in [../CHANGELOG.md](../CHANGELOG.md).

## Contributing

The full guide is [../CONTRIBUTING.md](../CONTRIBUTING.md). Before submitting a change:

1. Read the principles in the [design doc](design.md).
2. Add high-value tests for behavior changes, then run `go test ./...` and `go vet ./...`.
3. Keep `.devsys/` as the only source of truth; do not add write paths that bypass the application service.
4. Explain the motivation, compatibility impact, verification, and rollback path in the pull request.

---

<div align="center">
  <sub>Workloom keeps the project memory with the project.</sub>
</div>
