package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/next"
	"workloom/internal/project"
	"workloom/internal/record"
	"workloom/internal/registry"
	"workloom/internal/storage"
)

// ProjectView is the project metadata plus the version hash of its file.
type ProjectView struct {
	Project *domain.Project `json:"project"`
	Version string          `json:"version"`
}

// ProjectGet reads the project metadata (validated, read-only).
func (s *Service) ProjectGet(ctx context.Context) (ProjectView, error) {
	md, err := s.project()
	if err != nil {
		return ProjectView{}, err
	}
	raw, err := s.readFile(ctx, "project.yaml")
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectView{Project: md.Project, Version: versionHash(raw)}, nil
}

// UpdateProjectRequest patches project metadata. Nil fields stay untouched;
// Expect is the version hash from ProjectGet.
type UpdateProjectRequest struct {
	Name         *string
	Description  *string
	Status       *string
	CurrentPhase *string
	Expect       string
}

// ProjectUpdate applies a metadata patch under the version guard.
func (s *Service) ProjectUpdate(ctx context.Context, req UpdateProjectRequest) (ProjectView, error) {
	if req.Name == nil && req.Description == nil && req.Status == nil && req.CurrentPhase == nil {
		return ProjectView{}, Usagef("project update requires at least one field to change")
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return ProjectView{}, Usagef("name must not be empty")
	}
	if _, err := s.project(); err != nil {
		return ProjectView{}, err
	}
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return ProjectView{}, s.storeError(err)
	}
	now := s.now()
	if err := st.Write(ctx, func(tx *storage.Tx) error {
		data, ok, err := tx.ReadForExpect("project.yaml")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("project.yaml is missing")
		}
		if err := checkExpect(req.Expect, data, "project"); err != nil {
			return err
		}
		var proj domain.Project
		if err := storage.DecodeYAML(data, &proj); err != nil {
			return err
		}
		if req.Name != nil {
			proj.Name = *req.Name
		}
		if req.Description != nil {
			proj.Description = *req.Description
		}
		if req.Status != nil {
			proj.Status = *req.Status
		}
		if req.CurrentPhase != nil {
			proj.CurrentPhase = *req.CurrentPhase
		}
		proj.UpdatedAt = now
		return tx.PutYAML("project.yaml", &proj, storage.ExpectHash(storage.HashBytes(data)))
	}); err != nil {
		return ProjectView{}, s.classifyWrite(err)
	}
	return s.ProjectGet(ctx)
}

// UpdateStateRequest patches state/current.yaml. Nil/empty collections stay
// untouched; Expect is the version hash from ProjectStateGet.
type UpdateStateRequest struct {
	Summary   *string
	Risks     []string
	Blockers  []string
	NextFocus []string
	Expect    string
}

// StateView is the current-state document plus its version hash.
type StateView struct {
	State   *domain.CurrentStateFile `json:"state"`
	Version string                   `json:"version"`
}

// ProjectStateGet reads state/current.yaml.
func (s *Service) ProjectStateGet(ctx context.Context) (StateView, error) {
	if _, err := s.project(); err != nil {
		return StateView{}, err
	}
	raw, err := s.readFile(ctx, "state/current.yaml")
	if err != nil {
		return StateView{}, err
	}
	var doc domain.CurrentStateFile
	if err := storage.DecodeYAML(raw, &doc); err != nil {
		return StateView{}, Invalidf(KindInvalid, nil, "state/current.yaml: %v", err)
	}
	return StateView{State: &doc, Version: versionHash(raw)}, nil
}

// ProjectStateUpdate applies a current-state patch under the version guard.
func (s *Service) ProjectStateUpdate(ctx context.Context, req UpdateStateRequest) (StateView, error) {
	if req.Summary == nil && req.Risks == nil && req.Blockers == nil && req.NextFocus == nil {
		return StateView{}, Usagef("state update requires at least one field to change")
	}
	if _, err := s.project(); err != nil {
		return StateView{}, err
	}
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return StateView{}, s.storeError(err)
	}
	if err := st.Write(ctx, func(tx *storage.Tx) error {
		data, ok, err := tx.ReadForExpect("state/current.yaml")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("state/current.yaml is missing")
		}
		if err := checkExpect(req.Expect, data, "state"); err != nil {
			return err
		}
		var doc domain.CurrentStateFile
		if err := storage.DecodeYAML(data, &doc); err != nil {
			return err
		}
		if req.Summary != nil {
			doc.Summary = *req.Summary
		}
		if req.Risks != nil {
			doc.Risks = req.Risks
		}
		if req.Blockers != nil {
			doc.Blockers = req.Blockers
		}
		if req.NextFocus != nil {
			doc.NextFocus = req.NextFocus
		}
		return tx.PutYAML("state/current.yaml", &doc, storage.ExpectHash(storage.HashBytes(data)))
	}); err != nil {
		return StateView{}, s.classifyWrite(err)
	}
	return s.ProjectStateGet(ctx)
}

// ProjectBlueprint returns the artifact the project declares as its blueprint.
// A project that declares none is a normal state, not a failure: the caller
// gets (nil, nil) and reports "no blueprint declared" as a result, so a
// read-only query exits 0 the way `project status` and `next` do.
func (s *Service) ProjectBlueprint(ctx context.Context) (*domain.Artifact, error) {
	md, err := s.project()
	if err != nil {
		return nil, err
	}
	id := md.Project.BlueprintArtifactID
	if id == "" {
		return nil, nil
	}
	art, err := record.New(s.Root).GetArtifact(ctx, id)
	if err != nil {
		return nil, s.storeError(err)
	}
	return art, nil
}

// ProjectCreate initializes a project at path (default: the service root)
// and registers it in the user-level registry (方案 §14.4).
func (s *Service) ProjectCreate(ctx context.Context, path string) (*project.Result, string, error) {
	dir := path
	if dir == "" {
		dir = s.Root
	}
	res, err := project.Init(dir, project.Options{Now: s.now()})
	if err != nil {
		var problems config.Problems
		if errors.As(err, &problems) {
			return nil, "", Invalidf(KindInvalid, problems, "invalid managed state (%d problems)", len(problems))
		}
		var pe *project.PreconditionError
		if errors.As(err, &pe) {
			return nil, "", Preconditionf("%s", pe.Msg)
		}
		return nil, "", Internalf("init failed: %v", err)
	}
	regPath, err := registry.Path()
	if err != nil {
		return nil, "", Preconditionf("%v", err)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return nil, "", Internalf("read user registry: %v", err)
	}
	reg.Upsert(registry.Entry{ID: res.ID, Path: res.Root, LastSeenAt: s.now()})
	if err := reg.Save(regPath); err != nil {
		return nil, "", Preconditionf("write user registry: %v", err)
	}
	return res, regPath, nil
}

// ProjectEntry is one registry entry annotated with existence.
type ProjectEntry struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	LastSeenAt string `json:"last_seen_at"`
	Exists     bool   `json:"exists"`
	Current    bool   `json:"current"`
}

// ProjectList lists the user-level registry (方案 §14.4): paths and identity
// only, never project state.
func (s *Service) ProjectList(ctx context.Context) ([]ProjectEntry, error) {
	regPath, err := registry.Path()
	if err != nil {
		return nil, Preconditionf("%v", err)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return nil, Internalf("read user registry: %v", err)
	}
	out := make([]ProjectEntry, 0, len(reg.Projects))
	for _, e := range reg.Projects {
		out = append(out, ProjectEntry{
			ID:         e.ID,
			Path:       e.Path,
			LastSeenAt: e.LastSeenAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Exists:     dirExists(filepath.Join(e.Path, ".devsys")),
			Current:    samePath(e.Path, s.Root),
		})
	}
	return out, nil
}

// ProjectStatusView aggregates the project facts an agent needs to orient.
type ProjectStatusView struct {
	Project *domain.Project     `json:"project"`
	Counts  map[string]int      `json:"counts"`
	Verdict string              `json:"verdict"`
	Risks   []next.Risk         `json:"risks"`
	Next    next.Recommendation `json:"next"`
}

// ProjectStatus reports the project metadata, work item counts by status and
// the readiness verdict (same evaluation `devsys next` renders).
func (s *Service) ProjectStatus(ctx context.Context) (ProjectStatusView, error) {
	md, err := s.project()
	if err != nil {
		return ProjectStatusView{}, err
	}
	report, items, err := s.Next(ctx)
	if err != nil {
		return ProjectStatusView{}, err
	}
	view := ProjectStatusView{
		Project: md.Project,
		Counts:  map[string]int{},
		Verdict: report.Verdict,
		Risks:   report.Risks,
		Next:    report.Next,
	}
	for _, wi := range items {
		view.Counts[wi.Status]++
	}
	return view, nil
}

// readFile reads one managed file under the shared project lock.
func (s *Service) readFile(ctx context.Context, rel string) ([]byte, error) {
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return nil, s.storeError(err)
	}
	var data []byte
	if err := st.Read(ctx, func(r *storage.Reader) error {
		b, ok, err := r.Read(rel)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s is missing", rel)
		}
		data = b
		return nil
	}); err != nil {
		return nil, s.fileError(err)
	}
	return data, nil
}

// fileError classifies managed-file read failures as untrusted state rather
// than as work item problems.
func (s *Service) fileError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, storage.ErrNotInitialized) {
		return Preconditionf("%v", err)
	}
	return Invalidf(KindInvalid, nil, "%v", err)
}

// checkExpect enforces the caller's version hash against the bytes read
// inside the write transaction (fail closed on mismatch).
func checkExpect(expect string, data []byte, what string) error {
	if strings.TrimSpace(expect) == "" {
		return nil
	}
	want, err := hex.DecodeString(expect)
	sum := storage.HashBytes(data)
	if err != nil || len(want) != len(sum) || !bytes.Equal(want, sum[:]) {
		return Invalidf(KindInvalid, nil, "version mismatch: %s changed since your read; re-read and retry", what)
	}
	return nil
}

// samePath compares two project paths the way the registry does: cleaned and
// slash-normalised, case-insensitively only on case-insensitive platforms.
func samePath(a, b string) bool {
	ca := filepath.Clean(filepath.ToSlash(a))
	cb := filepath.Clean(filepath.ToSlash(b))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// classifyWrite maps write-transaction failures: application errors pass
// through, anything else is classified as store state.
func (s *Service) classifyWrite(err error) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	return s.storeError(err)
}
