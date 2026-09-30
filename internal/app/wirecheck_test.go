package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireCheckMCPRecognizesProjectScope(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	path := filepath.Join(root, ".mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"devsys":{"command":"workloom"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.checkMCPConfig()
	if !got.OK || !strings.Contains(got.Detail, "config") {
		t.Fatalf("registered = %+v, want scope-config success", got)
	}
}
