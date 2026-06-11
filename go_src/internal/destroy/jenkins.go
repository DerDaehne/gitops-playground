package destroy

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/jenkins"
)

// JenkinsHandler removes the example pipeline job and the global Jenkins
// properties GOP set up. Mirrors destroy/JenkinsDestructionHandler.groovy.
type JenkinsHandler struct {
	Client *jenkins.Client
}

// Name implements Handler.
func (JenkinsHandler) Name() string { return "jenkins" }

// Order matches @Order(300).
func (JenkinsHandler) Order() int { return 300 }

// Destroy implements Handler.
func (h JenkinsHandler) Destroy(ctx context.Context, cfg *config.Config) error {
	if h.Client == nil {
		return fmt.Errorf("jenkins destroy: client is nil")
	}
	prefix := cfg.Application.NamePrefix
	envPrefix := cfg.Application.NamePrefixForEnvVars

	if err := h.Client.DeleteJob(ctx, prefix+"example-apps"); err != nil {
		return err
	}

	for _, key := range []string{
		"SCMM_URL",
		envPrefix + "REGISTRY_URL",
		envPrefix + "REGISTRY_PATH",
		envPrefix + "REGISTRY_PROXY_URL",
		envPrefix + "REGISTRY_PROXY_PATH",
		envPrefix + "K8S_VERSION",
	} {
		if err := h.Client.DeleteGlobalProperty(ctx, key); err != nil {
			return fmt.Errorf("delete global property %q: %w", key, err)
		}
	}
	return nil
}
