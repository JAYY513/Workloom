// Package retry computes when a failed or stalled attempt may run again
// (方案 §15.4): an exponential backoff with a reproducible jitter.
//
// Reproducible means the delay is a pure function of the attempt number and a
// stable seed (the work item's identifier): the same failure on a restarted
// scheduler produces the same instant, which is what makes "到期判定在 tick 时
// 求值" checkable. Randomness would make the plan unreproducible across
// restarts, so the jitter is derived, not drawn.
package retry

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// DefaultBase is the delay before the second attempt when nothing declares one.
const DefaultBase = 30 * time.Second

// DefaultMax caps the delay when nothing declares one (方案 §15.4 退避上限).
const DefaultMax = time.Hour

// DefaultStallThreshold is how long an attempt may make no progress before the
// tick treats it as stalled (方案 §15.4) when the policy declares none.
const DefaultStallThreshold = 30 * time.Minute

// jitterPercent bounds the derived jitter: a delay lands in
// [delay, delay + delay·percent/100), so several items failing at once do not
// retry in lockstep while the schedule stays reproducible — and because the
// jitter is a fraction of the delay itself, doubling still dominates it.
const jitterPercent = 20

// Delay is the wait before the given attempt (1 = the first retry, i.e. the
// second attempt). base and max are the policy's bounds; zero or negative
// values fall back to the defaults. max is a hard ceiling: a policy may ask
// for retries sooner than the base (a small backoff_max_seconds is a legitimate
// "retry quickly"), and the jitter is clamped below it.
func Delay(base, max time.Duration, attempt int, seed string) time.Duration {
	if base <= 0 {
		base = DefaultBase
	}
	if max <= 0 {
		max = DefaultMax
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= max {
			delay = max
			break
		}
	}
	if delay > max {
		delay = max
	}
	// The jitter is a deterministic fraction of the delay itself, clamped by
	// the ceiling: growth stays exponential (each step at least doubles the
	// floor) and no delay ever exceeds the policy's bound.
	jitter := delay * time.Duration(derivedFraction(seed, attempt, jitterPercent)) / 100
	if delay+jitter > max {
		return max
	}
	return delay + jitter
}

// derivedFraction maps (seed, attempt) onto [0, percent) without any state:
// the same inputs always yield the same fraction.
func derivedFraction(seed string, attempt, percent int) int {
	if percent <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(seed + "#" + itoa(attempt)))
	return int(binary.BigEndian.Uint32(sum[:4]) % uint32(percent))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
