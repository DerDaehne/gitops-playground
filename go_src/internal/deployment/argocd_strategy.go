package deployment

import (
	"context"
	"fmt"
)

// ArgoCDStrategy creates an Argo CD Application manifest pointing at the
// chart, then commits and pushes it. Fully implemented alongside the
// ArgoCD feature port. Until then this stub returns an error so
// misconfigured pipelines fail loudly.
//
// The full counterpart of
// infrastructure/deployment/ArgoCdApplicationStrategy.groovy is wired up
// in the ArgoCD feature sub-agent (phase 4b).
type ArgoCDStrategy struct {
	// Push is set by the ArgoCD feature once it has access to the
	// cluster-resources git repo. When nil, Deploy returns an error.
	Push func(ctx context.Context, s Spec) error
}

// Deploy implements Strategy.
func (a ArgoCDStrategy) Deploy(ctx context.Context, s Spec) error {
	if a.Push == nil {
		return fmt.Errorf("argocd strategy: not initialised (ArgoCD feature did not register a Push function)")
	}
	return a.Push(ctx, s)
}
