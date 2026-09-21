package knowledge

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JAYY513/Workloom/internal/storage"
)

// FormatVersion is the page format contract version this build speaks; a
// generator is told which one it must produce (方案 §12.6).
const FormatVersion = 1

// ScopeModes a refresh can ask for.
const (
	ScopeFull     = "full"
	ScopeAffected = "affected"
)

// ScopeFile is the generation scope handed to a generator. It is written on
// every refresh so the request is inspectable after the fact, and so a long
// page list does not have to survive a command line.
const ScopeFile = Dir + "/refresh-scope.json"

// Scope is the generator contract's input (方案 §12.6): project root, the
// generation range, and the page format version.
type Scope struct {
	FormatVersion int      `json:"format_version"`
	Project       string   `json:"project"`
	Mode          string   `json:"mode"`
	Baseline      string   `json:"baseline,omitempty"`
	Head          string   `json:"head,omitempty"`
	Pages         []string `json:"pages"`
}

// GeneratorCommand builds the argv for one generator run from the configured
// command. The command is split on whitespace and never handed to a shell:
// a configured generator is a program with arguments, and a shell would add a
// second, platform-specific parsing layer to the contract.
func GeneratorCommand(command string, scopePath, projectRoot string) ([]string, error) {
	argv := strings.Fields(command)
	if len(argv) == 0 {
		return nil, fmt.Errorf("generator command is empty")
	}
	return append(argv,
		"--project", projectRoot,
		"--scope-file", scopePath,
		"--format-version", strconv.Itoa(FormatVersion),
	), nil
}

// WriteScope stores the scope atomically and returns its project-relative path.
func WriteScope(root string, scope Scope) (string, error) {
	data, err := json.MarshalIndent(scope, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode scope: %w", err)
	}
	data = append(data, '\n')
	if err := storage.AtomicWrite(filepath.Join(root, filepath.FromSlash(ScopeFile)), data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", ScopeFile, err)
	}
	return ScopeFile, nil
}
