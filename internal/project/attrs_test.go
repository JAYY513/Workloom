package project

// #345 N2：init 在用户项目根生成/合并 .gitattributes（.devsys/ 与
// .agents/ 走 LF），消除 Windows LF→CRLF 噪音；幂等，手写规则不动。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func attrsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	initRepo(t, root)
	return root
}

func TestInitWritesRootGitattributes(t *testing.T) {
	root := attrsRepo(t)
	res, err := Init(root, Options{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	found := false
	for _, p := range res.Created {
		if p == ".gitattributes" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created = %v, want .gitattributes listed", res.Created)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".devsys/** text eol=lf", ".agents/** text eol=lf"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf(".gitattributes = %q, want %q", data, want)
		}
	}
}

func TestInitMergesExistingGitattributes(t *testing.T) {
	root := attrsRepo(t)
	existing := "*.pdf binary\n.devsys/** text eol=lf\n"
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Init(root, Options{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, p := range res.Created {
		if p == ".gitattributes" {
			t.Fatalf("created = %v; an existing .gitattributes must not be listed as created", res.Created)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "*.pdf binary") {
		t.Fatalf("hand-written rule lost: %q", text)
	}
	// The duplicate .devsys line must not be appended; only .agents is missing.
	if strings.Count(text, ".devsys/**") != 1 {
		t.Fatalf("duplicate .devsys rule: %q", text)
	}
	if !strings.Contains(text, ".agents/** text eol=lf") {
		t.Fatalf("missing .agents rule: %q", text)
	}
}

func TestInitGitattributesIdempotent(t *testing.T) {
	root := attrsRepo(t)
	if _, err := Init(root, Options{Now: time.Now().UTC()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Init(root, Options{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if len(res.Created) != 0 {
		t.Fatalf("second run created = %v, want nothing", res.Created)
	}
	second, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("bytes changed on re-init:\nfirst:  %q\nsecond: %q", first, second)
	}
}
