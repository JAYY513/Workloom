package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"gopkg.in/yaml.v3"
	"workloom/internal/storage"
)

// EncodeYAML preserves null collections as well as empty collections. yaml.v3's
// default slice encoding conflates them, which would lose record information.
func EncodeYAML(v any) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(v); err != nil {
		return nil, err
	}
	preserveNulls(&node, reflect.ValueOf(v))
	b, err := storage.EncodeYAML(&node)
	if err != nil {
		return nil, err
	}
	if err := storage.CheckSchemaVersion(b, SchemaVersion); err != nil {
		return nil, err
	}
	return b, nil
}

func preserveNulls(n *yaml.Node, v reflect.Value) {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Slice {
		if v.IsNil() {
			*n = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
			return
		}
		for i := range n.Content {
			if i < v.Len() {
				preserveNulls(n.Content[i], v.Index(i))
			}
		}
	}
	if v.Kind() != reflect.Struct || n.Kind != yaml.MappingNode {
		return
	}
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == ",inline" {
			preserveNulls(n, v.Field(i))
			continue
		}
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == tag {
				preserveNulls(n.Content[j+1], v.Field(i))
				break
			}
		}
	}
}

func DecodeYAML(data []byte, v any) error {
	if err := storage.CheckSchemaVersion(data, SchemaVersion); err != nil {
		return err
	}
	return storage.DecodeYAML(data, v)
}

func EncodeJSON(v any) ([]byte, error) {
	b, err := storage.EncodeJSON(v)
	if err != nil {
		return nil, err
	}
	if err := storage.CheckSchemaVersion(b, SchemaVersion); err != nil {
		return nil, err
	}
	return b, nil
}

func DecodeJSON(data []byte, v any) error {
	if err := storage.CheckSchemaVersion(data, SchemaVersion); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("decode json: unexpected second document")
	}
	return nil
}
