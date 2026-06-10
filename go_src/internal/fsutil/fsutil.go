// Package fsutil contains the small set of file helpers used by features.
//
// The original FileSystemUtils.groovy carries many helpers that turned out
// to be test-only (getLineFromFile, getSubstringOfFile, …). They are not
// ported – features in this code base do not need them, and external
// callers can use the stdlib.
package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CopyDir copies src into dst recursively. dst is created if missing.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// ReadYAML reads `path` and decodes it into `into` (typically a *map or
// *struct).
func ReadYAML(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("yaml decode %s: %w", path, err)
	}
	return nil
}

// WriteYAML serialises data to YAML and writes it to path. Existing files
// are overwritten.
func WriteYAML(path string, data any) error {
	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// WriteTempYAML writes `data` to a temporary file and returns its path
// alongside a cleanup function. The cleanup function is safe to call
// multiple times.
func WriteTempYAML(data any) (string, func(), error) {
	f, err := os.CreateTemp("", "gop-*.yaml")
	if err != nil {
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	if err := WriteYAML(f.Name(), data); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(f.Name())
	}
	return f.Name(), cleanup, nil
}

// ReplaceInFile reads `path`, substitutes every occurrence of `from`
// with `to`, and writes the result back. Returns an error if `from` does
// not appear in the file (the Groovy original silently no-ops, which
// hides bugs).
func ReplaceInFile(path, from, to string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(raw)
	if !strings.Contains(content, from) {
		return fmt.Errorf("string %q not found in %s", from, path)
	}
	return os.WriteFile(path, []byte(strings.ReplaceAll(content, from, to)), 0o644)
}
