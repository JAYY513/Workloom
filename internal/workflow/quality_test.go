package workflow

import (
	"strings"
	"testing"
)

func TestScoreQualityTitleBoundaries(t *testing.T) {
	// CJK glyphs weigh double (#342): 7 Chinese characters already clear the
	// 8-char bar. ASCII rows pin the unweighted behavior.
	for _, tc := range []struct {
		title string
		want  int
	}{
		{strings.Repeat("题", 0), 0},
		{strings.Repeat("题", 7), 10},
		{strings.Repeat("题", 8), 20},
		{strings.Repeat("题", 15), 20},
		{strings.Repeat("题", 16), 20},
		{strings.Repeat("t", 7), 0},
		{strings.Repeat("t", 8), 10},
		{strings.Repeat("t", 16), 20},
	} {
		res := ScoreQuality(QualityInput{Title: tc.title})
		if res.Score != tc.want {
			t.Errorf("title %q: score=%d want %d", tc.title, res.Score, tc.want)
		}
	}
}

func TestScoreQualityDescriptionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		desc string
		want int
	}{
		{strings.Repeat("述", 0), 0},
		{strings.Repeat("述", 20), 15},
		{strings.Repeat("述", 60), 30},
		{strings.Repeat("d", 39), 0},
		{strings.Repeat("d", 40), 15},
		{strings.Repeat("d", 120), 30},
	} {
		res := ScoreQuality(QualityInput{Description: tc.desc})
		if res.Score != tc.want {
			t.Errorf("description len %d: score=%d want %d", len(tc.desc), res.Score, tc.want)
		}
	}
}

func TestScoreQualityComponents(t *testing.T) {
	// Structure: two non-empty lines score 15 on their own.
	if res := ScoreQuality(QualityInput{Description: "one\ntwo"}); res.Score != 15 {
		t.Errorf("structure: %+v", res)
	}
	// Acceptance criteria: 15 for the criteria component plus 10 because
	// structured criteria satisfy the acceptance-language component (#342).
	if res := ScoreQuality(QualityInput{AcceptanceCriteria: []string{"passes go test"}}); res.Score != 25 {
		t.Errorf("criteria: %+v", res)
	}
	// Concrete reference: 10 on top of the title component.
	if res := ScoreQuality(QualityInput{Title: "see internal/cli/cli.go"}); res.Score != 30 {
		t.Errorf("reference: %+v", res)
	}
	// Acceptance language via keyword: 10 on its own; a 5-glyph Chinese
	// title now also clears the (weighted) title bar.
	if res := ScoreQuality(QualityInput{Title: "验收怎么做"}); res.Score != 20 {
		t.Errorf("acceptance language: %+v", res)
	}
}

func TestScoreQualityPoorTaskScoresZeroWithAllImprovements(t *testing.T) {
	res := ScoreQuality(QualityInput{Title: "fix", Description: "x"})
	if res.Score != 0 {
		t.Fatalf("score = %d", res.Score)
	}
	if len(res.Improvements) != 6 {
		t.Fatalf("improvements = %q", res.Improvements)
	}
	for _, want := range []string{"标题过短", "描述过短", "描述缺少结构", "缺少验收标准", "缺少具体引用", "缺少验收语言"} {
		found := false
		for _, got := range res.Improvements {
			if strings.Contains(got, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no improvement mentioning %q in %q", want, res.Improvements)
		}
	}
}

func TestScoreQualityRichTaskScoresHundred(t *testing.T) {
	res := ScoreQuality(QualityInput{
		Title:       strings.Repeat("题", 16),
		Description: strings.Repeat("描", 100) + "\n见 internal/cli/cli.go 并写明验收方式",
		AcceptanceCriteria: []string{
			"验收：go test ./... 通过",
		},
	})
	if res.Score != 100 || len(res.Improvements) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestQualityGateBlocksThreshold(t *testing.T) {
	p := gatePolicy(t, "quality_gate:\n  min_score: 55\n")
	if p.QualityGateBlocks(QualityResult{Score: 54}) != true {
		t.Error("54 should block at 55")
	}
	if p.QualityGateBlocks(QualityResult{Score: 55}) != false {
		t.Error("55 should not block at 55")
	}

	// No declared gate leaves MinScore at zero: nothing blocks.
	p = gatePolicy(t, "")
	if p.QualityGateBlocks(QualityResult{Score: 0}) {
		t.Error("absent quality_gate must not block")
	}
}
