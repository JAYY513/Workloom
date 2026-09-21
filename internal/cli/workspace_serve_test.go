package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/sitestatic"
	"github.com/JAYY513/Workloom/internal/view"
)

// serveFixture returns a project with one work item committed, so the served
// pages carry real provenance and a baseline.
func serveFixture(t *testing.T) string {
	t.Helper()
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "serve the read-only view", "local service")
	gitCommitAll(t, root, "work item")
	return root
}

// getOK fetches path and asserts a 200 with the wanted Content-Type prefix.
func getOK(t *testing.T, srv *httptest.Server, path, wantCT string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%q", path, resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, wantCT) {
		t.Fatalf("GET %s: Content-Type=%q want prefix %q", path, ct, wantCT)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("GET %s: Cache-Control=%q want no-store", path, cc)
	}
	return string(body)
}

// TestServeRefusesWriteMethods: every write method is a 405 on any path, with
// the Allow header and the read-only wording; GET on the same path is fine.
func TestServeRefusesWriteMethods(t *testing.T) {
	root := serveFixture(t)
	srv := httptest.NewServer(newServeMux(root, view.DefaultLimit))
	defer srv.Close()

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/", "/api/view", "/data/model.json"} {
			req, err := http.NewRequest(method, srv.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status=%d want 405", method, path, resp.StatusCode)
			}
			if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
				t.Errorf("%s %s: Allow=%q want %q", method, path, allow, "GET, HEAD")
			}
			want := fmt.Sprintf("read-only: %s not allowed\n", method)
			if string(body) != want {
				t.Errorf("%s %s: body=%q want %q", method, path, body, want)
			}
		}
	}
	if body := getOK(t, srv, "/", "text/html"); !strings.Contains(body, "基线提交") {
		t.Errorf("GET / lost the baseline footer")
	}
}

// TestServeRoutes: every read route answers with the right shape; unknown
// paths are 404; served pages name their sources and baseline.
func TestServeRoutes(t *testing.T) {
	root := serveFixture(t)
	srv := httptest.NewServer(newServeMux(root, view.DefaultLimit))
	defer srv.Close()

	for _, page := range append([]string{"/"}, pagePaths()...) {
		body := getOK(t, srv, page, "text/html")
		for _, want := range []string{"数据来源", "基线提交"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: missing %q", page, want)
			}
		}
	}
	if css := getOK(t, srv, "/assets/style.css", "text/css"); css != sitestatic.Stylesheet() {
		t.Errorf("stylesheet differs from the static asset")
	}
	raw := getOK(t, srv, "/data/model.json", "application/json")
	var model view.Model
	if err := json.Unmarshal([]byte(raw), &model); err != nil {
		t.Fatalf("model.json: %v", err)
	}
	if model.SchemaVersion != view.SchemaVersion || len(model.Sources) == 0 || !model.Baseline.Available {
		t.Errorf("model.json = schema %d sources %d baseline %+v",
			model.SchemaVersion, len(model.Sources), model.Baseline)
	}
	api := getOK(t, srv, "/api/view", "application/json")
	var envelope struct {
		OK          bool   `json:"ok"`
		GeneratedAt string `json:"generated_at"`
		view.Model
	}
	if err := json.Unmarshal([]byte(api), &envelope); err != nil {
		t.Fatalf("api/view: %v", err)
	}
	if !envelope.OK || envelope.GeneratedAt == "" || len(envelope.Sources) == 0 {
		t.Errorf("api/view envelope = %+v", envelope)
	}
	health := getOK(t, srv, "/healthz", "application/json")
	if strings.TrimSpace(health) != `{"ok":true}` {
		t.Errorf("healthz = %q", health)
	}
	resp, err := http.Get(srv.URL + "/nope.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /nope.html: status=%d want 404", resp.StatusCode)
	}
	head, err := http.Head(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD /: status=%d want 200", head.StatusCode)
	}
}

func pagePaths() []string {
	out := make([]string, 0, len(sitestatic.PageFiles))
	for _, f := range sitestatic.PageFiles {
		out = append(out, "/"+f)
	}
	return out
}

// TestServeUsageErrors pins the code-2 group: non-loopback without
// --allow-remote, bad port/limit, extra args, --json/--quiet.
func TestServeUsageErrors(t *testing.T) {
	gatedProject(t)
	cases := [][]string{
		{"workspace", "serve", "--host", "0.0.0.0"},
		{"workspace", "serve", "--host", ""},
		{"workspace", "serve", "--host", "example.com"},
		{"workspace", "serve", "--port", "-1"},
		{"workspace", "serve", "--port", "65536"},
		{"workspace", "serve", "--port", "abc"},
		{"workspace", "serve", "--limit", "0"},
		{"workspace", "serve", "extra"},
	}
	for _, args := range cases {
		if code, _, errOut := run(t, args...); code != CodeUsage {
			t.Errorf("%v: code = %d (stderr=%q), want %d", args, code, errOut, CodeUsage)
		}
	}
	if code, _, errOut := run(t, "--json", "workspace", "serve"); code != CodeUsage {
		t.Errorf("--json serve: code = %d (stderr=%q), want %d", code, errOut, CodeUsage)
	}
	if code, _, errOut := run(t, "--quiet", "workspace", "serve"); code != CodeUsage {
		t.Errorf("--quiet serve: code = %d (stderr=%q), want %d", code, errOut, CodeUsage)
	}
	if code, _, errOut := run(t, "workspace", "serve", "--host", "::1", "--port", "0", "extra"); code != CodeUsage {
		t.Errorf("extra arg: code = %d (stderr=%q), want %d", code, errOut, CodeUsage)
	}
}

// TestServePortInUse reports a bind conflict as a friendly precondition
// error naming the way out (#342), not a raw OS message.
func TestServePortInUse(t *testing.T) {
	gatedProject(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	code, _, errOut := run(t, "workspace", "serve", "--port", fmt.Sprintf("%d", port))
	if code != CodePrecondition {
		t.Fatalf("code = %d (stderr=%q), want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "already in use") || !strings.Contains(errOut, "--port") {
		t.Fatalf("stderr = %q, want the friendly port-in-use message", errOut)
	}
}

// TestServeWithoutProject is the precondition failure.
func TestServeWithoutProject(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(dir)

	code, _, errOut := run(t, "workspace", "serve", "--port", "0")
	if code != CodePrecondition {
		t.Fatalf("code = %d, want %d (stderr=%q)", code, CodePrecondition, errOut)
	}
	if !strings.Contains(errOut, "devsys init") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestServeWritesNothing: a burst of reads leaves every project file alone
func TestServeWritesNothing(t *testing.T) {
	root := serveFixture(t)
	if _, err := os.Stat(filepath.Join(root, ".devsys", "local", "lock")); err != nil {
		t.Fatalf("fixture needs a lock file: %v", err)
	}
	before := workspaceFingerprint(t, root)

	srv := httptest.NewServer(newServeMux(root, view.DefaultLimit))
	defer srv.Close()
	for _, path := range append(pagePaths(), "/assets/style.css", "/data/model.json", "/api/view", "/healthz") {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status=%d", path, resp.StatusCode)
		}
	}
	if after := workspaceFingerprint(t, root); !equalFingerprints(before, after) {
		t.Fatalf("serve changed project files:\n%s", workspaceDiff(before, after))
	}
}
