package storage

import (
	"errors"
	"strings"
	"testing"
)

type sampleDoc struct {
	SchemaVersion int      `yaml:"schema_version"`
	ID            string   `yaml:"id"`
	Path          string   `yaml:"path"`
	CreatedAt     string   `yaml:"created_at"`
	Ambiguous     string   `yaml:"ambiguous"`
	Empty         string   `yaml:"empty"`
	Tags          []string `yaml:"tags"`
}

func sample() sampleDoc {
	return sampleDoc{
		SchemaVersion: 1,
		ID:            "tempmonitorsystem",
		Path:          `C:\Source\ai\Workloom`,
		CreatedAt:     "2026-09-17T09:00:00Z",
		Ambiguous:     "yes",
		Tags:          []string{},
	}
}

func TestEncodeYAMLIsStableAndOrdered(t *testing.T) {
	first, err := EncodeYAML(sample())
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeYAML(sample())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("encoding is not byte-stable:\n%s\n---\n%s", first, second)
	}
	const want = "schema_version: 1\n" +
		"id: tempmonitorsystem\n" +
		"path: C:\\Source\\ai\\Workloom\n" +
		"created_at: \"2026-09-17T09:00:00Z\"\n" +
		"ambiguous: \"yes\"\n" +
		"empty: \"\"\n" +
		"tags: []\n"
	if string(first) != want {
		t.Errorf("encoded YAML =\n%s\nwant\n%s", first, want)
	}
}

func TestDecodeYAMLRejectsUnknownKeysAndExtraDocuments(t *testing.T) {
	var doc sampleDoc
	err := DecodeYAML([]byte("schema_version: 1\nunknown_key: 1\n"), &doc)
	if err == nil {
		t.Fatal("unknown keys must be rejected")
	}
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "unknown_key") {
		t.Errorf("error should carry the line and the key: %v", err)
	}

	err = DecodeYAML([]byte("schema_version: 1\n---\nschema_version: 2\n"), &doc)
	if err == nil || !strings.Contains(err.Error(), "second document") {
		t.Errorf("error = %v, want a second-document rejection", err)
	}

	if err := DecodeYAML([]byte("schema_version: [1,2\n"), &doc); err == nil {
		t.Fatal("syntax errors must be reported")
	}
}

func TestDecodeYAMLRoundTripAndLegacyInput(t *testing.T) {
	data, err := EncodeYAML(sample())
	if err != nil {
		t.Fatal(err)
	}
	var back sampleDoc
	if err := DecodeYAML(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != "tempmonitorsystem" || back.Path != `C:\Source\ai\Workloom` || back.Ambiguous != "yes" || back.Tags == nil {
		t.Errorf("round trip = %+v", back)
	}

	// M0.2 wrote single-quoted scalars and bare paths; both still decode.
	legacy := "# comment\nprojects:\n  - id: 'repo'\n    path: C:\\Users\\q\\repo\n    last_seen_at: 2026-09-17T09:17:47Z\n"
	var reg struct {
		Projects []struct {
			ID         string `yaml:"id"`
			Path       string `yaml:"path"`
			LastSeenAt string `yaml:"last_seen_at"`
		} `yaml:"projects"`
	}
	if err := DecodeYAML([]byte(legacy), &reg); err != nil {
		t.Fatalf("legacy registry: %v", err)
	}
	if len(reg.Projects) != 1 || reg.Projects[0].ID != "repo" || reg.Projects[0].Path != `C:\Users\q\repo` {
		t.Errorf("legacy registry = %+v", reg.Projects)
	}
}

func TestCheckSchemaVersion(t *testing.T) {
	ok := []byte("schema_version: 1\nid: x\n")
	if err := CheckSchemaVersion(ok, 1); err != nil {
		t.Errorf("matching version: %v", err)
	}
	if err := CheckSchemaVersion(ok, 2); !errors.Is(err, ErrSchemaVersion) {
		t.Errorf("unknown version error = %v", err)
	}
	if err := CheckSchemaVersion([]byte("id: x\n"), 1); !errors.Is(err, ErrSchemaVersion) {
		t.Errorf("missing version error = %v", err)
	}
	if err := CheckSchemaVersion([]byte("schema_version: [\n"), 1); !errors.Is(err, ErrSchemaVersion) {
		t.Errorf("unparsable version error = %v", err)
	}
}

func TestEncodeJSONAndJSONL(t *testing.T) {
	type record struct {
		Event string `json:"event"`
		Note  string `json:"note"`
	}
	line, err := MarshalJSONL(record{Event: "created", Note: "a<b & c"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"event\":\"created\",\"note\":\"a<b & c\"}\n"
	if string(line) != want {
		t.Errorf("jsonl = %q, want %q", line, want)
	}

	blob, err := EncodeJSON(map[string]int{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) != "{\"a\":1,\"b\":2}" {
		t.Errorf("json = %s", blob)
	}
}
