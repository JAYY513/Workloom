package app

import (
	"context"
	"io/fs"
	"sort"
	"strings"

	"github.com/JAYY513/Workloom"
	"github.com/JAYY513/Workloom/internal/project"
	"github.com/JAYY513/Workloom/internal/storage"
)

// workflowTemplatesDir is the embed path (inside package workloom) of the
// example policies; docs/examples/workflows/ is the single source.
const workflowTemplatesDir = "docs/examples/workflows"

// WorkflowInitView reports what `workflow init --template` wrote.
type WorkflowInitView struct {
	Template string `json:"template"`
	Path     string `json:"path"` // project-relative, e.g. .devsys/workflows/quick-fix.md
}

// WorkflowTemplates lists the example workflow template ids available to
// `workflow init --template`, sorted for stable display.
func WorkflowTemplates() []string {
	entries, err := fs.ReadDir(workloom.WorkflowTemplatesFS, workflowTemplatesDir)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".md") && !e.IsDir() {
			ids = append(ids, strings.TrimSuffix(name, ".md"))
		}
	}
	sort.Strings(ids)
	return ids
}

// WorkflowInitTemplate installs one embedded example policy as
// .devsys/workflows/<id>.md, the directory workflow.Load reads. An existing
// file is never overwritten: the operator's edits win, and re-running init
// says so instead of silently resetting the policy.
func (s *Service) WorkflowInitTemplate(ctx context.Context, id string) (WorkflowInitView, error) {
	id = strings.TrimSpace(id)
	data, err := fs.ReadFile(workloom.WorkflowTemplatesFS, workflowTemplatesDir+"/"+id+".md")
	if err != nil {
		return WorkflowInitView{}, Usagef("unknown workflow template %q (available: %s)",
			id, strings.Join(WorkflowTemplates(), ", "))
	}
	rel := "workflows/" + id + ".md"
	display := project.DevsysDirName + "/" + rel
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return WorkflowInitView{}, s.storeError(err)
	}
	if err := st.Write(ctx, func(tx *storage.Tx) error {
		_, exists, err := tx.ReadForExpect(rel)
		if err != nil {
			return err
		}
		if exists {
			return Usagef("policy %q already exists at %s; edit it in place instead of re-initializing", id, display)
		}
		return tx.Put(rel, data, storage.ExpectAbsent())
	}); err != nil {
		return WorkflowInitView{}, s.classifyWrite(err)
	}
	return WorkflowInitView{Template: id, Path: display}, nil
}
