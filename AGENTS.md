## Dev Pipeline

All work flows through a shared pipeline. Dashboard: http://localhost:3422

Pipeline stages: **backlog → spec → plan → implement → test → review → done**

### How to use

1. **Check for work first** — call `task_list` to see what's in flight
2. **Create tasks** — `task_create` with title, description, priority, project
3. **Claim it** — `task_stage` with `action: "claim"` assigns it to your session and advances from backlog
4. **Do the work** — advance through stages with `task_stage` (`action: "advance"`)
5. **Attach artifacts** — at each stage, use `task_artifact` (specs, plans, code summaries, test results)
6. **Complete** — `task_stage` with `action: "complete"` when done, or `action: "fail"` if abandoned

### Inbox

Ideas that are not work yet: `task_config` with `action: "inbox_add"` (title; optional note / source_task_id). They are **not tasks** — not claimable, never in `task_list(next: true)`.

- Must have a **project**: pass it, set it on `task_config action=session`, or inherit from `source_task_id`
- Before `task_create`, `inbox_list` this project. Same idea and doing it now → `inbox_promote`. Still later → update the note. Should wait → `inbox_add`, do not create a task
- Mid-work spark: `inbox_add` with `source_task_id`, keep the current task
- Do not read inbox every turn, or on `next` / claim

### Rules

- **Never work without a task** — if there isn't one, create it
- **Never skip stages** — move through spec → plan → implement → test → review
- **Attach artifacts at each stage** — the pipeline is only useful if it shows what was decided/built/tested
- **Always complete or fail your tasks before stopping**
- Break large work into sub-tasks with `task_create` (`parent_id`) or dependencies via `task_update` (`dependency` field)

### Communicate around tasks (requires agent-comm)

If agent-comm is available, coordinate task work with other agents:

- **Claiming a task** — post to "general": "Claiming task #42: implement auth module"
- **Advancing a stage** — post to "general": "Task #42 moved to test — all unit tests passing"
- **Blocked on a dependency** — post to "general": "Blocked on task #38, need the DB schema first"
- **Editing shared files** — use `comm_state_set("locks", "path/to/file", "my-name")` before editing, `comm_state_delete` when done
- **Finishing work** — post a summary to "general" before stopping

<!-- repowiki:begin | 由 repowiki 管理：运行 `repowiki init` 原位更新本区块；手写内容请放在标记之外 -->

## repowiki

- docs/repowiki/ — 自动生成的项目知识（未人工验证）

## Wiki 纪律

- 涉及本项目代码理解、修改、排障前，先按 `repowiki` 读取相应内容（按页面 triggers 命中加载，禁止全文扫描）。
- 提交前 / 任务收尾前，运行 `repowiki status` 自查是否过期（退出码 10 = 过期 → 提示用户运行 /repowiki-gen）。
<!-- repowiki:end -->
