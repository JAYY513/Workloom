package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"workloom/internal/sitestatic"
	"workloom/internal/storage"
	"workloom/internal/view"
)

// serveDefaults pin the M7.3 bind posture (方案 §17): loopback only.
const (
	serveDefaultHost = "127.0.0.1"
	serveDefaultPort = 8080
)

// serveHandler answers every request from a fresh view.Build: no snapshot
// cache, no files written, so the sources and baseline on each response are
// the state at that request. It carries no mutable state and is safe for
// concurrent use; view.Build reads under the shared lock it never creates.
type serveHandler struct {
	root  string
	limit int
}

func newServeMux(root string, limit int) http.Handler {
	h := serveHandler{root: root, limit: limit}
	return http.HandlerFunc(h.serveHTTP)
}

func (h serveHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Write methods are refused before path routing: no path accepts them.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "read-only: %s not allowed\n", r.Method)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/", "/index.html":
		h.servePage(w, r, "index.html")
	case "/tasks.html", "/workflows.html", "/runs.html", "/records.html", "/knowledge.html":
		h.servePage(w, r, strings.TrimPrefix(r.URL.Path, "/"))
	case "/assets/style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(w, sitestatic.Stylesheet())
	case "/data/model.json":
		m, ok := h.buildView(w, r.Context())
		if !ok {
			return
		}
		raw, err := sitestatic.ModelJSON(m)
		if err != nil {
			serveReadError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	case "/api/view":
		m, ok := h.buildView(w, r.Context())
		if !ok {
			return
		}
		generatedAt := time.Now().UTC().Truncate(time.Second)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			OK          bool   `json:"ok"`
			GeneratedAt string `json:"generated_at"`
			view.Model
		}{OK: true, GeneratedAt: generatedAt.Format(time.RFC3339), Model: *m})
	case "/healthz":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}\n")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found\n")
	}
}

func (h serveHandler) servePage(w http.ResponseWriter, r *http.Request, file string) {
	m, ok := h.buildView(w, r.Context())
	if !ok {
		return
	}
	generatedAt := time.Now().UTC().Truncate(time.Second)
	body, err := sitestatic.RenderPage(m, generatedAt, file)
	if err != nil {
		serveReadError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, body)
}

func (h serveHandler) buildView(w http.ResponseWriter, ctx context.Context) (*view.Model, bool) {
	m, err := view.Build(ctx, h.root, view.Options{Limit: h.limit})
	if err != nil {
		serveReadError(w, err)
		return nil, false
	}
	return m, true
}

func serveReadError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, "serve: read view: %v\n", err)
}

// isLoopbackHost reports whether host is a loopback name or address. Anything
// else — including the empty string and 0.0.0.0 — needs --allow-remote.
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[] \t"))
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

// runWorkspaceServe runs the local read-only service (M7.3, 方案 §17 两种形态
// 之二). It is a foreground process like `dispatch --watch`: Ctrl+C stops it.
// The only data entry is view.Build — the shared lock is never created,
// transactions are never recovered — and the server writes no files itself.
func runWorkspaceServe(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workspace serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", serveDefaultHost, "bind address (loopback by default)")
	port := fs.Int("port", serveDefaultPort, "bind port (0 = assigned by the OS)")
	allowRemote := fs.Bool("allow-remote", false, "permit a non-loopback --host (anyone on the network could read project state)")
	limit := fs.Int("limit", view.DefaultLimit, "max entries per list section (runs, records, pages)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`devsys workspace serve [--host 127.0.0.1] [--port N] [--allow-remote] [--limit N]`")
	}
	if *limit <= 0 {
		return errUsage("`--limit` must be positive")
	}
	if *port < 0 || *port > 65535 {
		return errUsage("`--port` must be 0-65535")
	}
	if opts.json || opts.quiet {
		return errUsage("`devsys workspace serve` does not take --json or --quiet")
	}
	if !isLoopbackHost(*host) && !*allowRemote {
		return errUsage("refusing non-loopback bind %q without --allow-remote (anyone on the network could read project state)", *host)
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	if _, err := view.Build(context.Background(), svc.Root, view.Options{Limit: *limit}); err != nil {
		if errors.Is(err, storage.ErrNotInitialized) {
			return errPrecondition("workspace serve: %v", err)
		}
		return errInternal("workspace serve: %v", err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		return errInternal("workspace serve: listen %s:%d: %v", *host, *port, err)
	}
	addr := ln.Addr().String()
	banner := fmt.Sprintf("serving http://%s/ (read-only) — Ctrl+C to stop", addr)
	if *allowRemote {
		banner += " [REMOTE ACCESS ENABLED]"
	}
	fmt.Fprintln(stdout, banner)
	srv := &http.Server{Handler: newServeMux(svc.Root, *limit)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errInternal("workspace serve: %v", err)
	}
	return nil
}
