<!-- devsys-skill -->
# Devsys Skill

This project uses Devsys for tracked work. Prefer Devsys MCP tools when
available; otherwise use `devsys --json` through the shell.

## Start

1. Run `devsys session start` (one call: project facts, work in flight, recommended action).
2. Read the recommended work item (`workitem get` / `context get --task <id>`).

## Claim

Claim before tracked implementation (`workitem claim --expect <version>`);
reads after a write must re-read (expired hashes are refused, never forced).

## During work

Record significant findings, decisions and blockers
(`finding/event/decision`); keep the run evidence current (`run update`).

## Complete

1. Verify the implementation (tests or equivalent checks).
2. Complete the run (`run complete`; refused without branch evidence unless reviewed).
3. Transition the work item (`workitem transition --expect <version>`).

## Boundaries

- Never edit `.devsys/` files directly (repair via `devsys repair`).
- See `references/cli.md` for the command table and `references/troubleshooting.md` for exit codes and retries.
