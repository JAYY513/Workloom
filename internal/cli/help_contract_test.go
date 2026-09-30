package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpListsBlueprintImport(t *testing.T) {
	code, out, errOut := run(t, "--help")
	if code != CodeOK {
		t.Fatalf("help: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"import-blueprint",
		"workflow list|get|start|next|step-complete|pause|resume|cancel|init",
		"verify | complete",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("top-level help missing %q\n%s", want, out)
		}
	}

	code, out, errOut = run(t, "project")
	if code != CodeOK || !strings.Contains(out, "import-blueprint") {
		t.Fatalf("project family usage: code=%d out=%q stderr=%q", code, out, errOut)
	}
	code, out, errOut = run(t, "project", "--help")
	if code != CodeOK || !strings.Contains(out, "import-blueprint") {
		t.Fatalf("project --help: code=%d out=%q stderr=%q", code, out, errOut)
	}

	manual, err := os.ReadFile(filepath.Join("..", "..", "docs", "使用手册.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(manual)
	for _, want := range []string{
		"project import-blueprint",
		"project update --blueprint-artifact",
		"只绑定",
		"不导入蓝图字段",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("manual missing %q", want)
		}
	}
}
