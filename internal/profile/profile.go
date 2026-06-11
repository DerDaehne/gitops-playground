// Package profile provides access to the predefined GOP profiles that
// shipped as application-*.yaml resources in the Groovy version.
//
// They are embedded into the binary via go:embed so the CLI works without
// any external classpath.
package profile

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed profiles/application-*.yaml
var profileFS embed.FS

const (
	dir    = "profiles"
	prefix = "application-"
	suffix = ".yaml"
)

// List returns the names of every embedded profile (without the
// `application-` prefix or `.yaml` suffix), sorted alphabetically.
func List() []string {
	entries, err := fs.ReadDir(profileFS, dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix))
	}
	sort.Strings(names)
	return names
}

// Load returns the raw YAML content for a profile. Mirrors the
// classpath-based loader from GitopsPlaygroundCli.extractProfile.
func Load(name string) ([]byte, error) {
	if name == "" {
		return nil, nil
	}
	path := dir + "/" + prefix + name + suffix
	data, err := profileFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %q does not exist (resource %q not found): %w", name, path, err)
	}
	return data, nil
}
