// Package scmmanager is the Go port of com.cloudogu.gitops.tools.core.ScmManagerSetup
// (the tool-level wrapper) plus the bootstrap parts of
// infrastructure/git/providers/scmmanager/ScmManager.groovy.
//
// The HTTP client and the GitProvider abstraction itself are already ported
// in internal/scm/scmmanager and internal/scm; this package is purely the
// feature.Feature wrapper that the install runner schedules and that calls
// into the existing client to perform the post-deploy configuration.
//
// What is and isn't done here:
//
//   - IsEnabled returns true only when SCM-Manager runs internally. The
//     opaque cfg.Scm.Raw["scmManager"] map exposes a "url" field that is
//     empty for internal mode (matches ScmTenantSchema.scmManager.url
//     default and addScmConfig() in internal/config/configurator.go). When
//     the user points at an external SCMM URL, Install/Configure are
//     skipped entirely – matching ScmManager.init's `internal && installNeeded`
//     gate.
//
//   - Install renders the helm values (see values.go), deploys via the
//     wired-in deployment.Strategy, then calls Configure which
//     mirrors ScmManagerSetup.configure() (plugins → setup config →
//     jenkins plugin → users).
//
//   - The Configure loops respect ctx.Done(); the Groovy original busy-waited
//     via Thread.sleep with no cancellation surface.
//
//   - Validate is a soft schema check on cfg.Scm.Raw["scmManager"]: it makes
//     sure the bits the configurator should have filled in (username,
//     password, urlForJenkins) are non-empty when the feature is active.
package scmmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
	scmmclient "github.com/cloudogu/gitops-playground/go/internal/scm/scmmanager"
)

const (
	releaseName = "scmm"
	repoName    = "scm-manager"
	defaultNS   = "scm-manager"

	// defaultWaitTimeout matches ScmManagerSetup.waitForScmmAvailable's
	// default of 180 seconds.
	defaultWaitTimeout = 180 * time.Second
	// defaultWaitInterval matches the Groovy default of 5 s between polls.
	defaultWaitInterval = 5 * time.Second
)

// Feature implements feature.Feature for SCM-Manager.
//
// The struct mirrors the Groovy ScmManager constructor surface but only
// keeps the bits the Go port needs:
//
//   - Deploy installs the helm chart (Argo CD or imperative – the wiring is
//     decided by the deployment.Deployer the runner injects).
//   - Images creates the proxy-registry pull secret in the SCMM namespace
//     when cfg.Registry.CreateImagePullSecrets is true.
//   - Client is the already-ported SCM-Manager HTTP client used by
//     Configure (plugins, /v2/config, users).
//   - JenkinsActive returns true when the Jenkins feature is enabled. We
//     take a function (rather than read cfg.Jenkins directly) so the test
//     suite can decouple the two without constructing a full config tree.
type Feature struct {
	Deploy        deployment.Strategy
	Images        feature.ImagePullSecretCreator
	Client        *scmmclient.Client
	JenkinsActive func(*config.Config) bool

	// PollInterval is the interval between availability checks in
	// WaitForAvailable. Defaults to defaultWaitInterval (5s) when zero;
	// tests can shrink it so the test suite stays fast.
	PollInterval time.Duration
}

// Name implements feature.Feature.
func (Feature) Name() string { return "scm-manager" }

// Order matches the Groovy @Order(60) – SCM-Manager has to come up before
// Jenkins (70), ArgoCD and any feature that pushes git content.
func (Feature) Order() int { return 60 }

// IsEnabled implements feature.Feature. We delegate to isInternal so the
// gate is testable in isolation and so future external-SCMM modes only have
// to flip one helper.
func (Feature) IsEnabled(cfg *config.Config) bool {
	return isInternal(cfg)
}

// Namespace returns "<namePrefix>scm-manager". The Groovy code reads it
// from scm.scmManager.namespace, which defaults to "scm-manager" in
// ScmManagerTenantConfig; we materialise the same default here.
func (Feature) Namespace(cfg *config.Config) string {
	ns := cfg.Scm.ScmManager.Namespace
	if ns == "" {
		ns = defaultNS
	}
	return cfg.Application.NamePrefix + ns
}

// Disable is a no-op – SCM-Manager is left running so repository data
// survives a reconfigure. Destructive teardown is handled by the destroy
// command, not by this feature.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Validate checks the minimal subset of cfg.Scm.ScmManager fields the
// configurator should have filled in by the time the install pipeline
// runs. It is a fail-fast guard so we error out before we try to PUT
// /v2/config with empty credentials.
func (f Feature) Validate(_ context.Context, cfg *config.Config) error {
	if !isInternal(cfg) {
		return nil
	}
	scmm := cfg.Scm.ScmManager
	if scmm.Username == "" {
		return fmt.Errorf("scm-manager: scm.scmManager.username must not be empty")
	}
	if scmm.Password == "" {
		return fmt.Errorf("scm-manager: scm.scmManager.password must not be empty")
	}
	// urlForJenkins is required iff Jenkins is going to be configured.
	if f.JenkinsActive != nil && f.JenkinsActive(cfg) {
		if scmm.UrlForJenkins == "" {
			return fmt.Errorf("scm-manager: scm.scmManager.urlForJenkins must not be empty when jenkins is active")
		}
	}
	return nil
}

// Install installs (or upgrades) the SCM-Manager chart and then runs the
// post-deploy configuration via the wired-in API client. The ordering
// mirrors ScmManager.init(installNeeded=true) in the Groovy source:
// setupHelm → waitForScmmAvailable → configure.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := buildValues(cfg)
	inlineValues, _ := scmmHelmValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: inlineValues,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("scm-manager: render values: %w", err)
	}
	defer cleanup()

	chart, repoURL, version := scmmHelmChartCoordinates(cfg)
	if err := f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        repoURL,
		RepoName:       repoName,
		ChartOrPath:    chart,
		Version:        version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	}); err != nil {
		return fmt.Errorf("scm-manager: deploy chart: %w", err)
	}

	// Post-deploy configuration only runs when an API client is wired in.
	// The runner always wires one in production; tests opt out by leaving
	// it nil.
	if f.Client == nil {
		return nil
	}
	if err := f.WaitForAvailable(ctx, defaultWaitTimeout); err != nil {
		return fmt.Errorf("scm-manager: wait for available: %w", err)
	}
	if err := f.Configure(ctx, cfg); err != nil {
		return fmt.Errorf("scm-manager: configure: %w", err)
	}
	return nil
}

// isInternal reports whether SCM-Manager runs inside the cluster. With
// the typed schema, the gate collapses to "no URL = internal".
func isInternal(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Scm.ScmManager.URL == ""
}

// scmmHelmChartCoordinates returns the helm chart / repoURL / version
// the SCM-Manager feature should install. The typed schema's defaults
// (set by config.New) already carry the canonical values; an empty
// field falls back to the upstream-known constant.
func scmmHelmChartCoordinates(cfg *config.Config) (chart, repoURL, version string) {
	h := cfg.Scm.ScmManager.Helm
	chart = h.Chart
	repoURL = h.RepoURL
	version = h.Version
	if chart == "" {
		chart = "scm-manager"
	}
	if repoURL == "" {
		repoURL = "https://packages.scm-manager.org/repository/helm-v2-releases/"
	}
	if version == "" {
		version = "3.11.6"
	}
	return
}

// scmmHelmValues returns the user-supplied helm values block (or nil).
func scmmHelmValues(cfg *config.Config) (map[string]any, bool) {
	v := cfg.Scm.ScmManager.Helm.Values
	if len(v) == 0 {
		return nil, false
	}
	return v, true
}
