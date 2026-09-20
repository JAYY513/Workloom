package app

// The optimistic-concurrency message must name the way out: a version conflict
// is 方案 §15.2 losing to a concurrent writer (a dispatch child writing the run
// is the usual cause of `run fail` answering "storage: version conflict").

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"workloom/internal/storage"
)

func TestStoreErrorNamesTheConcurrentWriteEscape(t *testing.T) {
	svc := New(t.TempDir())
	err := svc.storeError(fmt.Errorf("runs/run-20260920-1.yaml: read: %w", storage.ErrConflict))
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("storeError(%v) = %T, want an application error", storage.ErrConflict, err)
	}
	if ae.Kind != KindWorkitem || ae.Class() != KindInvalid {
		t.Errorf("kind/class = %q/%q, want workitem/invalid", ae.Kind, ae.Class())
	}
	for _, want := range []string{"version conflict", "changed after your read", "--latest"} {
		if !strings.Contains(ae.Message, want) {
			t.Errorf("message = %q, want %q", ae.Message, want)
		}
	}
}
