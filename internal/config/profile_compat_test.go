package config

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/profile"

	"gopkg.in/yaml.v3"
)

// TestProfilesStrictDecode ensures every embedded Groovy-era profile
// (the application-*.yaml files under internal/profile/profiles/) maps
// into the typed Go Config without unknown keys. The Groovy CLI accepts
// these files verbatim; if a key drifts, the rewrite silently drops it.
// A failure here means YAML compatibility is broken.
func TestProfilesStrictDecode(t *testing.T) {
	for _, name := range profile.List() {
		t.Run(name, func(t *testing.T) {
			raw, err := profile.Load(name)
			if err != nil {
				t.Fatalf("profile.Load(%q): %v", name, err)
			}

			cfg := New()
			dec := yaml.NewDecoder(bytes.NewReader(raw))
			dec.KnownFields(true)
			if err := dec.Decode(cfg); err != nil {
				t.Errorf("STRICT yaml decode failed for profile %q (file %s.yaml):\n  %v",
					name, filepath.Join("internal/profile/profiles/application-"+name), err)
			}
		})
	}
}
