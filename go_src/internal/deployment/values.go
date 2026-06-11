package deployment

import (
	"fmt"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/fsutil"
	"github.com/cloudogu/gitops-playground/go/internal/template"

	"gopkg.in/yaml.v3"
)

// HelmValuesRequest mirrors what Tool.deployHelmChart does before handing
// off to the strategy: render the template, merge inline values and write
// a temporary values.yaml.
type HelmValuesRequest struct {
	// TemplatePath is either a Go text/template (with .tmpl suffix) or a
	// plain YAML file path. Empty means "use InlineValues only".
	TemplatePath string
	// TemplateData is passed verbatim to text/template.
	TemplateData any
	// InlineValues from the user (e.g. config.features.<name>.helm.values).
	// Merged on top of the rendered template (inline wins).
	InlineValues map[string]any
	// ExtraValues is the equivalent of Tool.helmValuesTemplateData – data
	// that features want to inject programmatically. Merged on top of the
	// rendered template, but UNDER InlineValues.
	ExtraValues map[string]any
}

// RenderHelmValues renders the request to a temp file and returns its path
// plus a cleanup function. Callers should defer cleanup.
func RenderHelmValues(cfg *config.Config, req HelmValuesRequest) (string, func(), error) {
	rendered := map[string]any{}

	if req.TemplatePath != "" {
		switch {
		case strings.HasSuffix(req.TemplatePath, ".tmpl") ||
			strings.HasSuffix(req.TemplatePath, ".tmpl.yaml") ||
			strings.HasSuffix(req.TemplatePath, ".yaml.tmpl"):
			data := req.TemplateData
			if data == nil {
				data = map[string]any{"config": cfg}
			}
			body, err := template.RenderFile(req.TemplatePath, data)
			if err != nil {
				return "", nil, fmt.Errorf("render values template: %w", err)
			}
			if strings.TrimSpace(body) != "" {
				if err := yaml.Unmarshal([]byte(body), &rendered); err != nil {
					return "", nil, fmt.Errorf("parse rendered values: %w", err)
				}
			}
		default:
			if err := fsutil.ReadYAML(req.TemplatePath, &rendered); err != nil {
				return "", nil, fmt.Errorf("read values yaml: %w", err)
			}
		}
	}

	merged := deepMerge(rendered, req.ExtraValues)
	merged = deepMerge(merged, req.InlineValues)

	return fsutil.WriteTempYAML(merged)
}

// deepMerge merges b into a, returning a new map. Matches MapUtils.deepMerge
// semantics: nested maps recurse, anything else is overwritten.
func deepMerge(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if existing, ok := out[k]; ok {
			if em, eok := existing.(map[string]any); eok {
				if vm, vok := v.(map[string]any); vok {
					out[k] = deepMerge(em, vm)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}
