package domain

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

// Populate every field, including future additions: dropping any serialized
// field must fail the persisted-record roundtrip contract.
func fullValue(v reflect.Value) {
	if v.Type() == reflect.TypeOf(time.Time{}) {
		v.Set(reflect.ValueOf(time.Date(2026, 9, 17, 12, 30, 0, 123, time.UTC)))
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fullValue(v.Field(i))
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fullValue(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fullValue(v.Index(0))
	case reflect.String:
		v.SetString("记录<&>\nvalue")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int:
		v.SetInt(1)
	}
}

func TestRecordsRoundTrip(t *testing.T) {
	records := []any{Project{}, WorkItem{}, Run{}, Decision{}, Finding{}, Event{}, Artifact{}, Approval{}, CurrentStateFile{}, MilestonesFile{}}
	formats := []struct {
		name   string
		encode func(any) ([]byte, error)
		decode func([]byte, any) error
	}{
		{"yaml", EncodeYAML, DecodeYAML}, {"json", EncodeJSON, DecodeJSON},
	}
	for _, record := range records {
		for _, format := range formats {
			t.Run(reflect.TypeOf(record).Name()+"/"+format.name, func(t *testing.T) {
				original := reflect.New(reflect.TypeOf(record))
				fullValue(original.Elem())
				first, err := format.encode(original.Interface())
				if err != nil {
					t.Fatal(err)
				}
				second, err := format.encode(original.Interface())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first, second) {
					t.Fatal("identical record changed bytes")
				}
				restored := reflect.New(original.Elem().Type())
				if err := format.decode(first, restored.Interface()); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(original.Interface(), restored.Interface()) {
					t.Fatalf("record fields lost:\nwant %#v\ngot %#v", original.Elem().Interface(), restored.Elem().Interface())
				}
				again, err := format.encode(restored.Interface())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first, again) {
					t.Fatal("roundtrip changed canonical bytes")
				}
			})
		}
	}
}

func TestNullableCollectionsAndVerification(t *testing.T) {
	no := false
	for _, encode := range []func(any) ([]byte, error){EncodeYAML, EncodeJSON} {
		for _, advanced := range []*bool{nil, &no} {
			original := Run{SchemaVersion: 1, Verification: Verification{Advanced: advanced}, Commands: []string{}}
			b, err := encode(original)
			if err != nil {
				t.Fatal(err)
			}
			var restored Run
			// JSON is also valid YAML; validate values independently of JSON syntax.
			if err := DecodeYAML(b, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, restored) {
				t.Fatalf("null, false or empty lost: %s", b)
			}
		}
	}
}

func TestRejectUnknownSchemaAndFields(t *testing.T) {
	for _, input := range []string{"schema_version: 2\nid: x\n", "id: x\n", "schema_version: 1\nunknown: x\n", "schema_version: 1\n---\nschema_version: 1\n"} {
		var got WorkItem
		if err := DecodeYAML([]byte(input), &got); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, input := range []string{`{"schema_version":2}`, `{"id":"x"}`, `{"schema_version":1,"unknown":true}`, `{"schema_version":1} {"schema_version":1}`} {
		var got WorkItem
		if err := DecodeJSON([]byte(input), &got); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
