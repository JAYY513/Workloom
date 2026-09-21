package app

// project_update 的蓝图字段（#337 M5）：服务层校验矩阵——声明、清除、
// 未注册 id、形态错误、以及 nil（不动）。

import (
	"context"
	"errors"
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
