package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPSnippetClaudeJSONAcceptsWindowsPaths(t *testing.T) {
	t.Setenv("DEVSYS_CONFIG_DIR", `C:\Users\tester\.devsys`)
	bin := `C:\Users\tester\AppData\Local\devsys\devsys.exe`
	root := `C:\Source\CodeSource\ai\demo`
	snippet, err := MCPSnippet("claude", MCPCommand{Command: bin}, root)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(snippet, "{")
	end := strings.LastIndex(snippet, "}")
	if start < 0 || end <= start {
		t.Fatalf("snippet has no JSON object: %s", snippet)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(snippet[start:end+1]), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, snippet)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	devsys, _ := servers["devsys"].(map[string]any)
	if devsys["command"] != filepathToSlashForTest(bin) && devsys["command"] != bin {
		// filepath.ToSlash on Windows turns backslashes into slashes.
		if got, _ := devsys["command"].(string); !strings.Contains(strings.ReplaceAll(got, "/", "\\"), "devsys.exe") {
			t.Fatalf("command = %v", devsys["command"])
		}
	}
	if _, ok := devsys["cwd"].(string); !ok {
		t.Fatalf("cwd missing: %v", devsys)
	}
	env, _ := devsys["env"].(map[string]any)
	if env["DEVSYS_CONFIG_DIR"] == nil {
		t.Fatalf("env missing DEVSYS_CONFIG_DIR: %v", devsys)
	}
}

func filepathToSlashForTest(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

func TestMCPSnippetUnknownHarness(t *testing.T) {
	if _, err := MCPSnippet("nope", MCPCommand{Command: "devsys"}, "."); err == nil {
		t.Fatal("expected usage error")
	}
}
