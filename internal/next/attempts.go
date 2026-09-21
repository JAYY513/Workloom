package next

import (
	"fmt"
	"sort"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

// SpawnGrace is how long a just-dispatched attempt may exist without a run
// stream before `next` treats it as dead. Dispatch does not wait for the
// child, so a healthy spawn is expected to open its stream inside this window.
const SpawnGrace = 15 * time.Second

// AttemptRef names one in-progress attempt `next` judged.
type AttemptRef struct {
	WorkitemID string `json:"workitem_id"`
	RunID      string `json:"run_id"`
	Detail     string `json:"detail"`
}

// ClassifyAttempts splits in-progress claims into dead (failed or no evidence)
// and healthy in-flight. latest is the newest run per work item; streamLines
// is that run's jsonl record count (0 when the stream does not exist).
func ClassifyAttempts(now time.Time, items []*domain.WorkItem, latest map[string]*domain.Run, streamLines map[string]int) (dead, inflight []AttemptRef) {
	for _, wi := range items {
		if wi == nil || !isAttempted(wi) {
			continue
		}
		r := latest[wi.ID]
		if r == nil {
			continue
		}
		switch r.Status {
		case "failed", "timed_out", "stalled", "canceled":
			dead = append(dead, AttemptRef{
				WorkitemID: wi.ID, RunID: r.ID,
				Detail: fmt.Sprintf("attempt %s ended as %s with no follow-up claim; recover with `%s` or inspect `devsys run get --id %s`",
					r.ID, r.Status, defaultRecoverCommand, r.ID),
			})
		case "succeeded":
			inflight = append(inflight, AttemptRef{
				WorkitemID: wi.ID, RunID: r.ID,
				Detail: fmt.Sprintf("attempt %s succeeded; work item %s is still in_progress", r.ID, wi.ID),
			})
		default:
			n := 0
			if streamLines != nil {
				n = streamLines[r.ID]
			}
			age := time.Duration(0)
			if !r.StartedAt.IsZero() && !now.IsZero() {
				age = now.Sub(r.StartedAt)
			}
			if n == 0 && (r.StartedAt.IsZero() || age >= SpawnGrace) {
				dead = append(dead, AttemptRef{
					WorkitemID: wi.ID, RunID: r.ID,
					Detail: fmt.Sprintf("attempt %s produced no run evidence; recover with `%s` or fail it with `devsys run fail --id %s --actor operator --reason \"attempt produced no evidence\"`",
						r.ID, defaultRecoverCommand, r.ID),
				})
				continue
			}
			inflight = append(inflight, AttemptRef{
				WorkitemID: wi.ID, RunID: r.ID,
				Detail: fmt.Sprintf("attempt %s is running", r.ID),
			})
		}
	}
	return sortedAttemptRefs(dead), sortedAttemptRefs(inflight)
}

func isAttempted(wi *domain.WorkItem) bool {
	if wi.Status == domain.StatusInProgress {
		return true
	}
	switch wi.SchedulingState {
	case domain.SchedulingClaimed, domain.SchedulingRunning:
		return true
	}
	return false
}

func sortedAttemptRefs(in []AttemptRef) []AttemptRef {
	if len(in) == 0 {
		return in
	}
	out := append([]AttemptRef(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].WorkitemID != out[j].WorkitemID {
			return out[i].WorkitemID < out[j].WorkitemID
		}
		return out[i].RunID < out[j].RunID
	})
	return out
}

func inflightIDs(refs []AttemptRef) string {
	ids := make([]string, 0, len(refs))
	for _, a := range refs {
		if a.RunID != "" {
			ids = append(ids, a.RunID)
			continue
		}
		ids = append(ids, a.WorkitemID)
	}
	return joinIDs(ids)
}

func joinIDs(ids []string) string {
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	case 2:
		return ids[0] + ", " + ids[1]
	default:
		return fmt.Sprintf("%s, %s, +%d", ids[0], ids[1], len(ids)-2)
	}
}
