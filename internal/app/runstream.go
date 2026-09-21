package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JAYY513/Workloom/internal/prompt"
	"github.com/JAYY513/Workloom/internal/storage"
)

// A run's stream (.devsys/runs/<run-id>.jsonl, 方案 §14.2) is both its evidence
// trail and its bookkeeping: the next round number, the phases a session went
// through and how the last attempt ended are read back from the stream, so a
// fresh process can continue a session without any in-memory state.
//
// One struct covers every record type so the on-disk shape is documented in
// one place and a reader tolerates records written by a different version;
// omitempty keeps each record small. code is -1 when the command has no exit
// status (killed by a signal, or never started).
type streamRecord struct {
	Time       time.Time  `json:"t"`
	Type       string     `json:"type"`
	Stream     string     `json:"stream,omitempty"`
	Line       string     `json:"line,omitempty"`
	Adapter    string     `json:"adapter,omitempty"`
	Command    string     `json:"command,omitempty"`
	Argv       []string   `json:"argv,omitempty"`
	Cwd        string     `json:"cwd,omitempty"`
	TimeoutMS  int64      `json:"timeout_ms,omitempty"`
	Code       *int       `json:"code,omitempty"`
	TimedOut   bool       `json:"timed_out,omitempty"`
	Canceled   bool       `json:"canceled,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	Text       string     `json:"text,omitempty"`
	Removed    int64      `json:"removed_bytes,omitempty"`
	Round      int        `json:"n,omitempty"`
	Mode       string     `json:"mode,omitempty"`
	PromptHash string     `json:"prompt_hash,omitempty"`
	PromptFile string     `json:"prompt_file,omitempty"`
	Refs       []string   `json:"refs,omitempty"`
	Phase      string     `json:"phase,omitempty"`
	Status     string     `json:"status,omitempty"`
	DueAt      *time.Time `json:"continuation_due_at,omitempty"`
	// Context is the round's context snapshot (方案 §11.3): the versions the
	// assembly read, the knowledge revision, and the commit it ran against.
	Context *contextSnapshot `json:"context,omitempty"`
}

// contextSnapshot is the context snapshot a round record carries. It mirrors
// prompt.Snapshot; the field names are the on-disk contract, so they are spelled
// out here rather than inherited from a type that may evolve.
type contextSnapshot struct {
	ProjectStateVersion string   `json:"project_state_version,omitempty"`
	WorkitemVersion     string   `json:"workitem_version,omitempty"`
	ArtifactVersions    []string `json:"artifact_versions,omitempty"`
	KnowledgeRevision   string   `json:"knowledge_revision,omitempty"`
	KnowledgePages      []string `json:"knowledge_pages,omitempty"`
	DecisionIDs         []string `json:"decision_ids,omitempty"`
	WorkspaceHead       string   `json:"workspace_head,omitempty"`
	KnowledgeBehind     bool     `json:"knowledge_behind,omitempty"`
	KnowledgeDegraded   bool     `json:"knowledge_degraded,omitempty"`
}

// snapshotRecord converts a prompt snapshot for the stream; an empty snapshot
// stays absent rather than becoming an empty object.
func snapshotRecord(snapshot prompt.Snapshot) *contextSnapshot {
	if snapshot.Empty() {
		return nil
	}
	return &contextSnapshot{
		ProjectStateVersion: snapshot.ProjectStateVersion,
		WorkitemVersion:     snapshot.WorkitemVersion,
		ArtifactVersions:    snapshot.ArtifactVersions,
		KnowledgeRevision:   snapshot.KnowledgeRevision,
		KnowledgePages:      snapshot.KnowledgePages,
		DecisionIDs:         snapshot.DecisionIDs,
		WorkspaceHead:       snapshot.WorkspaceHead,
		KnowledgeBehind:     snapshot.KnowledgeBehind,
		KnowledgeDegraded:   snapshot.KnowledgeDegraded,
	}
}

// readRunStream parses a run's stream. A missing live stream falls back to
// the archived stream (M8.3): archiving only takes terminal runs, whose
// streams are never written again, so live-wins-or-fallback is exact.
// A torn tail yields the records completed before it.
func readRunStream(root, runID string) ([]streamRecord, error) {
	path := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		archived := filepath.Join(root, ".devsys", "archive", "runs", runID+".jsonl")
		if _, aerr := os.Stat(archived); aerr == nil {
			path = archived
		}
	}
	var lines []streamRecord
	err := storage.ScanJSONL(path, func(raw []byte) error {
		var line streamRecord
		if err := json.Unmarshal(raw, &line); err != nil {
			return fmt.Errorf("run stream %s: %w", path, err)
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrIncompleteTail) {
		return nil, err
	}
	return lines, nil
}

// nextRound is the round a new attempt becomes: the highest recorded round
// plus one (1 when nothing has run yet).
func nextRound(lines []streamRecord) int {
	highest := 0
	for _, line := range lines {
		if line.Type == "round" && line.Round > highest {
			highest = line.Round
		}
	}
	return highest + 1
}

// lastRound returns the newest round record.
func lastRound(lines []streamRecord) (streamRecord, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Type == "round" {
			return lines[i], true
		}
	}
	return streamRecord{}, false
}

// lastExit returns the newest exit record, which carries how the attempt ended.
func lastExit(lines []streamRecord) (streamRecord, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Type == "exit" {
			return lines[i], true
		}
	}
	return streamRecord{}, false
}

// recordedPhases lists the phases the stream went through, in order.
func recordedPhases(lines []streamRecord) []string {
	var out []string
	for _, line := range lines {
		if line.Type == "phase" && line.Phase != "" {
			out = append(out, line.Phase)
		}
	}
	return out
}
