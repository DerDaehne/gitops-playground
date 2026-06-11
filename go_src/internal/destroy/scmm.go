package destroy

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/scm/scmmanager"
)

// ScmmHandler removes the SCM-Manager repositories and the technical user
// that GOP created. Mirrors destroy/ScmmDestructionHandler.groovy.
type ScmmHandler struct {
	Client *scmmanager.Client
}

// Name implements Handler.
func (ScmmHandler) Name() string { return "scm-manager" }

// Order matches @Order(200).
func (ScmmHandler) Order() int { return 200 }

// Destroy implements Handler.
func (h ScmmHandler) Destroy(ctx context.Context, cfg *config.Config) error {
	if h.Client == nil {
		return fmt.Errorf("scmm destroy: client is nil")
	}
	prefix := cfg.Application.NamePrefix

	if err := h.Client.DeleteUser(ctx, prefix+"gitops"); err != nil {
		return err
	}

	prefixed := []string{"argocd", "cluster-resources", "example-apps"}
	for _, ns := range prefixed {
		for _, name := range repoNamesFor(ns) {
			if err := h.Client.DeleteRepository(ctx, prefix+ns, name); err != nil {
				return err
			}
		}
	}

	// 3rd-party-dependencies stays un-prefixed (Groovy passes
	// `prefixNamespace=false`).
	for _, name := range []string{
		"ces-build-lib",
		"gitops-build-lib",
		"spring-boot-helm-chart",
		"spring-boot-helm-chart-with-dependency",
	} {
		if err := h.Client.DeleteRepository(ctx, "3rd-party-dependencies", name); err != nil {
			return err
		}
	}
	return nil
}

func repoNamesFor(ns string) []string {
	switch ns {
	case "argocd":
		return []string{"argocd"}
	case "cluster-resources":
		return []string{"cluster-resources"}
	case "example-apps":
		return []string{"example-apps"}
	default:
		return []string{ns}
	}
}
