package workflow

import (
	"strings"
	"testing"
)

func TestScoreQualityTitleBoundaries(t *testing.T) {
	for _, tc := range []struct{ n, want int }{{0, 0}, {7, 0}, {8, 10}, {15, 10}, {16, 20}} {
		res := ScoreQuality(QualityInput{Title: strings.Repeat("题", tc.n)})
		if res.Score != tc.want {
			t.Errorf("title %d runes: score=%d want %d", tc.n, res.Score, tc.want)
		}
	}
}

func TestScoreQualityDescriptionBoundaries(t *testing.T) {
	for _, tc := range []struct{ n, want int }{{0, 0}, {39, 0}, {40, 15}, {119, 15}, {120, 30}} {
		res := ScoreQuality(QualityInput{Description: strings.Repeat("述", tc.n)})
		if res.Score != tc.want {
			t.Errorf("description %d runes: score=%d want %d", tc.n, res.Score, tc.want)
		}
	}
}

func TestScoreQualityComponents(t *testing.T) {
	// Structure: two non-empty lines score 15 on their own.
	if res := ScoreQuality(QualityInput{Description: "one\ntwo"}); res.Score != 15 {
		t.Errorf("structure: %+v", res)
	}
	// Acceptance criteria: 15 (text without acceptance language so only the
	// criteria component scores).
	if res := ScoreQuality(QualityInput{AcceptanceCriteria: []string{"passes go test"}}); res.Score != 15 {
		t.Errorf("criteria: %+v", res)
	}
	// Concrete reference: 10 on top of the title component.
	if res := ScoreQuality(QualityInput{Title: "see internal/cli/cli.go"}); res.Score != 30 {
		t.Errorf("reference: %+v", res)
	}
	// Acceptance language: 10 on its own.
	if res := ScoreQuality(QualityInput{Title: "验收怎么做"}); res.Score != 10 {
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
