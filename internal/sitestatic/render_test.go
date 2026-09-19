package sitestatic

import (
	"testing"
)

// TestRenderPageMatchesBuild: the in-memory serve render is byte-identical to
// what Build writes to disk for the same model and timestamp — the static
// site and the served pages can never fork.
func TestRenderPageMatchesBuild(t *testing.T) {
	m := testModel()
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	for _, file := range PageFiles {
		got, err := RenderPage(m, fixedGen, file)
		if err != nil {
			t.Fatalf("RenderPage %s: %v", file, err)
		}
		if want := readFile(t, out, file); got != want {
			t.Errorf("%s: memory render differs from disk bytes", file)
		}
	}
}

// TestModelJSONMatchesBuild pins the data/model.json 口径 shared by Build and
// the serve side.
func TestModelJSONMatchesBuild(t *testing.T) {
	m := testModel()
	out := t.TempDir() + "/site"
	buildTo(t, m, out)
	raw, err := ModelJSON(m)
	if err != nil {
		t.Fatalf("ModelJSON: %v", err)
	}
	if want := readFile(t, out, "data/model.json"); string(raw) != want {
		t.Errorf("ModelJSON differs from disk data/model.json")
	}
	if Stylesheet() == "" {
		t.Errorf("Stylesheet is empty")
	}
}

// TestRenderPageUnknown rejects unknown pages and nil models.
func TestRenderPageUnknown(t *testing.T) {
	if _, err := RenderPage(testModel(), fixedGen, "nope.html"); err == nil {
		t.Errorf("RenderPage unknown page: want error")
	}
	if _, err := RenderPage(nil, fixedGen, "index.html"); err == nil {
		t.Errorf("RenderPage nil model: want error")
	}
	if _, err := ModelJSON(nil); err == nil {
		t.Errorf("ModelJSON nil model: want error")
	}
}
