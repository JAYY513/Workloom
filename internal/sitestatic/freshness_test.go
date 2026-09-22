package sitestatic

import (
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/view"
)

// TestFreshnessBannerStale: a stale model gets the banner on every page,
// with the reason and the refresh command.
func TestFreshnessBannerStale(t *testing.T) {
	m := testModel() // KnowledgeStale, "2 pages stale"
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		for _, want := range []string{"知识已过期", "2 pages stale", "workloom knowledge refresh"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s misses %q", page, want)
			}
		}
	}
}

// TestFreshnessBannerStates pins the three non-fresh banners: missing names
// --full, unavailable names no command (refresh would point at nothing).
func TestFreshnessBannerStates(t *testing.T) {
	m := testModel()
	m.Knowledge.Status = view.KnowledgeMissing
	m.Knowledge.Reason = "no page layer"
	out := t.TempDir() + "/missing"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		for _, want := range []string{"知识页面层缺失", "workloom knowledge refresh --full"} {
			if !strings.Contains(content, want) {
				t.Errorf("missing %s misses %q", page, want)
			}
		}
	}

	m.Knowledge.Status = view.KnowledgeUnavailable
	m.Knowledge.Reason = "freshness unavailable: boom"
	out = t.TempDir() + "/unavail"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		if !strings.Contains(content, "知识新鲜度不可判") {
			t.Errorf("unavailable %s misses title", page)
		}
		if strings.Contains(content, "workloom knowledge refresh") {
			t.Errorf("unavailable %s must not suggest a refresh command", page)
		}
	}
}

// TestFreshnessBannerAbsent: a fresh model renders no freshness banner.
func TestFreshnessBannerAbsent(t *testing.T) {
	m := testModel()
	m.Knowledge.Status = view.KnowledgeFresh
	m.Knowledge.Reason = "all 1 page(s) match"
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		for _, banned := range []string{"知识已过期", "知识页面层缺失", "知识新鲜度不可判"} {
			if strings.Contains(content, banned) {
				t.Errorf("fresh %s shows %q", page, banned)
			}
		}
	}
}

// TestKnowledgeHintRow: the knowledge page repeats the hint inline, next to
// the status — a reader who lands there directly still finds the command.
func TestKnowledgeHintRow(t *testing.T) {
	m := testModel()
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	content := readFile(t, out, "knowledge.html")
	for _, want := range []string{"执行以下命令重新生成受影响页面", "workloom knowledge refresh"} {
		if !strings.Contains(content, want) {
			t.Errorf("knowledge.html misses %q", want)
		}
	}
}

// TestFreshnessEscapesReason: the banner reason renders escaped (M7.2
// html/template discipline — no template.HTML anywhere).
func TestFreshnessEscapesReason(t *testing.T) {
	m := testModel()
	m.Knowledge.Reason = "stale <b>now</b>"
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		got := readFile(t, out, page)
		if strings.Contains(got, "<b>now</b>") {
			t.Errorf("%s renders the reason raw", page)
		}
	}
}

// TestFreshnessKeepsOffline: the new banner adds no external references.
func TestFreshnessKeepsOffline(t *testing.T) {
	m := testModel()
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		if extRef.MatchString(readFile(t, out, page)) {
			t.Errorf("%s references the network", page)
		}
	}
}

// TestPendingBannerNamesDoctor: the trust banner keeps its recover wording
// and gains the doctor pointer.
func TestPendingBannerNamesDoctor(t *testing.T) {
	m := testModel()
	m.Trust = view.Trust{State: view.TrustPending, Pending: []string{"tx-1"}, Note: "pending transactions"}
	m.Progress.Items = nil
	m.Progress.Counts = map[string]int{}
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		for _, want := range []string{"workloom recover", "workloom doctor", "tx-1"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s misses %q", page, want)
			}
		}
	}
}
