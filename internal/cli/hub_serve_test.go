package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/registry"
	"github.com/JAYY513/Workloom/internal/view"
)

func TestHubProjectsAndViews(t *testing.T) {
	requireGit(t)
	valid := filepath.Join(t.TempDir(), "valid")
	missing := filepath.Join(t.TempDir(), "missing")
	notInitialized := filepath.Join(t.TempDir(), "plain")
	for _, dir := range []string{valid, notInitialized} {
		initRepo(t, dir)
	}
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(valid)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%q", code, errOut)
	}
	path, err := registry.Path()
	if err != nil {
		t.Fatal(err)
	}
	r := &registry.Registry{Projects: []registry.Entry{{ID: "valid", Path: valid}, {ID: "missing", Path: missing}, {ID: "plain", Path: notInitialized}}}
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	h := hubServer{registryPath: path, limit: view.DefaultLimit}

	projects := httptest.NewRecorder()
	h.serveHTTP(projects, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if projects.Code != http.StatusOK {
		t.Fatalf("projects status=%d body=%s", projects.Code, projects.Body)
	}
	var got []hubProject
	if err := json.Unmarshal(projects.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]hubProject, len(got))
	for _, p := range got {
		byID[p.ID] = p
	}
	if len(got) != 3 || !byID["valid"].Exists {
		t.Fatalf("projects=%+v", got)
	}
	if byID["missing"].Tasks != nil || byID["plain"].Tasks != nil {
		t.Fatalf("unavailable projects exposed stats: %+v", got)
	}
	if byID["valid"].Trust == view.TrustPending {
		t.Fatalf("valid project unexpectedly pending: %+v", byID["valid"])
	}
	etag := projects.Header().Get("ETag")
	if etag == "" {
		t.Fatal("projects missing ETag")
	}
	conditional := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("If-None-Match", etag)
	h.serveHTTP(conditional, req)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status=%d", conditional.Code)
	}

	viewResponse := httptest.NewRecorder()
	h.serveHTTP(viewResponse, httptest.NewRequest(http.MethodGet, "/api/projects/valid/view", nil))
	if viewResponse.Code != http.StatusOK {
		t.Fatalf("valid view status=%d body=%s", viewResponse.Code, viewResponse.Body)
	}
	if viewResponse.Header().Get("ETag") == "" {
		t.Fatal("view missing ETag")
	}
	for id, want := range map[string]int{"missing": http.StatusUnprocessableEntity, "plain": http.StatusUnprocessableEntity, "unknown": http.StatusNotFound} {
		response := httptest.NewRecorder()
		h.serveHTTP(response, httptest.NewRequest(http.MethodGet, "/api/projects/"+id+"/view", nil))
		if response.Code != want {
			t.Errorf("%s status=%d want=%d body=%s", id, response.Code, want, response.Body)
		}
	}
	method := httptest.NewRecorder()
	h.serveHTTP(method, httptest.NewRequest(http.MethodDelete, "/api/projects", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status=%d", method.Code)
	}
	if got := method.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Fatalf("Allow=%q", got)
	}
	other := httptest.NewRecorder()
	h.serveHTTP(other, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if other.Code != http.StatusMethodNotAllowed || other.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST /healthz status=%d allow=%q", other.Code, other.Header().Get("Allow"))
	}
}

// assertEmptyProjects checks GET /api/projects answers an empty JSON list.
func assertEmptyProjects(t *testing.T, h hubServer) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.serveHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/projects status=%d body=%s", rec.Code, rec.Body)
	}
	var got []hubProject
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("projects=%+v want empty", got)
	}
}

// TestHubStartsWithoutRegistry proves `hub serve`'s registry load treats an
// absent or blank file as an empty registry and still rejects corruption.
func TestHubStartsWithoutRegistry(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.yaml")
	reg, err := loadHubRegistry(missing)
	if err != nil || len(reg.Projects) != 0 {
		t.Fatalf("missing registry: reg=%+v err=%v", reg, err)
	}
	blank := filepath.Join(dir, "registry.yaml")
	if err := os.WriteFile(blank, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reg, err = loadHubRegistry(blank); err != nil || len(reg.Projects) != 0 {
		t.Fatalf("blank registry: reg=%+v err=%v", reg, err)
	}
	corrupt := filepath.Join(dir, "corrupt.yaml")
	if err := os.WriteFile(corrupt, []byte("other: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHubRegistry(corrupt); err == nil {
		t.Fatal("corrupt registry must still error")
	}
	// The server itself starts with no registry and serves an empty list.
	t.Setenv("DEVSYS_CONFIG_DIR", dir)
	h := hubServer{registryPath: filepath.Join(dir, "api", "registry.yaml"), limit: view.DefaultLimit}
	assertEmptyProjects(t, h)
}

// hubAddFixture returns a Hub server backed by a fresh, absent registry and a
// directory containing .devsys/ ready to register.
func hubAddFixture(t *testing.T) (hubServer, string, string) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("DEVSYS_CONFIG_DIR", configDir)
	regPath, err := registry.Path()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "workloom")
	if err := os.MkdirAll(filepath.Join(root, ".devsys"), 0o755); err != nil {
		t.Fatal(err)
	}
	return hubServer{registryPath: regPath, limit: view.DefaultLimit}, regPath, root
}

// hubPost sends a local POST /api/projects request.
func hubPost(t *testing.T, h hubServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	h.serveHTTP(rec, req)
	return rec
}

// hubAddBody renders a POST body from a path and optional id.
func hubAddBody(path string, id *string) string {
	req := map[string]any{"path": path}
	if id != nil {
		req["id"] = *id
	}
	b, _ := json.Marshal(req)
	return string(b)
}

// hubErrorBody asserts the response body is the JSON {"error": ...} shape and
// returns the message.
func hubErrorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body not JSON: %s", rec.Body)
	}
	if body.Error == "" {
		t.Fatalf("error body has no message: %s", rec.Body)
	}
	return body.Error
}

func TestHubAddProjectRegisters(t *testing.T) {
	h, regPath, root := hubAddFixture(t)
	rec := hubPost(t, h, hubAddBody(root, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body)
	}
	var got hubProject
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "workloom" || got.Path != abs || !got.Exists || got.LastSeenAt == "" {
		t.Fatalf("entry=%+v want id=workloom path=%s exists with last_seen_at", got, abs)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 1 || reg.Projects[0].ID != "workloom" || reg.Projects[0].Path != abs {
		t.Fatalf("registry=%+v", reg.Projects)
	}
	list := httptest.NewRecorder()
	h.serveHTTP(list, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var projects []hubProject
	if err := json.Unmarshal(list.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != "workloom" || !projects[0].Exists {
		t.Fatalf("projects=%+v", projects)
	}
}

func TestHubAddProjectRejectsInvalid(t *testing.T) {
	h, regPath, root := hubAddFixture(t)
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, body string }{
		{"malformed", `{"path":`},
		{"trailing json", hubAddBody(root, nil) + " {}"},
		{"missing path", `{}`},
		{"empty path", `{"path":"  "}`},
		{"missing dir", hubAddBody(filepath.Join(t.TempDir(), "missing"), nil)},
		{"not a dir", hubAddBody(file, nil)},
		{"no devsys", hubAddBody(plain, nil)},
		{"empty id", hubAddBody(root, new("  "))},
		{"separator id", hubAddBody(root, new("a/b"))},
		{"space id", hubAddBody(root, new("a b"))},
	}
	for _, tc := range cases {
		rec := hubPost(t, h, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", tc.name, rec.Code, rec.Body)
			continue
		}
		hubErrorBody(t, rec)
	}
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Fatalf("registry written on rejected request (err=%v)", err)
	}
}

func TestHubAddProjectConflicts(t *testing.T) {
	h, regPath, root := hubAddFixture(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(filepath.Join(other, ".devsys"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rec := hubPost(t, h, hubAddBody(root, nil)); rec.Code != http.StatusOK {
		t.Fatalf("first add status=%d body=%s", rec.Code, rec.Body)
	}
	// Re-adding the same path is an idempotent refresh, not a conflict.
	if rec := hubPost(t, h, hubAddBody(root, nil)); rec.Code != http.StatusOK {
		t.Fatalf("repeat add status=%d body=%s", rec.Code, rec.Body)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 1 {
		t.Fatalf("duplicate path wrote %d entries: %+v", len(reg.Projects), reg.Projects)
	}
	// Same id, different path.
	if rec := hubPost(t, h, hubAddBody(other, new("workloom"))); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate id status=%d body=%s", rec.Code, rec.Body)
	} else {
		hubErrorBody(t, rec)
	}
	// Same path, different id.
	if rec := hubPost(t, h, hubAddBody(root, new("renamed"))); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate path status=%d body=%s", rec.Code, rec.Body)
	} else {
		hubErrorBody(t, rec)
	}
	reg, err = registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 1 || reg.Projects[0].ID != "workloom" {
		t.Fatalf("conflicts mutated registry: %+v", reg.Projects)
	}
}

func TestHubAddProjectLocalOnly(t *testing.T) {
	h, regPath, root := hubAddFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(hubAddBody(root, nil)))
	req.RemoteAddr = "203.0.113.5:12345"
	rec := httptest.NewRecorder()
	h.serveHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote add status=%d body=%s", rec.Code, rec.Body)
	}
	hubErrorBody(t, rec)
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Fatalf("remote add wrote registry (err=%v)", err)
	}
}
