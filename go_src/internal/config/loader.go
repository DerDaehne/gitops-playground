package config

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudogu/gitops-playground/go/internal/profile"

	"gopkg.in/yaml.v3"
)

// ConfigMapReader fetches a key from a Kubernetes ConfigMap. The CLI calls
// this via the K8s adapter; tests pass a stub.
type ConfigMapReader interface {
	GetConfigMap(ctx context.Context, name, key string) (string, error)
}

// LoadOptions controls the merge sequence. It corresponds to the args
// readConfigs() works with in the Groovy GitopsPlaygroundCli.
type LoadOptions struct {
	// ProfileName is the value of --profile.
	ProfileName string
	// ConfigFiles are paths from --config-file.
	ConfigFiles []string
	// ConfigMaps are names from --config-map (as expected by k8s).
	ConfigMaps []string
	// K8s reads ConfigMaps. May be nil when no --config-map is requested.
	K8s ConfigMapReader
}

// Load builds the merged configuration following the precedence
// documented in README:
//
//  1. defaults (config.New)
//  2. profile YAML (if --profile is set)
//  3. each --config-map in order
//  4. each --config-file in order
//
// CLI flags are NOT applied here – the cli package owns that step because
// it has the parsed pflag values. The configurator stage (see Initialise)
// adds derived/internal values on top.
func Load(ctx context.Context, opts LoadOptions) (*Config, error) {
	cfg := New()

	if opts.ProfileName != "" {
		data, err := profile.Load(opts.ProfileName)
		if err != nil {
			return nil, err
		}
		if err := mergeYAML(cfg, data, fmt.Sprintf("profile %q", opts.ProfileName)); err != nil {
			return nil, err
		}
	}

	for _, name := range opts.ConfigMaps {
		if opts.K8s == nil {
			return nil, fmt.Errorf("--config-map %q requires a Kubernetes connection", name)
		}
		raw, err := opts.K8s.GetConfigMap(ctx, name, "config.yaml")
		if err != nil {
			return nil, fmt.Errorf("reading config map %q: %w", name, err)
		}
		if err := mergeYAML(cfg, []byte(raw), fmt.Sprintf("config map %q", name)); err != nil {
			return nil, err
		}
	}

	for _, path := range opts.ConfigFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}
		if err := mergeYAML(cfg, data, fmt.Sprintf("config file %q", path)); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// mergeYAML unmarshals raw YAML into the existing Config in place. Unlike
// MapUtils.deepMerge in the Groovy code we let yaml.v3 do the work – it
// already preserves fields that are not mentioned in the input and merges
// nested structs. Slices and maps are replaced (same semantics as the
// Groovy version, which replaces them too because Lists/Maps don't trigger
// the nested recursion).
//
// We do NOT collapse nil-input to "keep default" automatically: yaml.v3
// already leaves untouched fields alone. The only Groovy behaviour we
// don't replicate is "explicit null overwrites default" – yaml.v3 with a
// pointer-free struct treats an explicit `key: null` as the zero value,
// which is what Groovy's deepMergeDefaults() avoids. We accept that minor
// divergence in favour of code that is easier to reason about; the profile
// YAMLs in the repo never use explicit nulls.
func mergeYAML(cfg *Config, data []byte, source string) error {
	if len(data) == 0 {
		return nil
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing %s: %w", source, err)
	}
	return nil
}
