package app

// project_update 的蓝图字段（#337 M5）：服务层校验矩阵——声明、清除、
// 未注册 id、形态错误、以及 nil（不动）。

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/project"
	"github.com/JAYY513/Workloom/internal/record"
)

func blueprintFixture(t *testing.T) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := filepath.Join(t.TempDir(), "proj")
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if _, err := project.Init(root, project.Options{}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	id, err := record.New(root).CreateArtifact(context.Background(), &domain.Artifact{
		Name: "blueprint.md", Path: "docs/blueprint.md", Status: "draft",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	return New(root), id
}

func classOf(t *testing.T, err error) string {
	t.Helper()
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("error = %T %v, want an app error", err, err)
	}
	return ae.Class()
}

func TestProjectUpdateBlueprintLifecycle(t *testing.T) {
	svc, artifactID := blueprintFixture(t)
	ctx := context.Background()

	view, err := svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new(artifactID)})
	if err != nil {
		t.Fatalf("declare blueprint: %v", err)
	}
	if view.Project.BlueprintArtifactID != artifactID {
		t.Fatalf("blueprint = %q, want %q", view.Project.BlueprintArtifactID, artifactID)
	}

	// nil leaves it untouched.
	view, err = svc.ProjectUpdate(ctx, UpdateProjectRequest{Status: new("active")})
	if err != nil {
		t.Fatalf("status-only update: %v", err)
	}
	if view.Project.BlueprintArtifactID != artifactID {
		t.Fatalf("nil blueprint field wiped the declaration: %q", view.Project.BlueprintArtifactID)
	}

	// empty clears.
	view, err = svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new("")})
	if err != nil {
		t.Fatalf("clear blueprint: %v", err)
	}
	if view.Project.BlueprintArtifactID != "" {
		t.Fatalf("clear left %q", view.Project.BlueprintArtifactID)
	}

	// whitespace trims to clear too.
	if _, err = svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new("  ")}); err != nil {
		t.Fatalf("whitespace clear: %v", err)
	}
}

func TestBlueprintWarningsDescribeIncompleteDraft(t *testing.T) {
	warnings := BlueprintWarnings(&domain.Artifact{Status: "draft"})
	if len(warnings) != 2 || !strings.Contains(strings.Join(warnings, "\n"), "draft") || !strings.Contains(strings.Join(warnings, "\n"), "no source") {
		t.Fatalf("warnings = %v, want draft and provenance warnings", warnings)
	}
	if got := BlueprintWarnings(&domain.Artifact{Status: "active", Path: "docs/blueprint.md", Source: "user"}); len(got) != 0 {
		t.Fatalf("complete sourced blueprint warnings = %v, want none", got)
	}
}

func TestArtifactRegisterUsesLocalPathAsProvenance(t *testing.T) {
	svc, _ := blueprintFixture(t)
	path := filepath.Join(svc.Root, "product-blueprint.yaml")
	if err := os.WriteFile(path, []byte("name: Local\n"), 0o600); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	view, err := svc.ArtifactRegister(context.Background(), RegisterArtifactRequest{
		Name: "product-blueprint.yaml", Path: "product-blueprint.yaml", Status: "active",
		Actor: "tester", Reason: "register local blueprint",
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}
	if view.Artifact.Source != "product-blueprint.yaml" {
		t.Fatalf("source = %q, want local path", view.Artifact.Source)
	}
	if warnings := BlueprintWarnings(view.Artifact); len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestImportBlueprintPreservesMissingFieldsAndUpdatesBinding(t *testing.T) {
	svc, _ := blueprintFixture(t)
	ctx := context.Background()
	content := "name: Imported\ngoals:\n  - ship safely\nscope:\n  in:\n    - service\n  out:\n    - mobile\ntech_stack:\n  - Go\n"
	if err := os.WriteFile(filepath.Join(svc.Root, "product-blueprint.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	id, err := record.New(svc.Root).CreateArtifact(ctx, &domain.Artifact{Name: "product-blueprint.yaml", Path: "product-blueprint.yaml", Status: "active", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	before, err := svc.ProjectGet(ctx)
	if err != nil {
		t.Fatalf("project get: %v", err)
	}
	view, err := svc.ImportBlueprint(ctx, ImportBlueprintRequest{ArtifactID: id, Expect: before.Version, Actor: "tester", Reason: "sync approved blueprint"})
	if err != nil {
		t.Fatalf("import blueprint: %v", err)
	}
	if view.Project.Name != "Imported" || len(view.Project.Goals) != 1 || view.Project.Goals[0] != "ship safely" {
		t.Fatalf("imported project = %+v", view.Project)
	}
	if view.Project.Description != "" || len(view.Project.Constraints) != 0 || view.Project.BlueprintArtifactID != id {
		t.Fatalf("missing fields or binding changed unexpectedly: %+v", view.Project)
	}
}

func TestImportBlueprintRejectsInvalidDocumentWithoutWriting(t *testing.T) {
	svc, _ := blueprintFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(svc.Root, "product-blueprint.yaml"), []byte("goals: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := record.New(svc.Root).CreateArtifact(ctx, &domain.Artifact{Name: "product-blueprint.yaml", Path: "product-blueprint.yaml", Status: "active", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.ProjectGet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportBlueprint(ctx, ImportBlueprintRequest{ArtifactID: id, Expect: before.Version, Actor: "tester", Reason: "reject malformed"}); err == nil {
		t.Fatal("invalid blueprint unexpectedly imported")
	}
	after, err := svc.ProjectGet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Project.BlueprintArtifactID != "" {
		t.Fatalf("invalid import changed project: before=%+v after=%+v", before, after)
	}
}

func TestProjectUpdateBlueprintRejects(t *testing.T) {
	svc, _ := blueprintFixture(t)
	ctx := context.Background()

	_, err := svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new("artifact-404")})
	if classOf(t, err) != KindPrecondition || !strings.Contains(err.Error(), "artifact register") {
		t.Fatalf("unknown artifact = %v, want precondition naming the way out", err)
	}
	_, err = svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new("nope")})
	if classOf(t, err) != KindUsage {
		t.Fatalf("malformed id = %v, want usage", err)
	}
	_, err = svc.ProjectUpdate(ctx, UpdateProjectRequest{Description: new("x")})
	if err != nil {
		t.Fatalf("plain update must still work: %v", err)
	}
}

// TestProjectBlueprintFollowsAppendedVersion pins the F-03 fix: `artifact
// update` appends a version linked by previous_id, so a project.yaml that
// still names the bound ID must resolve to the newest version instead of
// reporting the stale draft as if nothing had changed.
func TestProjectBlueprintFollowsAppendedVersion(t *testing.T) {
	svc, boundID := blueprintFixture(t)
	ctx := context.Background()

	if _, err := svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new(boundID)}); err != nil {
		t.Fatalf("declare blueprint: %v", err)
	}

	before, err := svc.ProjectBlueprint(ctx)
	if err != nil {
		t.Fatalf("read blueprint: %v", err)
	}
	if before.Artifact == nil || before.Artifact.ID != boundID || before.Artifact.Version != 1 {
		t.Fatalf("blueprint = %+v, want the bound %s at version 1", before.Artifact, boundID)
	}
	if before.Stale() || before.BlueprintStaleNotice() != "" {
		t.Fatalf("freshly bound blueprint reported stale: %q", before.BlueprintStaleNotice())
	}

	updated, err := svc.ArtifactUpdate(ctx, UpdateArtifactRequest{
		ID: boundID, Status: "active", Source: "user", Actor: "tester", Reason: "reviewed",
	})
	if err != nil {
		t.Fatalf("append version: %v", err)
	}
	if updated.Artifact.ID == boundID {
		t.Fatalf("artifact update reused %s; versions must be appended", boundID)
	}

	after, err := svc.ProjectBlueprint(ctx)
	if err != nil {
		t.Fatalf("read blueprint after append: %v", err)
	}
	if after.Artifact == nil || after.Artifact.ID != updated.Artifact.ID || after.Artifact.Status != "active" {
		t.Fatalf("blueprint = %+v, want the appended %s (active)", after.Artifact, updated.Artifact.ID)
	}
	if after.Artifact.Version != 2 {
		t.Fatalf("resolved version = %d, want 2", after.Artifact.Version)
	}
	if !after.Stale() {
		t.Fatalf("blueprint bound to %s, resolved %s, but Stale() is false", after.BoundID, after.Artifact.ID)
	}
	notice := after.BlueprintStaleNotice()
	if !strings.Contains(notice, boundID) || !strings.Contains(notice, updated.Artifact.ID) {
		t.Fatalf("notice %q must name both the bound and the resolved id", notice)
	}
	if !strings.Contains(notice, "project update --blueprint-artifact "+updated.Artifact.ID) {
		t.Fatalf("notice %q must carry the re-pointing command", notice)
	}
	if warnings := BlueprintWarnings(after.Artifact); len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none once the newest version is active and sourced", warnings)
	}

	// Re-pointing the declaration at the newest version clears staleness.
	if _, err := svc.ProjectUpdate(ctx, UpdateProjectRequest{BlueprintArtifactID: new(updated.Artifact.ID)}); err != nil {
		t.Fatalf("re-point blueprint: %v", err)
	}
	repointed, err := svc.ProjectBlueprint(ctx)
	if err != nil {
		t.Fatalf("read re-pointed blueprint: %v", err)
	}
	if repointed.Stale() || repointed.BlueprintStaleNotice() != "" {
		t.Fatalf("re-pointed blueprint reported stale: %s", repointed.BlueprintStaleNotice())
	}
	if repointed.BoundID != updated.Artifact.ID {
		t.Fatalf("bound = %q, want %q", repointed.BoundID, updated.Artifact.ID)
	}
}
