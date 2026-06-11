// Package registry is the Go port of com.cloudogu.gitops.tools.Registry.
//
// The Groovy original deploys the internal docker registry imperatively via
// Helm even when ArgoCD is active – the constructor takes a HelmStrategy
// (not a Deployer) precisely to avoid an ordering problem at bootstrap. We
// preserve that intent here: Feature.Helm is a deployment.HelmStrategy,
// not the generic deployment.Strategy interface.
//
// The feature is only active when the registry is internal. When the user
// points the playground at an external registry (cfg.Registry.URL != "" and
// cfg.Registry.Internal == false), IsEnabled returns false and Install is
// never invoked.
package registry

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
)

const (
	releaseName = "registry"
	repoName    = "docker-registry"
	chartName   = "docker-registry"
	// containerPort mirrors Registry.CONTAINER_PORT – the registry's listen
	// port inside the pod. It is paired with the user-configured nodePort
	// when an additional NodePort service is needed.
	containerPort = "5000"
	// extraServiceName mirrors the Groovy literal used in
	// k8sClient.createServiceNodePort(...).
	extraServiceName = "docker-registry-internal-port"
)

// NodePortCreator is the minimal slice of the K8s client this feature
// needs. The runner wires in internal/k8s.Client; tests use a stub. When
// nil, the additional NodePort step is skipped – matching the documented
// behaviour for environments that do not need the extra port mapping.
//
// The tcp argument follows the Groovy convention "<port>:<targetPort>";
// nodePort is passed as a string so the empty case ("no nodePort") stays
// expressible.
type NodePortCreator interface {
	CreateServiceNodePort(ctx context.Context, name, tcp, nodePort, namespace string) error
}

// Feature implements feature.Feature for the internal docker registry.
//
// Helm is a concrete HelmStrategy (not the Strategy interface) on purpose:
// the Groovy constructor takes HelmStrategy directly because the registry
// must be installed imperatively even when ArgoCD is active.
type Feature struct {
	Helm     deployment.HelmStrategy
	NodePort NodePortCreator
}

// Name implements feature.Feature.
func (Feature) Name() string { return "registry" }

// Order matches the Groovy @Order(40).
func (Feature) Order() int { return 40 }

// IsEnabled implements feature.Feature.
//
// The Groovy isEnabled() returns config.registry.active, but the enable()
// body short-circuits on !config.registry.internal. We fold both checks
// into IsEnabled so the runner skips the feature entirely when the user
// configured an external registry.
func (Feature) IsEnabled(cfg *config.Config) bool {
	return cfg.Registry.Active && cfg.Registry.Internal
}

// Namespace returns "<namePrefix>registry" when internal, else "".
func (Feature) Namespace(cfg *config.Config) string {
	if !cfg.Registry.Internal {
		return ""
	}
	return cfg.Application.NamePrefix + "registry"
}

// Disable is a no-op; the registry is install-only.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs the docker-registry chart with the service block fixed
// to NodePort/DEFAULT_REGISTRY_PORT, then – if the user picked a different
// internal port – creates an additional NodePort service so the kubelet
// inside the k3d server container can still reach the registry on port
// 30000 (see the comment on createServiceNodePort in Registry.groovy).
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	values := buildValues()
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Registry.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("registry: render values: %w", err)
	}
	defer cleanup()

	if err := f.Helm.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Registry.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Registry.Helm.Chart,
		Version:        cfg.Registry.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	}); err != nil {
		return fmt.Errorf("registry: deploy chart: %w", err)
	}

	if cfg.Registry.InternalPort != config.DefaultRegistryPort {
		if f.NodePort == nil {
			// No K8s client wired in (e.g. unit test). Skip silently –
			// the runner is responsible for wiring this when it matters.
			return nil
		}
		tcp := containerPort + ":" + containerPort
		port := strconv.Itoa(cfg.Registry.InternalPort)
		if err := f.NodePort.CreateServiceNodePort(ctx, extraServiceName, tcp, port, ns); err != nil {
			return fmt.Errorf("registry: create additional node port: %w", err)
		}
	}
	return nil
}

// buildValues is the Go counterpart of the addHelmValuesData("service", …)
// call in Registry.groovy. The registry chart needs both nodePort and the
// service type so the playground can reach it from outside the cluster.
func buildValues() map[string]any {
	return map[string]any{
		"service": map[string]any{
			"nodePort": config.DefaultRegistryPort,
			"type":     "NodePort",
		},
	}
}
