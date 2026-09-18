package retry

import (
	"testing"
	"time"
)

// The same failure on a restarted scheduler must produce the same instant.
func TestDelayIsReproducible(t *testing.T) {
	first := Delay(30*time.Second, time.Hour, 3, "WLM-7")
	for i := 0; i < 5; i++ {
		if got := Delay(30*time.Second, time.Hour, 3, "WLM-7"); got != first {
			t.Fatalf("delay %d = %v, want %v (must be reproducible)", i, got, first)
		}
	}
	if other := Delay(30*time.Second, time.Hour, 3, "WLM-8"); other == first {
		t.Fatalf("two different work items got the same delay %v, want the jitter to separate them", first)
	}
}

// The backoff doubles per attempt and never exceeds the ceiling.
func TestDelayGrowsAndStaysUnderTheCeiling(t *testing.T) {
	base, max := 10*time.Second, 5*time.Minute
	previous := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		delay := Delay(base, max, attempt, "WLM-1")
		if delay > max {
			t.Fatalf("attempt %d delay = %v, above the ceiling %v", attempt, delay, max)
		}
		// The jitter is bounded, so the growth is visible step by step.
		floor := base * (1 << (attempt - 1))
		if floor > max {
			floor = max
		}
		if delay < floor || (floor < max && delay >= floor+floor/5) {
			t.Fatalf("attempt %d delay = %v, want [%v, %v)", attempt, delay, floor, floor+floor/5)
		}
		if attempt > 1 && delay < previous && previous < max {
			t.Fatalf("attempt %d delay %v is shorter than the previous %v", attempt, delay, previous)
		}
		previous = delay
	}
	if got := Delay(base, max, 8, "WLM-1"); got > max {
		t.Fatalf("capped delay = %v, want at most %v", got, max)
	}
}

// The ceiling is hard: a policy may ask for retries sooner than the base, and
// degenerate bounds fall back to the defaults instead of producing nonsense.
func TestDelayHandlesDegenerateBounds(t *testing.T) {
	if got := Delay(0, 0, 1, "seed"); got < DefaultBase || got >= DefaultBase+DefaultBase/5 {
		t.Fatalf("defaults produced %v, want [%v, %v)", got, DefaultBase, DefaultBase+DefaultBase/5)
	}
	if got := Delay(time.Minute, time.Second, 2, "seed"); got > time.Second {
		t.Fatalf("a one-second ceiling produced %v, want at most 1s", got)
	}
	if got := Delay(time.Minute, time.Hour, 0, "seed"); got < time.Minute {
		t.Fatalf("attempt 0 produced %v, want at least the base", got)
	}
}
