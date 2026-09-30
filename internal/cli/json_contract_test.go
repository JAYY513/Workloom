package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONEnvelopeContract(t *testing.T) {
	repo, _ := gatedProject(t)
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "design.md"), []byte("# design\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--json", "workitem", "create", "--title", "envelope task", "--actor", "test", "--reason", "contract")
	if code != CodeOK {
		t.Fatalf("create: code=%d stderr=%q", code, errOut)
	}
	var created struct {
		Version string `json:"version"`
		Item    struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Version) != 64 {
		t.Fatalf("workitem create version = %q, want a top-level 64-hex hash", created.Version)
	}

	code, out, errOut = run(t, "--json", "workitem", "list")
	if code != CodeOK {
		t.Fatalf("workitem list: code=%d stderr=%q", code, errOut)
	}
	var listed struct {
		Count int `json:"count"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Count != len(listed.Items) || listed.Count < 1 {
		t.Fatalf("workitem list count = %d items = %d", listed.Count, len(listed.Items))
	}

	code, out, errOut = run(t, "--json", "project", "get")
	if code != CodeOK {
		t.Fatalf("project get: code=%d stderr=%q", code, errOut)
	}
	var project struct {
		Version string `json:"version"`
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(out), &project); err != nil || len(project.Version) != 64 || project.Project.ID == "" {
		t.Fatalf("project get version/id: err=%v out=%s", err, out)
	}

	code, out, errOut = run(t, "--json", "decision", "list")
	if code != CodeOK {
		t.Fatalf("decision list: code=%d stderr=%q", code, errOut)
	}
	var decisions struct {
		Count     int              `json:"count"`
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(out), &decisions); err != nil || decisions.Count != len(decisions.Decisions) {
		t.Fatalf("decision list count: err=%v out=%s", err, out)
	}

	if code, _, errOut = run(t, "artifact", "register", "--name", "design.md", "--path", "docs/design.md",
		"--actor", "test", "--reason", "contract"); code != CodeOK {
		t.Fatalf("register: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut = run(t, "--json", "artifact", "list")
	if code != CodeOK {
		t.Fatalf("artifact list: code=%d stderr=%q", code, errOut)
	}
	var artifacts struct {
		Count       int `json:"count"`
		LatestCount int `json:"latest_count"`
		Artifacts   []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
		Latest []struct {
			ID string `json:"id"`
		} `json:"latest"`
	}
	if err := json.Unmarshal([]byte(out), &artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts.Count != 1 || artifacts.LatestCount != 1 || len(artifacts.Artifacts) != 1 || artifacts.Artifacts[0].ID != artifacts.Latest[0].ID {
		t.Fatalf("single artifact envelope = %+v", artifacts)
	}
	hash := cliRecordVersion(t, "artifact", artifacts.Artifacts[0].ID)
	code, out, errOut = run(t, "--json", "artifact", "update", "--id", artifacts.Artifacts[0].ID,
		"--status", "approved", "--actor", "test", "--reason", "new version", "--expect", hash)
	if code != CodeOK {
		t.Fatalf("update: code=%d stderr=%q", code, errOut)
	}
	var updated struct {
		Version  string `json:"version"`
		Artifact struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"artifact"`
	}
	if err := json.Unmarshal([]byte(out), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Version) != 64 || updated.Artifact.Version != 2 || updated.Artifact.ID == artifacts.Artifacts[0].ID {
		t.Fatalf("update envelope = %+v", updated)
	}

	code, out, errOut = run(t, "--json", "artifact", "list")
	if code != CodeOK {
		t.Fatalf("list after update: code=%d stderr=%q", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts.Count != 2 || len(artifacts.Artifacts) != 2 || artifacts.LatestCount != 1 || len(artifacts.Latest) != 1 || artifacts.Latest[0].ID != updated.Artifact.ID {
		t.Fatalf("chain envelope = %+v head=%s", artifacts, updated.Artifact.ID)
	}
	code, human, errOut := run(t, "artifact", "list")
	if code != CodeOK {
		t.Fatalf("human list: code=%d stderr=%q", code, errOut)
	}
	if strings.Contains(human, artifacts.Artifacts[0].ID) || !strings.Contains(human, updated.Artifact.ID) || !strings.Contains(human, "--history") {
		t.Fatalf("human list = %q, want only the head and a history hint", human)
	}
	code, human, errOut = run(t, "artifact", "list", "--history")
	if code != CodeOK || !strings.Contains(human, artifacts.Artifacts[0].ID) || !strings.Contains(human, updated.Artifact.ID) {
		t.Fatalf("history view: code=%d out=%q stderr=%q", code, human, errOut)
	}
}
