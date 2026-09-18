package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"workloom/internal/storage"
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
}

// readRunStream parses a run's stream. A missing stream reads as empty, and a
// torn tail left by an interrupted writer yields the records that completed
// before it — reading must not be blocked by a tail the writer repairs on its
// next open.
func readRunStream(root, runID string) ([]streamRecord, error) {
	path := filepath.Join(root, ".devsys", "runs", runID+".jsonl")
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
