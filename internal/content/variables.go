package content

import (
	"log/slog"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// scmInfo is the subset of SCM provider data exposed to templates. Mirrors
// the "scm" map built in ContentLoader.applyTemplatingIfApplicable.
type scmInfo struct {
	BaseURL  string `yaml:"baseUrl"`
	Host     string `yaml:"host"`
	Protocol string `yaml:"protocol"`
	RepoURL  string `yaml:"repoUrl"`
}

// Variables is the data map handed to the templating engine for a single
// content repo. It exposes:
//
//   - "config"  – the full GOP config (read-only from the template).
//   - "scm"     – the scmInfo above, derived from the tenant SCM provider.
//   - "vars"    – cfg.Content.Variables, merged with the optional per-call
//     overrides (overrides win on key collision).
//   - "statics" – a placeholder for the FreeMarker static-class injection.
//     Empty in the Go port; templates that rely on Java statics will need
//     to be ported to use template functions instead.
type Variables struct {
	Config  *config.Config
	Scm     scmInfo
	Vars    map[string]any
	Statics map[string]any
}

// BuildVariables assembles a Variables value for the given repo target.
// When cfg.Content.UseWhitelist is true the Vars map is restricted to the
// keys listed in cfg.Content.AllowedStaticsWhitelist; any key outside the
// whitelist is dropped and logged at WARN level. This is a deliberately
// limited stand-in for the Freemarker AllowListObjectWrapper – it enforces
// the *key whitelist* on user data, not a full sandbox of method calls on
// the way to the template (Go's text/template has no equivalent escape
// hatch, so a per-method sandbox would not buy us much).
func BuildVariables(cfg *config.Config, provider scm.Provider, target string, overrides map[string]any) Variables {
	v := Variables{
		Config:  cfg,
		Vars:    mergeMaps(cfg.Content.Variables, overrides),
		Statics: map[string]any{},
	}

	if provider != nil {
		namespace, name, err := scm.SplitRepoTarget(target)
		if err == nil {
			v.Scm = scmInfo{
				BaseURL:  provider.RepoURL(namespace, name, scm.RepoURLInCluster),
				Host:     scm.ParseHost(provider.RepoURL(namespace, name, scm.RepoURLInCluster)),
				Protocol: scm.ParseScheme(provider.RepoURL(namespace, name, scm.RepoURLInCluster)),
				RepoURL:  provider.RepoURL(namespace, name, scm.RepoURLInCluster),
			}
		}
	}

	if cfg.Content.UseWhitelist {
		v.Vars = applyWhitelist(v.Vars, cfg.Content.AllowedStaticsWhitelist)
	}
	return v
}

// AsMap renders Variables into the map[string]any shape expected by Go's
// text/template engine. Field names are lower-camelCase to match the Groovy
// template variables (config, scm, vars, statics).
func (v Variables) AsMap() map[string]any {
	return map[string]any{
		"config":  v.Config,
		"scm":     v.Scm,
		"vars":    v.Vars,
		"statics": v.Statics,
	}
}

// applyWhitelist returns a copy of in containing only entries whose key
// appears in allowed. Removed keys are logged at WARN so users can see
// which Variables were dropped.
func applyWhitelist(in map[string]any, allowed []string) map[string]any {
	if len(in) == 0 {
		return in
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		allow[k] = struct{}{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if _, ok := allow[k]; ok {
			out[k] = v
			continue
		}
		slog.Warn("content: whitelist active – dropping variable",
			"feature", FeatureName,
			"key", k,
		)
	}
	return out
}

// mergeMaps returns a new map with entries from a overridden by entries in b.
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
