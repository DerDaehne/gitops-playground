// Package template renders Go text/template files for GOP manifests.
//
// The Groovy original uses Freemarker via TemplatingEngine.groovy. We
// switch to Go's text/template – with two consequences:
//
//   - Existing .ftl templates will be ported to .tmpl in Phase 4 alongside
//     their feature implementations. Until that happens this package is
//     feature-complete but only operates on the new format.
//   - Templates that produce empty output are NOT written out (matches
//     Groovy's replaceTemplate behaviour, which silently drops the file).
package template

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Funcs returns the function map shared by every template render.
// Extra entries are merged on top with FuncMap.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"nullToEmpty": func(v any) string {
			if v == nil {
				return ""
			}
			return fmt.Sprint(v)
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
	}
}

// RenderString renders body using data. Useful for tiny inline templates
// (e.g. a single URL or annotation).
func RenderString(name, body string, data any) (string, error) {
	t, err := template.New(name).Funcs(Funcs()).Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute %s: %w", name, err)
	}
	return buf.String(), nil
}

// RenderFile reads a single template from disk and renders it.
func RenderFile(path string, data any) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return RenderString(filepath.Base(path), string(raw), data)
}

// RenderOptions controls RenderTree.
type RenderOptions struct {
	// Suffix marks template files. Files ending in Suffix get rendered;
	// the suffix is stripped from the destination path. Defaults to
	// ".tmpl" when empty.
	Suffix string
	// Skip is called for every source path; returning true skips that
	// entry entirely.
	Skip func(path string) bool
}

// RenderTree walks srcDir, rendering every template file into dstDir
// preserving the directory layout. Non-template files are copied
// verbatim. Empty render output drops the destination file.
func RenderTree(srcDir, dstDir string, data any, opts RenderOptions) error {
	suffix := opts.Suffix
	if suffix == "" {
		suffix = ".tmpl"
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if opts.Skip != nil && opts.Skip(path) {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if strings.HasSuffix(rel, suffix) {
			dst = strings.TrimSuffix(dst, suffix)
			out, err := RenderFile(path, data)
			if err != nil {
				return err
			}
			if strings.TrimSpace(out) == "" {
				return nil
			}
			return os.WriteFile(dst, []byte(out), 0o644)
		}
		// Plain copy
		buf, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, buf, 0o644)
	})
}
