// Package prompt assembles the text one attempt round hands to a harness
// (方案 §4.8): the first round carries the full task brief, later rounds carry
// only what changed and what is left, so a session's context is not rebuilt
// from scratch every round.
//
// Assembly is a pure function of its input — the caller injects everything
// (task, policy, state, context, the boundary time) — so any round's text can
// be replayed and compared offline, and the stored hash always identifies the
// text the agent actually received.
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"workloom/internal/workflow"
)

// Mode is the kind of round a prompt belongs to.
type Mode string

const (
	// ModeFull is the first round: task brief, policy body, state, context
	// pointers and the reporting protocol.
	ModeFull Mode = "full"
	// ModeContinuation is every later round: what changed and what is left.
	ModeContinuation Mode = "continuation"
)

// Section is one titled block of the assembled text.
type Section struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Prompt is one assembled round.
type Prompt struct {
	Mode     Mode      `json:"mode"`
	Round    int       `json:"round"`
	Text     string    `json:"text"`
	Hash     string    `json:"hash"`
	Sections []Section `json:"sections"`
	Refs     []string  `json:"refs"`
	// Snapshot records what this round's context was assembled from, so a later
	// reader can ask what the agent saw and why it decided (方案 §11.3).
	Snapshot Snapshot `json:"snapshot"`
}

// Empty reports whether the snapshot carries nothing worth recording.
func (s Snapshot) Empty() bool {
	return s.ProjectStateVersion == "" && s.WorkitemVersion == "" && len(s.ArtifactVersions) == 0 &&
		s.KnowledgeRevision == "" && len(s.KnowledgePages) == 0 && len(s.DecisionIDs) == 0 &&
		s.WorkspaceHead == "" && !s.KnowledgeBehind && !s.KnowledgeDegraded
}

// Snapshot is the context record of one round (方案 §11.3): the versions the
// assembly read, the knowledge revision it used, and the workspace commit it
// ran against. Knowledge is never split per worktree; a workspace ahead of the
// knowledge baseline is annotated instead.
type Snapshot struct {
	ProjectStateVersion string   `json:"project_state_version,omitempty"`
	WorkitemVersion     string   `json:"workitem_version,omitempty"`
	ArtifactVersions    []string `json:"artifact_versions,omitempty"`
	KnowledgeRevision   string   `json:"knowledge_revision,omitempty"`
	KnowledgePages      []string `json:"knowledge_pages,omitempty"`
	DecisionIDs         []string `json:"decision_ids,omitempty"`
	WorkspaceHead       string   `json:"workspace_head,omitempty"`
	// KnowledgeBehind is true when the workspace is ahead of the knowledge
	// baseline: the pages describe older code than the round ran against.
	KnowledgeBehind bool `json:"knowledge_behind,omitempty"`
	// KnowledgeDegraded is true when no page layer exists at all.
	KnowledgeDegraded bool `json:"knowledge_degraded,omitempty"`
}

// Task is the work item brief.
type Task struct {
	ID                 string
	Title              string
	Type               string
	Status             string
	Description        string
	Spec               string
	AcceptanceCriteria []string
	Constraints        []string
	Dependencies       []string
	WorkflowID         string
	WorkflowStep       string
	WorkflowPaused     bool
}

// Policy is the workflow policy whose body is the round's prompt template.
type Policy struct {
	ID   string
	Body string
	Vars map[string]string
}

// State is the readiness picture the round starts from (方案 §7.4).
type State struct {
	Verdict string
	Next    string
	Risks   []string
}

// Ref is one context pointer: identity and title, never a body — the agent
// reads bodies on demand through the tool surface (方案 §11.2).
type Ref struct {
	Ref   string
	Title string
	Extra string
}

// Context is the layered context handed to a round.
type Context struct {
	Decisions []Ref
	Findings  []Ref
	Artifacts []Ref
	Comments  []Ref
	// Knowledge lists the knowledge pages this task touches: the third layer of
	// 方案 §11.1, loaded on demand and addressed by path.
	Knowledge []Ref
	Notes     []string
}

// Change is one thing that happened since the previous round.
type Change struct {
	Kind   string
	Ref    string
	Detail string
}

// Input is everything the assembly reads.
type Input struct {
	Round     int
	Task      Task
	Policy    Policy
	State     State
	Context   Context
	Snapshot  Snapshot
	Changes   []Change
	Remaining []string
}

// protocol is the reporting contract every round carries: agents report
// through the same surfaces humans use, so evidence never lives only in the
// harness's own transcript (方案 §8/§9.2).
const protocol = `- 通过 CLI 或 MCP 汇报（两者同一实现）：` + "`devsys ...`" + ` 与同名 MCP 工具等价。
- 进度：` + "`devsys run update --id $DEVSYS_RUN_ID --log <一行说明>`" + `（可多次）。
- 产物：` + "`devsys artifact register --name <名称> --path <路径> --run $DEVSYS_RUN_ID --related $DEVSYS_WORKITEM`" + `。
- 决策：` + "`devsys decision create --title <标题> --decision <结论>`" + `；发现：` + "`devsys finding create --title <标题> --description <说明>`" + `。
- 完成：` + "`devsys run complete --id $DEVSYS_RUN_ID --actor <身份> --reason <原因>`" + `；失败：` + "`devsys run fail ...`" + `。
- 退出码 0 表示本轮成功，非零表示失败（失败原因写 stderr）。`

// Assemble builds one round's text. Round 1 is the full brief; later rounds
// carry the delta only, per §4.8 (a session's thread already holds the rest).
func Assemble(in Input) (Prompt, error) {
	if in.Round < 1 {
		return Prompt{}, fmt.Errorf("prompt round must be at least 1, got %d", in.Round)
	}
	if in.Task.ID == "" {
		return Prompt{}, fmt.Errorf("prompt assembly requires a work item")
	}
	mode := ModeContinuation
	if in.Round == 1 {
		mode = ModeFull
	}

	var sections []Section
	if mode == ModeFull {
		body := in.Policy.Body
		if strings.TrimSpace(body) != "" {
			rendered, err := workflow.Render(body, in.Policy.Vars)
			if err != nil {
				return Prompt{}, fmt.Errorf("render policy %s body: %w", in.Policy.ID, err)
			}
			body = rendered
		}
		sections = []Section{
			{Title: "任务", Body: taskSection(in.Task)},
			{Title: "工作流策略正文", Body: strings.TrimRight(body, "\n")},
			{Title: "当前状态", Body: stateSection(in.State)},
			{Title: "上下文引用", Body: contextSection(in.Context)},
		}
		if knowledgeNotice := knowledgeSection(in.Snapshot); knowledgeNotice != "" {
			sections = append(sections, Section{Title: "知识基线", Body: knowledgeNotice})
		}
		sections = append(sections, Section{Title: "汇报协议", Body: protocol})
	} else {
		sections = []Section{
			{Title: "任务", Body: taskLine(in.Task)},
			{Title: "上一轮以来的变化", Body: changesSection(in.Changes)},
			{Title: "未完成项", Body: listSection(in.Remaining, "（无）")},
			{Title: "汇报协议", Body: "沿用首轮协议：`devsys run update` 记进度、`devsys run complete`/`fail` 收尾；不要重述完整计划，从断点继续。"},
		}
	}

	text := renderSections(sections)
	sum := sha256.Sum256([]byte(text))
	return Prompt{
		Mode:     mode,
		Round:    in.Round,
		Text:     text,
		Hash:     hex.EncodeToString(sum[:]),
		Sections: sections,
		Refs:     refs(in),
		Snapshot: in.Snapshot,
	}, nil
}

// renderSections joins titled blocks; the shape is fixed so two assemblies of
// the same input are byte-identical.
func renderSections(sections []Section) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(s.Title)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(s.Body, "\n"))
	}
	b.WriteString("\n")
	return b.String()
}

func taskLine(t Task) string {
	parts := []string{t.ID, t.Title}
	if t.Type != "" || t.Status != "" {
		parts = append(parts, fmt.Sprintf("（%s/%s）", t.Type, t.Status))
	}
	if t.WorkflowStep != "" {
		step := t.WorkflowStep
		if t.WorkflowPaused {
			step += "，已暂停"
		}
		parts = append(parts, "步骤："+step)
	}
	return strings.Join(parts, " ")
}

func taskSection(t Task) string {
	var b strings.Builder
	b.WriteString(taskLine(t))
	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(t.Description))
	}
	if strings.TrimSpace(t.Spec) != "" {
		b.WriteString("\n\n### 任务规格\n\n")
		b.WriteString(strings.TrimSpace(t.Spec))
	}
	if len(t.AcceptanceCriteria) > 0 {
		b.WriteString("\n\n### 验收标准\n\n")
		b.WriteString(bullets(t.AcceptanceCriteria))
	}
	if len(t.Constraints) > 0 {
		b.WriteString("\n\n### 约束\n\n")
		b.WriteString(bullets(t.Constraints))
	}
	if len(t.Dependencies) > 0 {
		b.WriteString("\n\n### 依赖\n\n")
		b.WriteString(bullets(t.Dependencies))
	}
	return b.String()
}

func stateSection(s State) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("就绪判定：%s", orDash(s.Verdict)))
	if s.Next != "" {
		b.WriteString("\n建议下一步：" + s.Next)
	}
	if len(s.Risks) > 0 {
		b.WriteString("\n风险：\n")
		b.WriteString(bullets(s.Risks))
	}
	return b.String()
}

func contextSection(c Context) string {
	var b strings.Builder
	writeRefs(&b, "决策", c.Decisions)
	writeRefs(&b, "发现", c.Findings)
	writeRefs(&b, "产物", c.Artifacts)
	writeRefs(&b, "评论", c.Comments)
	writeRefs(&b, "相关知识页", c.Knowledge)
	if len(c.Notes) > 0 {
		b.WriteString("说明：\n")
		b.WriteString(bullets(c.Notes))
	}
	if b.Len() == 0 {
		return "（无）"
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeRefs(b *strings.Builder, title string, refs []Ref) {
	if len(refs) == 0 {
		return
	}
	b.WriteString(title + "：\n")
	for _, ref := range refs {
		line := fmt.Sprintf("- %s %s", ref.Ref, ref.Title)
		if ref.Extra != "" {
			line += "（" + ref.Extra + "）"
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
}

// knowledgeSection annotates the knowledge baseline a round runs against
// (方案 §11.3): a workspace ahead of it means the pages describe older code,
// and the round deserves to know that. An absent page layer is stated as the
// documented degradation rather than left silent.
func knowledgeSection(snapshot Snapshot) string {
	switch {
	case snapshot.KnowledgeDegraded:
		return "本项目尚未生成知识页面层（方案 §12.6 降级）：上下文只含记录与事件，按需查阅代码。"
	case snapshot.KnowledgeBehind:
		return fmt.Sprintf("知识基线 %s 落后于工作区 HEAD %s：本页描述的是较早的代码，涉及细节时以代码为准。",
			shortCommit(snapshot.KnowledgeRevision), shortCommit(snapshot.WorkspaceHead))
	default:
		return ""
	}
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func changesSection(changes []Change) string {
	if len(changes) == 0 {
		return "（无）"
	}
	var b strings.Builder
	for _, c := range changes {
		line := fmt.Sprintf("- %s %s", c.Kind, c.Ref)
		if c.Detail != "" {
			line += "：" + c.Detail
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func listSection(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	return bullets(items)
}

func bullets(items []string) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// refs lists every pointer the round refers to, in section order, so a run's
// input_context_refs records what the agent was pointed at (方案 §11.3).
func refs(in Input) []string {
	var out []string
	for _, list := range [][]Ref{in.Context.Decisions, in.Context.Findings, in.Context.Artifacts, in.Context.Comments, in.Context.Knowledge} {
		for _, ref := range list {
			if ref.Ref != "" {
				out = append(out, ref.Ref)
			}
		}
	}
	if in.Policy.ID != "" && in.Round == 1 {
		out = append(out, "workflow://"+in.Policy.ID)
	}
	return out
}
