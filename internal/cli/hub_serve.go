package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/JAYY513/Workloom/internal/hubstatic"
	"github.com/JAYY513/Workloom/internal/project"
	"github.com/JAYY513/Workloom/internal/registry"
	"github.com/JAYY513/Workloom/internal/storage"
	"github.com/JAYY513/Workloom/internal/view"
)

const (
	hubDefaultHost = "127.0.0.1"
	hubDefaultPort = 18080
)

type hubProject struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	Exists     bool   `json:"exists"`
	Trust      string `json:"trust,omitempty"`
	Tasks      *int   `json:"tasks,omitempty"`
	Active     *int   `json:"active_tasks,omitempty"`
	Risks      *int   `json:"risks,omitempty"`
	Runs       *int   `json:"runs,omitempty"`
	Knowledge  string `json:"knowledge,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type hubServer struct {
	registryPath string
	limit        int
}

// loadHubRegistry reads the user-level registry for `hub serve`. A missing or
// blank file is an empty registry: Hub must start when no project has been
// registered yet and when the current directory is not a project
// (docs/hub-v1.md §4). A genuinely corrupt file still fails.
func loadHubRegistry(path string) (*registry.Registry, error) {
	reg, err := registry.Load(path)
	if err == nil {
		return reg, nil
	}
	if data, readErr := os.ReadFile(path); readErr == nil && strings.TrimSpace(string(data)) == "" {
		return &registry.Registry{}, nil
	}
	return nil, err
}

func (h hubServer) load() (*registry.Registry, error) { return loadHubRegistry(h.registryPath) }

// allow lists the methods a path accepts: GET and HEAD everywhere, POST for
// the local project registration endpoint.
func (h hubServer) allow(path string) string {
	if path == "/api/projects" {
		return "GET, HEAD, POST"
	}
	return "GET, HEAD"
}

func (h hubServer) project(id string) (registry.Entry, bool, error) {
	r, err := h.load()
	if err != nil {
		return registry.Entry{}, false, err
	}
	for _, p := range r.Projects {
		if p.ID == id {
			return p, true, nil
		}
	}
	return registry.Entry{}, false, nil
}

func (h hubServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.URL.Path == "/api/projects" && r.Method == http.MethodPost:
		h.addProject(w, r)
	case r.Method != http.MethodGet && r.Method != http.MethodHead:
		w.Header().Set("Allow", h.allow(r.URL.Path))
		http.Error(w, "read-only", http.StatusMethodNotAllowed)
	case r.URL.Path == "/" || r.URL.Path == "/index.html":
		h.asset(w, "index.html", "text/html; charset=utf-8")
	case r.URL.Path == "/assets/app.js":
		h.asset(w, "app.js", "text/javascript; charset=utf-8")
	case r.URL.Path == "/app.js":
		h.asset(w, "app.js", "text/javascript; charset=utf-8")
	case r.URL.Path == "/assets/style.css":
		h.asset(w, "style.css", "text/css; charset=utf-8")
	case r.URL.Path == "/api/projects":
		h.projects(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/projects/"):
		h.projectView(w, r)
	case r.URL.Path == "/healthz":
		h.json(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (h hubServer) asset(w http.ResponseWriter, name, contentType string) {
	data, err := hubstatic.Files.ReadFile(name)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (h hubServer) projects(w http.ResponseWriter, req *http.Request) {
	r, err := h.load()
	if err != nil {
		http.Error(w, "read registry: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]hubProject, 0, len(r.Projects))
	for _, p := range r.Projects {
		out = append(out, h.projectItem(req.Context(), p))
	}
	h.jsonConditional(w, req, out)
}

// projectItem maps a registry entry to its Hub summary. Existence is the
// <path>/.devsys directory: a missing project keeps its registry facts and
// reports why the business view is unavailable instead of exposing zeros.
func (h hubServer) projectItem(ctx context.Context, p registry.Entry) hubProject {
	info, statErr := os.Stat(filepath.Join(p.Path, ".devsys"))
	item := hubProject{
		ID:         p.ID,
		Path:       p.Path,
		LastSeenAt: p.LastSeenAt.UTC().Format("2006-01-02T15:04:05Z"),
		Exists:     statErr == nil && info.IsDir(),
	}
	if !item.Exists {
		return item
	}
	m, buildErr := view.Build(ctx, p.Path, view.Options{Limit: h.limit})
	if buildErr != nil {
		item.Reason = buildErr.Error()
		return item
	}
	if m.Trust.State == view.TrustPending {
		item.Trust, item.Reason = m.Trust.State, m.Trust.Note
		return item
	}
	item.Trust, item.Knowledge = m.Trust.State, m.Knowledge.Status
	tasks, active, risks, runs := len(m.Progress.Items), 0, len(m.Project.Risks)+len(m.Project.Blockers), len(m.Runs.Entries)
	for _, task := range m.Progress.Items {
		switch strings.ToLower(task.Status) {
		case "进行中", "in_progress", "review", "评审", "verify", "验证":
			active++
		}
	}
	item.Tasks, item.Active, item.Risks, item.Runs = &tasks, &active, &risks, &runs
	if m.Trust.State != view.TrustOK {
		item.Reason = m.Trust.Note
	}
	return item
}

// hubAddRequest is the POST /api/projects body. id is optional: an omitted id
// is derived from the directory name with the same Slug rule `workloom init`
// uses, so re-adding an initialized project is stable.
type hubAddRequest struct {
	Path string  `json:"path"`
	ID   *string `json:"id"`
}

// maxHubAddBody bounds the registration request; a directory path is the only
// payload the endpoint accepts.
const maxHubAddBody = 64 << 10

// addProject registers one local project (docs/hub-v1.md §5). It normalizes
// the path, requires an existing directory that holds .devsys/, and upserts
// the user-level registry atomically. Duplicate ids or paths are refused so a
// mistaken entry never silently rewrites another project's registration.
func (h hubServer) addProject(w http.ResponseWriter, r *http.Request) {
	if !loopbackClient(r) {
		h.apiError(w, http.StatusForbidden, "project registration is local-only")
		return
	}
	var req hubAddRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxHubAddBody))
	if err := dec.Decode(&req); err != nil {
		h.apiError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if dec.More() {
		h.apiError(w, http.StatusBadRequest, "invalid JSON body: unexpected trailing data")
		return
	}
	raw := strings.TrimSpace(req.Path)
	if raw == "" {
		h.apiError(w, http.StatusBadRequest, "path is required")
		return
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		h.apiError(w, http.StatusBadRequest, "invalid path: "+err.Error())
		return
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		h.apiError(w, http.StatusBadRequest, "path does not exist: "+abs)
		return
	}
	if !info.IsDir() {
		h.apiError(w, http.StatusBadRequest, "path is not a directory: "+abs)
		return
	}
	ds, err := os.Stat(filepath.Join(abs, ".devsys"))
	if err != nil || !ds.IsDir() {
		h.apiError(w, http.StatusBadRequest, "path has no .devsys/ directory: "+abs+" — run `workloom init` there first")
		return
	}
	id := project.Slug(filepath.Base(abs))
	if req.ID != nil {
		id = strings.TrimSpace(*req.ID)
		if msg, ok := validHubID(id); !ok {
			h.apiError(w, http.StatusBadRequest, msg)
			return
		}
	}
	reg, err := h.load()
	if err != nil {
		h.apiError(w, http.StatusInternalServerError, "read registry: "+err.Error())
		return
	}
	for _, p := range reg.Projects {
		if hubPathEqual(p.Path, abs) {
			if !strings.EqualFold(p.ID, id) {
				h.apiError(w, http.StatusConflict, fmt.Sprintf("path %s is already registered as id %q", abs, p.ID))
				return
			}
			continue
		}
		if strings.EqualFold(p.ID, id) {
			h.apiError(w, http.StatusConflict, fmt.Sprintf("id %q is already registered to %s", id, p.Path))
			return
		}
	}
	entry := registry.Entry{ID: id, Path: abs, LastSeenAt: time.Now().UTC()}
	reg.Upsert(entry)
	if err := reg.Save(h.registryPath); err != nil {
		h.apiError(w, http.StatusInternalServerError, "save registry: "+err.Error())
		return
	}
	h.json(w, h.projectItem(r.Context(), entry))
}

// validHubID rejects identifiers that cannot name a Hub project: empty,
// oversized, path-like or carrying whitespace/control characters.
func validHubID(id string) (string, bool) {
	switch {
	case id == "":
		return "id must not be empty", false
	case len(id) > 128:
		return "id must be 128 characters or fewer", false
	case id == "." || id == "..":
		return "invalid id: " + id, false
	}
	for _, r := range id {
		switch {
		case r == '/' || r == '\\':
			return "id must not contain path separators", false
		case r < 0x20 || r == 0x7f:
			return "id must not contain control characters", false
		case unicode.IsSpace(r):
			return "id must not contain whitespace", false
		}
	}
	return "", true
}

// hubPathEqual compares registry paths the way registry.Save keys them:
// cleaned slash form, case-insensitively on Windows.
func hubPathEqual(a, b string) bool {
	ka, kb := hubPathKey(a), hubPathKey(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ka, kb)
	}
	return ka == kb
}

func hubPathKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// loopbackClient reports whether the request came from this machine.
// Registration writes the user-level registry, so it stays local even when the
// server was started with --allow-remote.
func loopbackClient(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// apiError writes the JSON error shape the Hub app surfaces verbatim.
func (h hubServer) apiError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h hubServer) projectView(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	if !strings.HasSuffix(path, "/view") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(path, "/view")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	entry, ok, err := h.project(id)
	if err != nil {
		http.Error(w, "read registry: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	m, err := view.Build(r.Context(), entry.Path, view.Options{Limit: h.limit})
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotInitialized) {
			code = http.StatusUnprocessableEntity
		}
		http.Error(w, "read view: "+err.Error(), code)
		return
	}
	h.jsonConditional(w, r, m)
}

func (h hubServer) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func (h hubServer) jsonConditional(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode view", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(append(body, '\n'))
}

func runHub(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom hub` needs a subcommand: serve") {
		return nil
	}
	if rest[0] != "serve" {
		return errUsage("unknown `workloom hub` subcommand %q", rest[0])
	}
	if opts.json || opts.quiet {
		return errUsage("`workloom hub serve` does not take --json or --quiet")
	}
	fs := flag.NewFlagSet("hub serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", hubDefaultHost, "bind address (loopback by default)")
	port := fs.Int("port", hubDefaultPort, "bind port (0 = assigned by the OS)")
	allowRemote := fs.Bool("allow-remote", false, "permit a non-loopback --host")
	limit := fs.Int("limit", view.DefaultLimit, "max entries per list section")
	if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
		return errUsage("`workloom hub serve [--host 127.0.0.1] [--port N] [--allow-remote] [--limit N]`")
	}
	if *limit <= 0 || *port < 0 || *port > 65535 {
		return errUsage("invalid hub serve limit or port")
	}
	if !isLoopbackHost(*host) && !*allowRemote {
		return errUsage("refusing non-loopback bind %q without --allow-remote", *host)
	}
	path, err := registry.Path()
	if err != nil {
		return errInternal("hub serve: %v", err)
	}
	if _, err := loadHubRegistry(path); err != nil {
		return errInternal("hub serve: %v", err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		if addrInUse(err) {
			return errPrecondition("hub serve: port %d is already in use", *port)
		}
		return errInternal("hub serve: listen: %v", err)
	}
	defer ln.Close()
	fmt.Fprintf(stdout, "serving http://%s/ (local Hub) — Ctrl+C to stop\n", ln.Addr())
	srv := &http.Server{Handler: http.HandlerFunc(hubServer{registryPath: path, limit: *limit}.serveHTTP)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errInternal("hub serve: %v", err)
	}
	return nil
}
