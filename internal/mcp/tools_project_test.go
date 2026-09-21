package mcp

// project_update 的 blueprint_artifact_id 字段（#337 M5）：MCP 入口与
// CLI 共用同一个服务校验。

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/app"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/record"
)

func TestProjectUpdateBlueprintArtifactField(t *testing.T) {
	root, _ := runFixture(t)
	artifactID, err := record.New(root).CreateArtifact(context.Background(), &domain.Artifact{
		Name: "blueprint.md", Path: "docs/blueprint.md", Status: "draft",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	cs := session(t, Config{Root: root, Profiles: []string{ProfileAdmin}, Tier: TierStandard})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "project_update",
		Arguments: map[string]any{
			"blueprint_artifact_id": artifactID,
			"expect":                "",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("project_update: err=%v res=%+v", err, res)
	}
	if got := payload[app.ProjectView](t, res).Project.BlueprintArtifactID; got != artifactID {
		t.Fatalf("blueprint_artifact_id = %q, want %q", got, artifactID)
	}

	res, err = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "project_update",
		Arguments: map[string]any{
			"blueprint_artifact_id": "artifact-404",
			"expect":                "",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("unknown artifact id must fail as a tool error: %+v", res)
	}
}
