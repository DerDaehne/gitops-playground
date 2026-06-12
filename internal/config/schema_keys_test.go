package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestPrintSchemaKeys is a helper, not a check. Run with
//
//	go test -v -run TestPrintSchemaKeys ./internal/config/
//
// to dump every YAML key path the typed Config struct accepts. The
// caller diffs the output against retired/docs/configuration.schema.json
// to find drifts between Groovy and Go.
func TestPrintSchemaKeys(t *testing.T) {
	keys := schemaKeysOf(reflect.TypeOf(Config{}), "")
	sort.Strings(keys)
	for _, k := range keys {
		t.Log(k)
	}
}

// schemaKeysOf walks a struct via reflection and emits the YAML key
// path for each field. Anonymous fields with `yaml:",inline"` are
// flattened. Slices/maps are noted with [] / {}.
func schemaKeysOf(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		name, opts := splitTag(tag)
		if name == "-" {
			continue
		}
		switch {
		case f.Anonymous && contains(opts, "inline"):
			out = append(out, schemaKeysOf(f.Type, prefix)...)
			continue
		case name == "":
			name = strings.ToLower(f.Name[:1]) + f.Name[1:]
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}

		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Slice:
			out = append(out, full)
			elem := ft.Elem()
			for elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			if elem.Kind() == reflect.Struct {
				out = append(out, schemaKeysOf(elem, full+"[]")...)
			}
		case reflect.Map:
			out = append(out, full)
		case reflect.Struct:
			out = append(out, full)
			out = append(out, schemaKeysOf(ft, full)...)
		default:
			out = append(out, full)
		}
	}
	return out
}

func splitTag(tag string) (string, []string) {
	parts := strings.Split(tag, ",")
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
