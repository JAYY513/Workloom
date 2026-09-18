package app

import (
	"context"
	"os"
	"path/filepath"
)

// KnowledgeStatusView reports the knowledge layer's availability. The index
// layer arrives with M5 (方案 §12.6): until then the honest answer is
// "unavailable, no index", which is exactly the degradation contract — a
// missing generator must not break the rest of the system.
type KnowledgeStatusView struct {
	Status     string   `json:"status"`
	Layer      string   `json:"layer"`
	Configured bool     `json:"configured"`
	Reason     string   `json:"reason"`
	Paths      []string `json:"paths,omitempty"`
}

// KnowledgeStatus reports whether a knowledge index is present under
// .devsys/knowledge/ and what the layer can answer today.
func (s *Service) KnowledgeStatus(ctx context.Context) (KnowledgeStatusView, error) {
	if _, err := s.project(); err != nil {
		return KnowledgeStatusView{}, err
	}
	dir := filepath.Join(s.Root, ".devsys", "knowledge")
	view := KnowledgeStatusView{Status: "unavailable", Layer: "index"}
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil:
		view.Configured = true
		for _, entry := range entries {
			view.Paths = append(view.Paths, filepath.ToSlash(filepath.Join(".devsys/knowledge", entry.Name())))
		}
		view.Reason = "knowledge index directory exists but the index layer arrives with M5 (方案 §12.6); no page contract is enforced yet"
	case os.IsNotExist(err):
		view.Reason = "no .devsys/knowledge/ index: the knowledge layer arrives with M5 (方案 §12.6); queries degrade to record and event tools"
	default:
		return KnowledgeStatusView{}, Internalf("inspect knowledge directory: %v", err)
	}
	return view, nil
}
