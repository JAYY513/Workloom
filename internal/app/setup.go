package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/project"
	"github.com/JAYY513/Workloom/internal/reconcile"
	"github.com/JAYY513/Workloom/internal/storage"

	"gopkg.in/yaml.v3"
)

// DefaultSetupTemplate is the neutral policy selected by `workloom setup`.
const DefaultSetupTemplate = "intake"

// DefaultSetupTemplates are the executable policies installed for every new
// project. reference-template remains opt-in documentation only.
var DefaultSetupTemplates = []string{"intake", "quick-fix", "feature-development", "architecture-change"}

// SetupStep is one line of the setup report. OK false on blueprint or mcp
// is a report, not a failure: those steps do not write and do not gate Ready.
type SetupStep struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Detail  string `json:"detail"`
}

// SetupView is the product onboarding report. Ready means init, the starter
// workflow, wire, config, and doctor succeeded. A missing blueprint or MCP
// client config is reported in Steps and Next; setup does not invent either.
type SetupView struct {
	Ready    bool        `json:"ready"`
	Template string      `json:"template"`
	Steps    []SetupStep `json:"steps"`
	Next     string      `json:"next,omitempty"`
}

// Setup runs the project onboarding sequence and stops at the first hard
// failure. It composes the existing service methods; it does not add a
// second write path. Re-running is idempotent: an existing policy file is
// left in place.
func (s *Service) Setup(ctx context.Context, template string) (SetupView, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		template = DefaultSetupTemplate
	}
	view := SetupView{Template: template}
	if !slices.Contains(WorkflowTemplates(), template) {
		return view, Usagef("unknown workflow template %q (available: %s)",
			template, strings.Join(WorkflowTemplates(), ", "))
	}

	_, projectStatErr := os.Stat(filepath.Join(s.Root, project.DevsysDirName))
	if projectStatErr != nil && !os.IsNotExist(projectStatErr) {
		return view, projectStatErr
	}
	res, _, err := s.ProjectCreate(ctx, "")
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "init", Detail: err.Error()})
		view.Next = "fix the init error, then rerun `workloom setup`"
		return view, err
	}
	initDetail := "already initialized"
	if len(res.Created) > 0 {
		initDetail = fmt.Sprintf("created %d paths", len(res.Created))
	}
	view.Steps = append(view.Steps, SetupStep{Name: "init", OK: true, Detail: initDetail})

	templates := append([]string(nil), DefaultSetupTemplates...)
	if template == "reference-template" {
		templates = []string{template}
	} else {
		for i, id := range templates {
			if id == template {
				templates[0], templates[i] = templates[i], templates[0]
				break
			}
		}
	}
	for _, id := range templates {
		policy := filepath.Join(s.Root, project.DevsysDirName, "workflows", id+".md")
		if _, statErr := os.Stat(policy); statErr == nil {
			view.Steps = append(view.Steps, SetupStep{
				Name: "workflow", OK: true, Skipped: true,
				Detail: id + " already present; left in place",
			})
		} else if !os.IsNotExist(statErr) {
			view.Steps = append(view.Steps, SetupStep{Name: "workflow", Detail: statErr.Error()})
			return view, Internalf("inspect workflow %s: %v", policy, statErr)
		} else {
			installed, err := s.WorkflowInitTemplate(ctx, id)
			if err != nil {
				view.Steps = append(view.Steps, SetupStep{Name: "workflow", Detail: err.Error()})
				return view, err
			}
			view.Steps = append(view.Steps, SetupStep{Name: "workflow", OK: true, Detail: "installed " + installed.Template})
		}
	}

	if changed, err := s.ensureDefaultPolicy(ctx, template, os.IsNotExist(projectStatErr)); err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "policy", Detail: err.Error()})
		return view, err
	} else if changed {
		view.Steps = append(view.Steps, SetupStep{Name: "policy", OK: true, Detail: "default_policy=\"" + template + "\""})
	}

	wired, err := s.Wire(ctx, false)
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "wire", Detail: err.Error()})
		return view, err
	}
	skillChanged, err := s.WriteSkill()
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "wire", Detail: err.Error()})
		return view, err
	}
	wireDetail := "already wired"
	switch {
	case wired.Created:
		wireDetail = "created AGENTS.md"
	case wired.Changed:
		wireDetail = "updated AGENTS.md"
	}
	if len(skillChanged) > 0 {
		wireDetail += fmt.Sprintf("; skill wrote %d files", len(skillChanged))
	} else {
		wireDetail += "; skill already installed"
	}
	view.Steps = append(view.Steps, SetupStep{Name: "wire", OK: true, Detail: wireDetail})

	_, problems := config.Diagnose(s.Root)
	if len(problems) > 0 {
		view.Steps = append(view.Steps, SetupStep{Name: "config", Detail: problems[0].String()})
		view.Next = "workloom config check"
		return view, Invalidf(KindInvalid, problems, "invalid managed state (%d problems)", len(problems))
	}
	view.Steps = append(view.Steps, SetupStep{
		Name: "config", OK: true,
		Detail: fmt.Sprintf("%d files checked", len(config.ManagedFiles())),
	})

	var ownedFail []string
	mcp := WireCheckLine{Name: "mcp", Detail: "mcp check missing"}
	for _, line := range s.WireCheck().Lines {
		switch line.Name {
		case "mcp":
			mcp = line
		case ".devsys", "schema", "registry", "AGENTS.md", "skill":
			if !line.OK {
				ownedFail = append(ownedFail, line.Name+": "+line.Detail)
			}
		}
	}
	if len(ownedFail) > 0 {
		view.Steps = append(view.Steps, SetupStep{Name: "wire-check", Detail: strings.Join(ownedFail, "; ")})
		view.Next = "workloom wire --check"
		return view, Preconditionf("setup wrote state that wire --check still rejects: %s", strings.Join(ownedFail, "; "))
	}
	view.Steps = append(view.Steps, SetupStep{
		Name: "wire-check", OK: true,
		Detail: "owned checks passed",
	})
	view.Steps = append(view.Steps, SetupStep{Name: "mcp", OK: mcp.OK, Detail: mcp.Detail})

	session, err := s.SessionStart(ctx, SessionRequest{Compact: true})
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "prime", Detail: err.Error()})
		return view, err
	}
	primeDetail := session.RecommendedNextAction.Reason
	if cmd := session.RecommendedNextAction.Command; cmd != "" {
		primeDetail = cmd
	}
	view.Steps = append(view.Steps, SetupStep{Name: "prime", OK: true, Detail: primeDetail})

	blueprint, err := s.ProjectBlueprint(ctx)
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "blueprint", Detail: err.Error()})
		return view, err
	}
	var blueprintMissing bool
	if blueprint.Artifact == nil {
		blueprintMissing = true
		view.Steps = append(view.Steps, SetupStep{
			Name:   "blueprint",
			Detail: "no blueprint declared; ask for project goals, then `workloom project update --blueprint-artifact <artifact-id>`",
		})
	} else {
		detail := blueprint.Artifact.ID
		if stale := blueprint.BlueprintStaleNotice(); stale != "" {
			detail += "; " + stale
		}
		if warnings := BlueprintWarnings(blueprint.Artifact); len(warnings) > 0 {
			detail += "; " + strings.Join(warnings, "; ")
		}
		view.Steps = append(view.Steps, SetupStep{Name: "blueprint", OK: true, Detail: detail})
	}

	rep, err := reconcile.Doctor(ctx, s.Root, reconcile.Options{})
	if err != nil {
		view.Steps = append(view.Steps, SetupStep{Name: "doctor", Detail: err.Error()})
		return view, Internalf("doctor: %v", err)
	}
	if len(rep.InvalidFiles) > 0 {
		view.Steps = append(view.Steps, SetupStep{Name: "doctor", Detail: rep.InvalidFiles[0].String()})
		view.Next = "workloom doctor"
		return view, Invalidf(KindInvalid, nil, "unreadable managed files (%d)", len(rep.InvalidFiles))
	}
	docDetail := "clean"
	switch {
	case len(rep.PendingTransactions) > 0:
		docDetail = fmt.Sprintf("%d pending transactions; run `workloom recover`", len(rep.PendingTransactions))
	case !rep.InspectionOK && rep.Note != "":
		docDetail = rep.Note
	}
	view.Steps = append(view.Steps, SetupStep{Name: "doctor", OK: true, Detail: docDetail})

	view.Ready = true
	switch {
	case blueprintMissing && session.RecommendedNextAction.Command != "":
		view.Next = "no blueprint declared; ask for project goals before `workloom project update --blueprint-artifact <artifact-id>`; then " + session.RecommendedNextAction.Command
	case blueprintMissing:
		view.Next = "no blueprint declared; ask for project goals before `workloom project update --blueprint-artifact <artifact-id>`"
	case session.RecommendedNextAction.Command != "":
		view.Next = session.RecommendedNextAction.Command
	}
	return view, nil
}

func (s *Service) ensureDefaultPolicy(ctx context.Context, template string, fresh bool) (bool, error) {
	if !fresh || template == "reference-template" {
		return false, nil
	}
	md, problems := config.Load(s.Root)
	if len(problems) > 0 || md == nil || md.Config == nil || md.Config.DefaultPolicy != "" {
		return false, nil
	}
	st, err := storage.Open(s.Root, storage.Options{})
	if err != nil {
		return false, err
	}
	changed := false
	err = st.Write(ctx, func(tx *storage.Tx) error {
		raw, ok, err := tx.ReadForExpect(config.ConfigFile)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		var document yaml.Node
		if err := yaml.Unmarshal(raw, &document); err != nil {
			return err
		}
		for i := 0; i+1 < len(document.Content[0].Content); i += 2 {
			if document.Content[0].Content[i].Value == "default_policy" {
				return nil // Explicit empty value is an opt-out.
			}
		}
		updated := append([]byte(nil), raw...)
		if len(updated) > 0 && !bytes.HasSuffix(updated, []byte("\n")) {
			updated = append(updated, '\n')
		}
		updated = append(updated, []byte("default_policy: "+template+"\n")...)
		if err := tx.Put(config.ConfigFile, updated, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}
