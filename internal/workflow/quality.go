package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// QualityInput is the claim-time evidence the deterministic quality gate
// scores (方案 §4.7). Scoring uses length, structure and language markers
// only — never a model call.
type QualityInput struct {
	Title              string
	Description        string
	AcceptanceCriteria []string
}

// QualityResult is the deterministic quality score. Improvements name the
// components that scored zero. Components always lists every item with its
// current value, earned weight and threshold so a blocked claim can be
// diagnosed without re-scoring.
type QualityResult struct {
	Score        int
	Improvements []string
	Components   []string
}

// referencePattern matches concrete references (paths or file names with a
// known extension) in any of the scored text.
var referencePattern = regexp.MustCompile(`[A-Za-z0-9_./\\-]+\.(?:go|md|yaml|yml|json|ts|tsx|js|ps1|sh|sql|toml|txt|cs|py)\b`)

// weightedLen counts display width for the length components: CJK ideographs
// carry roughly double the information density per glyph, so a 6-character
// Chinese title weighs 12 (#342: quality thresholds must not silently
// penalise Chinese writing). Purely deterministic — no model, no locale.
func weightedLen(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// ScoreQuality computes the claim-time quality score (0..100) from title,
// description and acceptance criteria. Every component is a deterministic
// text measure; the improvements list names the components that scored zero
// so a blocked claim can tell the caller what to add.
func ScoreQuality(in QualityInput) QualityResult {
	var res QualityResult
	note := func(name, current string, weight, max int, threshold, zero string) {
		res.Components = append(res.Components, fmt.Sprintf("%s: 当前 %s / 权重 %d/%d / 阈值 %s", name, current, weight, max, threshold))
		if weight == 0 && zero != "" {
			res.Improvements = append(res.Improvements, zero+"（"+res.Components[len(res.Components)-1]+"）")
		}
	}

	titleLen := weightedLen(strings.TrimSpace(in.Title))
	titleWeight := 0
	switch {
	case titleLen >= 16:
		titleWeight = 20
	case titleLen >= 8:
		titleWeight = 10
	}
	res.Score += titleWeight
	note("标题", fmt.Sprintf("%d", titleLen), titleWeight, 20, "8 得 10、16 得 20",
		"标题过短：补充一个可辨识的任务标题（≥8 字符，中日韩文字按双倍计）")

	desc := strings.TrimSpace(in.Description)
	descLen := weightedLen(desc)
	descWeight := 0
	switch {
	case descLen >= 120:
		descWeight = 30
	case descLen >= 40:
		descWeight = 15
	}
	res.Score += descWeight
	note("描述长度", fmt.Sprintf("%d", descLen), descWeight, 30, "40 得 15、120 得 30",
		"描述过短：补充背景、范围与做法（≥40 字符，中日韩文字按双倍计）")

	lines := nonEmptyLines(desc)
	lineWeight := 0
	if lines >= 2 {
		lineWeight = 15
	}
	res.Score += lineWeight
	note("描述结构", fmt.Sprintf("%d 非空行", lines), lineWeight, 15, "≥2 非空行得 15",
		"描述缺少结构：用分点或步骤组织描述")

	acceptWeight := 0
	if len(in.AcceptanceCriteria) > 0 {
		acceptWeight = 15
	}
	res.Score += acceptWeight
	note("验收标准", fmt.Sprintf("%d 条", len(in.AcceptanceCriteria)), acceptWeight, 15, "≥1 条得 15",
		"缺少验收标准：用 `workloom workitem update --id <id> --acceptance a,b --actor <you> --reason <why>` 补充验收条件")

	text := in.Title + "\n" + in.Description + "\n" + strings.Join(in.AcceptanceCriteria, "\n")
	refWeight := 0
	if referencePattern.MatchString(text) {
		refWeight = 10
	}
	res.Score += refWeight
	note("具体引用", fmt.Sprintf("命中=%t", refWeight > 0), refWeight, 10, "路径或已知扩展名得 10",
		"缺少具体引用：补充文件路径、命令或标识符")

	// Acceptance language: structured criteria satisfy this component (they
	// already say how completion is verified); otherwise the keyword check
	// runs. Demanding the keyword on top of criteria double-counted the same
	// evidence and read as if --acceptance had been ignored (#342).
	langWeight := 0
	langCurrent := "无验收条件且无验收语言"
	if len(in.AcceptanceCriteria) > 0 {
		langWeight = 10
		langCurrent = fmt.Sprintf("%d 条验收条件", len(in.AcceptanceCriteria))
	} else if strings.Contains(text, "验收") || strings.Contains(strings.ToLower(text), "acceptance") {
		langWeight = 10
		langCurrent = "含验收语言"
	}
	res.Score += langWeight
	note("验收语言", langCurrent, langWeight, 10, "验收条件或「验收」/acceptance 得 10",
		"缺少验收语言：写明如何验收（「验收」或 acceptance），或直接 --acceptance 给出验收条件")
	return res
}

// QualityGateBlocks reports whether the score is below the policy's declared
// threshold. An absent quality_gate leaves MinScore at zero, which never
// blocks.
func (p *Policy) QualityGateBlocks(res QualityResult) bool {
	return res.Score < p.QualityGate.MinScore
}

func nonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
