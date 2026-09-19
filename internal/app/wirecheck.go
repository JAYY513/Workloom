package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"workloom/internal/config"
	"workloom/internal/registry"
)

// WireCheckLine is one environment line of `devsys wire --check`.
type WireCheckLine struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// WireCheckView is the read-only environment report: toolchain, project,
// registry, AGENTS.md block and skill files. Inspecting never writes.
type WireCheckView struct {
	Lines []WireCheckLine `json:"lines"`
}

// WireCheck inspects the operator environment without writing anything:
// no lock, no recovery, no file creation.
func (s *Service) WireCheck() WireCheckView {
	lines := []WireCheckLine{
		checkTool("go", "Go toolchain"),
		checkTool("git", "Git"),
		s.checkDevsys(),
		s.checkSchema(),
		checkRegistry(),
		s.checkAgentsBlock(),
		s.checkSkill(),
		{"mcp", true, "printable via `devsys wire --print-mcp <codex|claude|opencode>`"},
	}
	return WireCheckView{Lines: lines}
}

func checkTool(bin, label string) WireCheckLine {
	if _, err := exec.LookPath(bin); err != nil {
		return WireCheckLine{Name: label, Detail: bin + " not on PATH"}
	}
	return WireCheckLine{Name: label, OK: true, Detail: bin + " on PATH"}
}

func (s *Service) checkDevsys() WireCheckLine {
	info, err := os.Stat(filepath.Join(s.Root, ".devsys"))
	switch {
	case err == nil && info.IsDir():
		return WireCheckLine{Name: ".devsys", OK: true, Detail: ".devsys/ present"}
	case err == nil:
		return WireCheckLine{Name: ".devsys", Detail: ".devsys is not a directory"}
	case os.IsNotExist(err):
		return WireCheckLine{Name: ".devsys", Detail: "missing: run `devsys init` first"}
	default:
		return WireCheckLine{Name: ".devsys", Detail: err.Error()}
	}
}

func (s *Service) checkSchema() WireCheckLine {
	if _, err := os.Stat(filepath.Join(s.Root, ".devsys")); err != nil {
		return WireCheckLine{Name: "schema", Detail: "skipped (no .devsys/)"}
	}
	_, problems := config.Load(s.Root)
	if len(problems) > 0 {
		return WireCheckLine{Name: "schema", Detail: problems[0].String()}
	}
	return WireCheckLine{Name: "schema", OK: true, Detail: "managed files parse"}
}

func checkRegistry() WireCheckLine {
	p, err := registry.Path()
	if err != nil {
		return WireCheckLine{Name: "registry", Detail: err.Error()}
	}
	if _, err := os.Stat(p); err == nil {
		return WireCheckLine{Name: "registry", OK: true, Detail: p}
	}
	return WireCheckLine{Name: "registry", Detail: "not created yet (written by init)"}
}

func (s *Service) checkAgentsBlock() WireCheckLine {
	data, err := os.ReadFile(filepath.Join(s.Root, "AGENTS.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return WireCheckLine{Name: "AGENTS.md", Detail: "missing: `devsys wire` creates it"}
		}
		return WireCheckLine{Name: "AGENTS.md", Detail: err.Error()}
	}
	_, changed, err := spliceWireBlock(string(data))
	if err != nil {
		return WireCheckLine{Name: "AGENTS.md", Detail: "block broken (wire refuses)"}
	}
	if !changed {
		return WireCheckLine{Name: "AGENTS.md", OK: true, Detail: "devsys block present"}
	}
	return WireCheckLine{Name: "AGENTS.md", Detail: "devsys block missing"}
}

func (s *Service) checkSkill() WireCheckLine {
	files, marked := s.SkillStatus()
	total := len(skillFiles())
	if files == 0 {
		return WireCheckLine{Name: "skill", Detail: "missing: `devsys wire --skill` writes " + skillDir + "/"}
	}
	if marked == total && files == total {
		return WireCheckLine{Name: "skill", OK: true, Detail: skillDir + "/ present"}
	}
	if files == total && marked < total {
		return WireCheckLine{Name: "skill", Detail: "present but hand-written (no devsys marker)"}
	}
	var missing []string
	for _, f := range skillFiles() {
		if _, err := os.Stat(filepath.Join(s.Root, filepath.FromSlash(f.rel))); err != nil {
			missing = append(missing, f.rel)
		}
	}
	return WireCheckLine{Name: "skill", Detail: "incomplete: missing " + strings.Join(missing, ", ")}
}
