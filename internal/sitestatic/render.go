package sitestatic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/view"
)

// RenderPage renders one site page (a member of PageFiles) to a string, the
// in-memory twin of what Build writes to disk for the same model and
// generatedAt. The serve side (M7.3) renders every request through this so the
// static site and the served pages can never fork: same templates, same data,
// same bytes.
func RenderPage(m *view.Model, generatedAt time.Time, file string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("sitestatic: nil model")
	}
	for _, pm := range pageMetas(m) {
		if pm.file != file {
			continue
		}
		var sb strings.Builder
		data := newSiteData(m, generatedAt, pm.title, pm.file, pm.section, pm.sources)
		if err := siteTmpl.ExecuteTemplate(&sb, pm.tmpl, data); err != nil {
			return "", fmt.Errorf("sitestatic: render %s: %w", file, err)
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("sitestatic: unknown page %q", file)
}

// ModelJSON encodes the model the way data/model.json carries it: indented,
// trailing newline, verbatim (no generated_at inside).
func ModelJSON(m *view.Model) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("sitestatic: nil model")
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("sitestatic: encode model: %w", err)
	}
	return append(out, '\n'), nil
}

// Stylesheet returns the whole site stylesheet.
func Stylesheet() string { return styleCSS }
