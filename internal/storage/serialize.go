package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// EncodeYAML renders v deterministically so that repeated writes of the same
// value produce identical bytes (方案 §14.1 文本优先/可 diff): key order follows
// struct field order (maps sort by key), indentation is two spaces and the
// output always ends in exactly one LF. State files must be produced through an
// encoder like this one rather than by hand-rolling YAML.
func EncodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	out := buf.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

// DecodeYAML decodes exactly one YAML document into v with unknown keys
// rejected, so typos and half-written files fail loudly instead of being
// silently ignored. Field errors carry line numbers from the parser.
func DecodeYAML(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode yaml: %w", err)
	}
	var extra any
	switch err := dec.Decode(&extra); {
	case err == nil:
		return errors.New("decode yaml: unexpected second document")
	case errors.Is(err, io.EOF):
		return nil
	default:
		return fmt.Errorf("decode yaml: %w", err)
	}
}

// EncodeJSON renders v as compact deterministic JSON. Struct fields keep their
// declaration order and map keys are sorted; HTML escaping is off so state
// files stay readable and diffable.
func EncodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MarshalJSONL renders one JSONL record: compact JSON plus a trailing LF.
func MarshalJSONL(v any) ([]byte, error) {
	b, err := EncodeJSON(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// CheckSchemaVersion validates the top-level schema_version field of a managed
// file against the version this build understands (方案 §14.1: 读到未知版本时
// 拒绝写入). Unlike DecodeYAML it ignores unknown keys on purpose, because it
// runs before the strict per-file decoding introduced in M0.4.
func CheckSchemaVersion(data []byte, supported int) error {
	var doc struct {
		SchemaVersion *int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: parse: %v", ErrSchemaVersion, err)
	}
	if doc.SchemaVersion == nil {
		return fmt.Errorf("%w: missing schema_version", ErrSchemaVersion)
	}
	if *doc.SchemaVersion != supported {
		return fmt.Errorf("%w: got %d, supported %d", ErrSchemaVersion, *doc.SchemaVersion, supported)
	}
	return nil
}
