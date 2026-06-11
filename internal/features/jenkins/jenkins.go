// Package jenkins is the Go port of com.cloudogu.gitops.tools.core.Jenkins.
//
// The Groovy original both installs the Jenkins helm chart and configures
// the resulting controller via REST/scriptText calls. The Go port keeps
// that split visible:
//
//   - When cfg.Jenkins.Internal (i.e. cfg.Jenkins.URL is empty) the feature
//     ensures a pull secret, renders the helm values programmatically and
//     delegates the chart install to the deployment.Strategy. After the
//     install it talks to Jenkins via internal/jenkins.Client to push the
//     global properties, ensure the metrics user and toggle the prometheus
//     endpoint.
//   - When cfg.Jenkins.URL is set (external Jenkins) the feature skips the
//     helm install entirely and only runs the configuration step. The
//     namespace returned by Namespace() is empty in that case so the runner
//     does not allocate a dedicated namespace.
//
// The HTTP client is built by the runner via the API factory, not here:
// the feature stays decoupled from internal/httpx so it can be exercised
// with a stub *jenkins.Client in tests.
package jenkins

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
	jenkinsapi "github.com/cloudogu/gitops-playground/go/internal/jenkins"
)

const (
	releaseName  = "jenkins"
	repoName     = "jenkins"
	namespaceTag = "jenkins"
)

// Feature implements feature.Feature for the Jenkins controller.
//
// API is a factory rather than an already-built *jenkins.Client because
// the URL/auth become known only after the chart is installed (the
// Groovy code rewrites config.jenkins.url to the NodePort/Service after
// the wait-for-NodePort step). The runner supplies the factory; tests
// pass a closure returning a stub client.
type Feature struct {
	Deploy deployment.Strategy
	API    func(cfg *config.Config) (*jenkinsapi.Client, error)
	Images feature.ImagePullSecretCreator

	// DockerGid is the GID of the `docker` group on the worker node, used
	// to run the agent under the right group so it can talk to the host
	// docker socket. The runner discovers it once (Jenkins.groovy
	// findDockerGid) before calling Install; an empty value falls back to
	// root + GID 133, matching the .ftl branch.
	DockerGid string
}

// Name implements feature.Feature.
func (Feature) Name() string { return "jenkins" }

// Order matches the Groovy @Order(70) — but the Go runner uses a higher
// number to keep Jenkins after the SCM is up, mirroring the existing port
// of the monitoring/ingress features. 90 places it between monitoring
// (80) and ingress (150).
func (Feature) Order() int { return 90 }

// IsEnabled implements feature.Feature.
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Jenkins.Active }

// Namespace returns "<namePrefix>jenkins" when internal, "" otherwise.
// The Groovy Jenkins constructor only sets `this.namespace` when
// config.jenkins.internal — the feature has nothing to own in
// the external-jenkins case.
func (Feature) Namespace(cfg *config.Config) string {
	if !isInternal(cfg) {
		return ""
	}
	return cfg.Application.NamePrefix + namespaceTag
}

// Disable is a no-op: the Groovy port has no destructive teardown either,
// destruction is handled by the runner-level JenkinsDestructionHandler.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Validate mirrors the implicit validation in Jenkins.groovy + the
// "Mandatory when jenkins-url is set" guards on the CLI options: when the
// user pins an external Jenkins URL they must also supply credentials.
// Returns nil for the internal case (the configurator fills in defaults).
func (Feature) Validate(_ context.Context, cfg *config.Config) error {
	if cfg.Jenkins.URL == "" {
		return nil
	}
	var missing []string
	if cfg.Jenkins.Username == "" {
		missing = append(missing, "jenkins.username")
	}
	if cfg.Jenkins.Password == "" {
		missing = append(missing, "jenkins.password")
	}
	if cfg.Features.Monitoring.Active {
		if cfg.Jenkins.MetricsUsername == "" {
			missing = append(missing, "jenkins.metricsUsername")
		}
		if cfg.Jenkins.MetricsPassword == "" {
			missing = append(missing, "jenkins.metricsPassword")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("jenkins: external jenkins URL %q requires %v", cfg.Jenkins.URL, missing)
}

// Install runs the install + post-install sequence. For an internal
// Jenkins the order is:
//
//  1. ensure proxy-registry pull secret in the jenkins namespace,
//  2. render helm values via buildValues,
//  3. helm-deploy the chart through the configured Strategy,
//  4. run the post-install API calls (global properties, metrics user,
//     prometheus, optional restart).
//
// For an external Jenkins steps 1-3 are skipped; step 4 still runs so the
// global properties and metrics user are kept in sync with config.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	if isInternal(cfg) {
		if err := f.installInternal(ctx, cfg); err != nil {
			return err
		}
	}
	return f.configureAPI(ctx, cfg)
}

func (f Feature) installInternal(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := buildValues(cfg, f.DockerGid)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Jenkins.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("jenkins: render values: %w", err)
	}
	defer cleanup()

	if f.Deploy == nil {
		return errors.New("jenkins: Deploy strategy is required for internal jenkins")
	}
	if err := f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Jenkins.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Jenkins.Helm.Chart,
		Version:        cfg.Jenkins.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	}); err != nil {
		return fmt.Errorf("jenkins: helm deploy: %w", err)
	}
	return nil
}

// configureAPI is the Groovy enable() tail end: it pushes the global env
// variables, ensures the metrics user, toggles the authenticated
// prometheus endpoint and, unless SkipRestart is set, asks Jenkins to
// safeRestart so the new env values take effect.
//
// The factory may return a nil client without an error to mean "API
// configuration is not desired in this run" (used by the destroy path).
func (f Feature) configureAPI(ctx context.Context, cfg *config.Config) error {
	if f.API == nil {
		return nil
	}
	client, err := f.API(cfg)
	if err != nil {
		return fmt.Errorf("jenkins: build api client: %w", err)
	}
	if client == nil {
		return nil
	}

	if err := setGlobalProperties(ctx, client, cfg); err != nil {
		return err
	}

	if cfg.Features.Monitoring.Active && isInternal(cfg) {
		// An external Jenkins is typically managed outside the playground
		// so we don't flip its prometheus switch — matches the Groovy
		// `config.jenkins.internal` guard around enableAuthentication().
		if err := client.ConfigurePrometheus(ctx, jenkinsapi.PrometheusConfig{
			MetricsUsername: cfg.Jenkins.MetricsUsername,
			MetricsPassword: cfg.Jenkins.MetricsPassword,
		}); err != nil {
			return fmt.Errorf("jenkins: configure prometheus: %w", err)
		}
	} else if cfg.Jenkins.MetricsUsername != "" {
		// External jenkins / monitoring disabled: still ensure the user
		// has the metrics-view permission, mirroring the Groovy code
		// which calls grantPermission unconditionally.
		if err := client.GrantMetricsView(ctx, cfg.Jenkins.MetricsUsername); err != nil {
			return fmt.Errorf("jenkins: grant metrics view: %w", err)
		}
	}

	if isInternal(cfg) && !cfg.Jenkins.SkipRestart {
		if err := client.RestartSafely(ctx); err != nil {
			return fmt.Errorf("jenkins: restart: %w", err)
		}
	}
	return nil
}

// setGlobalProperties pushes the env-var node properties that mirror the
// Groovy enable() block. Returns the first error so the caller can stop
// before triggering a restart on an inconsistent controller.
func setGlobalProperties(ctx context.Context, client *jenkinsapi.Client, cfg *config.Config) error {
	prefix := cfg.Application.NamePrefixForEnvVars

	props := []struct{ key, val string }{
		{prefix + "K8S_VERSION", config.K8sVersion},
	}

	if cfg.Registry.URL != "" {
		props = append(props, struct{ key, val string }{prefix + "REGISTRY_URL", cfg.Registry.URL})
	}
	if cfg.Registry.Path != "" {
		props = append(props, struct{ key, val string }{prefix + "REGISTRY_PATH", cfg.Registry.Path})
	}
	if cfg.Registry.TwoRegistries {
		props = append(props,
			struct{ key, val string }{prefix + "REGISTRY_PROXY_URL", cfg.Registry.ProxyURL},
			struct{ key, val string }{prefix + "REGISTRY_PROXY_PATH", cfg.Registry.ProxyPath},
		)
	}
	if cfg.Jenkins.MavenCentralMirror != "" {
		props = append(props, struct{ key, val string }{prefix + "MAVEN_CENTRAL_MIRROR", cfg.Jenkins.MavenCentralMirror})
	}

	// AdditionalEnvs is keyed by the operator: we forward as-is without
	// prefixing (matches Jenkins.groovy line 127-130).
	for k, v := range cfg.Jenkins.AdditionalEnvs {
		props = append(props, struct{ key, val string }{k, v})
	}

	for _, p := range props {
		if err := client.SetGlobalProperty(ctx, p.key, p.val); err != nil {
			return fmt.Errorf("jenkins: set %s: %w", p.key, err)
		}
	}
	return nil
}

// isInternal reports whether the controller is hosted by the playground.
// External mode is signalled by a non-empty URL in the configurator —
// see ApplicationConfigurator.addJenkinsConfig.
func isInternal(cfg *config.Config) bool {
	return cfg.Jenkins.URL == ""
}
