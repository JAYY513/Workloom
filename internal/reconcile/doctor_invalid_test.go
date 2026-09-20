package reconcile

// Doctor 对损坏 workitem 文件的行为（#337 M6）：文件级报告、继续扫其余；
// RepairDryRun 保持 fail-closed。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corruptWorkitemTree prepares WLM-1 (damaged bytes) + WLM-2 (done, missing
// referenced artifact → downward repair candidate).
func corruptWorkitemTree(t *testing.T) string {
	t.Helper()
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-2", "done", []string{"artifact-999"}))
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workitems", "WLM-1.yaml"),
		[]byte("status: [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDoctorReportsInvalidFilesAndKeepsScanning(t *testing.T) {
	root := corruptWorkitemTree(t)
	rep, err := Doctor(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("doctor: %v (it must report, not fail)", err)
	}
	if len(rep.InvalidFiles) != 1 {
		t.Fatalf("invalid files = %+v, want exactly WLM-1", rep.InvalidFiles)
	}
	if got := rep.InvalidFiles[0]; got.Path != "workitems/WLM-1.yaml" || !strings.Contains(got.Err, "yaml") {
		t.Fatalf("invalid file = %+v, want located yaml error", got)
	}
	found := false
	for _, p := range rep.Orphans {
		if p.WorkitemID == "WLM-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("WLM-2 proposal missing: %+v (the walk must continue)", rep.Orphans)
	}
}

func TestRepairDryRunRefusesUnreadableWorkitems(t *testing.T) {
	root := corruptWorkitemTree(t)
	if _, err := RepairDryRun(context.Background(), root, Options{}); err == nil ||
		!strings.Contains(err.Error(), "workitems/WLM-1.yaml") {
		t.Fatalf("repair plan over unreadable files = %v, want a refusal naming the file", err)
	}
}
