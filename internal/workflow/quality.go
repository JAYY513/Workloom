package workflow

import (
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

// QualityResult is the deterministic quality score with the scored-zero
// components named as improvements.
type QualityResult struct {
	Score        int
	Improvements []string
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

	switch titleLen := weightedLen(strings.TrimSpace(in.Title)); {
	case titleLen >= 16:
		res.Score += 20
	case titleLen >= 8:
		res.Score += 10
	default:
		res.Improvements = append(res.Improvements, "标题过短：补充一个可辨识的任务标题（≥8 字符，中日韩文字按双倍计）")
	}

	desc := strings.TrimSpace(in.Description)
	switch descLen := weightedLen(desc); {
	case descLen >= 120:
		res.Score += 30
	case descLen >= 40:
		res.Score += 15
	default:
		res.Improvements = append(res.Improvements, "描述过短：补充背景、范围与做法（≥40 字符，中日韩文字按双倍计）")
	}

	if nonEmptyLines(desc) >= 2 {
		res.Score += 15
	} else {
		res.Improvements = append(res.Improvements, "描述缺少结构：用分点或步骤组织描述")
	}

	if len(in.AcceptanceCriteria) > 0 {
		res.Score += 15
	} else {
		res.Improvements = append(res.Improvements, "缺少验收标准：用 `devsys workitem update --id <id> --acceptance a,b --actor <you> --reason <why>` 补充验收条件")
	}

	text := in.Title + "\n" + in.Description + "\n" + strings.Join(in.AcceptanceCriteria, "\n")
	if referencePattern.MatchString(text) {
		res.Score += 10
	} else {
		res.Improvements = append(res.Improvements, "缺少具体引用：补充文件路径、命令或标识符")
	}
	// Acceptance language: structured criteria satisfy this component (they
	// already say how completion is verified); otherwise the keyword check
	// runs. Demanding the keyword on top of criteria double-counted the same
	// evidence and read as if --acceptance had been ignored (#342).
	if len(in.AcceptanceCriteria) > 0 {
		res.Score += 10
	} else if strings.Contains(text, "验收") || strings.Contains(strings.ToLower(text), "acceptance") {
		res.Score += 10
	} else {
		res.Improvements = append(res.Improvements, "缺少验收语言：写明如何验收（「验收」或 acceptance），或直接 --acceptance 给出验收条件")
	}
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
