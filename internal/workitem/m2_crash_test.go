package workitem

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/storage"
)

// Child-process mode names, mirroring internal/storage's re-exec pattern.
const (
	envWICrashHelper = "DEVSYS_WI_CRASH_HELPER"
	envWICrashRoot   = "DEVSYS_WI_CRASH_ROOT"
	envWICrashAt     = "DEVSYS_WI_CRASH_AT"
	exitCodeCrash    = 9
)

// crashStages are the storage stage names (internal/storage/txn.go:93-94):
// a crash after journal-durable leaves the transaction undecided
// (discarded); a crash after committed leaves it decided (replayed).
var crashStages = []string{"journal-durable", "committed"}

// TestHelperProcessCrash is not a real test: it implements the child that
// claims a work item and is killed mid-transaction, so the parent observes a
// real process interruption rather than a simulated torn file.
func TestHelperProcessCrash(t *testing.T) {
	if os.Getenv(envWICrashHelper) == "" {
		t.Skip("helper mode not requested")
	}
	storage.TxnStageHookForTest(func(stage string) {
		if stage == os.Getenv(envWICrashAt) {
			os.Exit(exitCodeCrash)
		}
	})
	s := New(os.Getenv(envWICrashRoot))
	ctx := context.Background()
	_, raw, err := s.ReadSnapshot(ctx, "WLM-1")
	if err != nil {
		t.Fatalf("helper ReadSnapshot: %v", err)
	}
	if _, err := s.Claim(ctx, "WLM-1", ClaimOptions{
		Owner:         "crasher",
		Actor:         "crasher",
		Reason:        "interrupted claim",
		Now:           time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
		Agent:         domain.RunAgent{ID: "crasher", Harness: "test"},
		LeaseDuration: time.Hour,
		Expected:      raw,
	}); err != nil {
		t.Fatalf("helper claim: %v", err)
	}
	os.Exit(0) // hook never fired: parent fails on the missing crash
}

// TestClaimCrashMidTransaction kills a child process at each durable claim
// stage and verifies the M2.5 acceptance: a reader never observes a
// half-committed claim (半提交状态不得对外呈现) — the lease file, the
// scheduling state and the claimed event always agree, because the
// transaction is decided at the commit marker: journal-durable crashes are
// discarded, committed crashes are fully replayed (storage's commit-point
// semantics, TestCrashRecoveryMatrix). Afterwards the project is consistent:
// a discarded claim leaves the item claimable again; a replayed claim stays
// fenced against a second claimer.
func TestClaimCrashMidTransaction(t *testing.T) {
	for _, stage := range crashStages {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			s := m2Store(t)
			id, _ := createReady(t, s)

			cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessCrash")
			cmd.Env = append(os.Environ(),
				envWICrashHelper+"=1",
				envWICrashRoot+"="+s.root,
				envWICrashAt+"="+stage,
			)
			out, err := cmd.CombinedOutput()
			if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != exitCodeCrash {
				t.Fatalf("child exit = %v, want crash %d\n%s", err, exitCodeCrash, out)
			}

			// First read after the crash recovers deterministically; the
			// observation below is therefore always a decided state.
			cur, err := s.Get(ctx, id)
			if err != nil {
				t.Fatalf("read after crash: %v", err)
			}
			leases, err := s.ListLeases(ctx)
			if err != nil {
				t.Fatalf("list leases after crash: %v", err)
			}
			evs, err := events.New(s.root).Read(ctx, events.Filter{Type: "claimed"})
			if err != nil {
				t.Fatal(err)
			}
			leaseVisible := len(leases) > 0
			stateClaimed := cur.SchedulingState == domain.SchedulingClaimed ||
				cur.SchedulingState == domain.SchedulingRunning

			if stage == "committed" {
				// Decided: the whole claim is visible and consistent.
				if !leaseVisible || !stateClaimed || len(evs) != 1 {
					t.Fatalf("replayed claim inconsistent: lease=%v state=%q events=%d",
						leaseVisible, cur.SchedulingState, len(evs))
				}
				// Fencing: the decided claim keeps its owner; a second
				// claimer loses even though the first process is dead.
				if _, err := s.Claim(ctx, id, ClaimOptions{
					Owner: "second-agent", Actor: "second-agent",
					Reason: "must lose to live lease",
				}); !errors.Is(err, ErrAlreadyClaimed) {
					t.Fatalf("second claim = %v, want ErrAlreadyClaimed", err)
				}
				lease, err := s.LeaseInspection(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if lease.Owner != "crasher" {
					t.Fatalf("lease owner = %q, want crasher", lease.Owner)
				}
			} else {
				// Undecided: discarded, claim invisible in full.
				if leaseVisible || stateClaimed || len(evs) != 0 {
					t.Fatalf("discarded claim visible: lease=%v state=%q events=%d",
						leaseVisible, cur.SchedulingState, len(evs))
				}
				// 重新领取: the interrupted claim released cleanly, so the
				// work item is claimable again with exactly one new event.
				_, raw2, err := s.ReadSnapshot(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				res, err := s.Claim(ctx, id, ClaimOptions{
					Owner:         "second-agent",
					Actor:         "second-agent",
					Reason:        "re-claim after crash recovery",
					Now:           time.Date(2026, 9, 18, 13, 0, 0, 0, time.UTC),
					Agent:         domain.RunAgent{ID: "second", Harness: "test"},
					LeaseDuration: time.Hour,
					Expected:      raw2,
				})
				if err != nil {
					t.Fatalf("re-claim after recovery: %v", err)
				}
				if res.Token == "" || res.RunID == "" {
					t.Fatalf("re-claim incomplete: %+v", res)
				}
				evs2, err := events.New(s.root).Read(ctx, events.Filter{Type: "claimed"})
				if err != nil {
					t.Fatal(err)
				}
				if len(evs2) != 1 || !strings.Contains(evs2[0].Content, "re-claim") {
					t.Fatalf("claimed events = %+v, want exactly the re-claim", evs2)
				}
			}

			// Recovery already ran inside the first read: a second pass
			// must be a no-op (deterministic replay is idempotent).
			st, err := storage.Open(s.root, storage.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rep, err := st.Recover(ctx)
			if err != nil {
				t.Fatalf("second recover: %v", err)
			}
			if !rep.Empty() {
				t.Fatalf("second recover = %+v, want nothing to do", rep)
			}
		})
	}
}
