package mcp

import (
	"context"
	"os"
	"path/filepath"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/config"
)

// healthResult is the introspection payload of the health tool.
type healthResult struct {
	Status   string           `json:"status"`
	Server   healthServer     `json:"server"`
	Profiles []string         `json:"profiles"`
	Tier     string           `json:"tier"`
	Root     string           `json:"root"`
	Project  *healthProject   `json:"project"`
	Devsys   healthDevsys     `json:"devsys"`
	Problems []config.Problem `json:"problems,omitempty"`
}

type healthServer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type healthProject struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SchemaVersion int    `json:"schema_version"`
}

type healthDevsys struct {
	Present bool `json:"present"`
}

type healthInput struct{}

// registerHealth reports server identity, active profiles and project facts.
// It is read-only: plain reads, no lock, no recovery — so it also answers on
// state that write commands must refuse (方案 §15.4).
func registerHealth(s *mcpsdk.Server, cfg Config) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "health",
		Description: "Report the devsys MCP server identity, active profiles and project facts.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ healthInput) (*mcpsdk.CallToolResult, healthResult, error) {
		md, problems := config.Load(cfg.Root)
		devsysDir := filepath.Join(cfg.Root, ".devsys")
		present := false
		if info, err := os.Stat(devsysDir); err == nil && info.IsDir() {
			present = true
		}
		tier := cfg.Tier
		if tier == "" {
			tier = DefaultTier()
		}
		result := healthResult{
			Status:   "ok",
			Server:   healthServer{Name: ServerName, Version: cfg.ServerVersion},
			Profiles: append([]string(nil), cfg.Profiles...),
			Tier:     tier,
			Root:     cfg.Root,
			Devsys:   healthDevsys{Present: present},
			Problems: problems,
		}
		if md != nil && md.Project != nil {
			result.Project = &healthProject{
				ID:            md.Project.ID,
				Name:          md.Project.Name,
				SchemaVersion: md.Project.SchemaVersion,
			}
		}
		return nil, result, nil
	})
}
