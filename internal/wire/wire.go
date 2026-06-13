// Package wire assembles concrete component instances and registers every
// feature with the runner. It is the Go counterpart of the Micronaut DI
// graph the Groovy code relies on – but explicit, traceable and tested.
//
// Why a hand-written wiring instead of a DI framework: with eleven
// features and roughly that many adapters the graph still fits on one
// screen. Reading and grepping the wiring stays trivial; there are no
// reflection-driven surprises at startup, no late binding failures and
// no annotation-based ordering. The Groovy original spent ~10% of its
// startup measuring Micronaut's bean discovery, which we avoid entirely.
package wire

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/destroy"
	"github.com/cloudogu/gitops-playground/go/internal/exec"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
	"github.com/cloudogu/gitops-playground/go/internal/httpx"
	"github.com/cloudogu/gitops-playground/go/internal/jenkins"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
	"github.com/cloudogu/gitops-playground/go/internal/runner"
	"github.com/cloudogu/gitops-playground/go/internal/scm/scmmanager"

	fargocd "github.com/cloudogu/gitops-playground/go/internal/features/argocd"
	fcm "github.com/cloudogu/gitops-playground/go/internal/features/certmanager"
	feso "github.com/cloudogu/gitops-playground/go/internal/features/externalsecrets"
	fing "github.com/cloudogu/gitops-playground/go/internal/features/ingress"
	fjenkins "github.com/cloudogu/gitops-playground/go/internal/features/jenkins"
	fmon "github.com/cloudogu/gitops-playground/go/internal/features/monitoring"
	freg "github.com/cloudogu/gitops-playground/go/internal/features/registry"
	fscm "github.com/cloudogu/gitops-playground/go/internal/features/scmmanager"
	fvault "github.com/cloudogu/gitops-playground/go/internal/features/vault"
)

// Components holds the assembled adapters. Returned from Build so callers
// can keep a handle (e.g. to read kube-context for log lines) without
// re-instantiating them.
type Components struct {
	K8s           *k8s.Client
	Helm          helm.Client
	Runner        runner.Runner
	Destroyer     *destroy.Destroyer
	Deploy        deployment.Strategy
	SCM           *scmmanager.Client
	JenkinsClient *jenkins.Client
}

// Build constructs the runtime graph for an install or destroy run. It is
// safe to call without a Kubernetes cluster reachable when DryRun is set;
// the K8s client is then nil and the features that absolutely need it
// will surface a clear error when Install is invoked.
type BuildOptions struct {
	// DryRun skips K8s client construction. Useful for --output-config-file
	// and unit tests.
	DryRun bool
	// HTTPTimeout caps every outbound HTTP call (SCM, Jenkins).
	HTTPTimeout time.Duration
}

// Build returns a runner and the assembled components.
func Build(ctx context.Context, cfg *config.Config, opts BuildOptions) (*Components, error) {
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 60 * time.Second
	}

	c := &Components{}

	if !opts.DryRun {
		kc, err := k8s.New(k8s.Options{})
		if err != nil {
			return nil, fmt.Errorf("kubernetes client: %w", err)
		}
		c.K8s = kc
	}

	c.Helm = helm.New(exec.Real{})

	helmStrategy := deployment.HelmStrategy{Client: &c.Helm}
	argoStrategy := deployment.ArgoCDStrategy{}
	c.Deploy = deployment.Deployer{
		ArgoCD:       argoStrategy,
		Helm:         helmStrategy,
		ArgoCDActive: func() bool { return cfg.Features.ArgoCD.Active && !cfg.Features.ArgoCD.Operator },
	}

	// SCM-Manager client. Always constructed; both internal and external
	// SCMM modes need it for post-deploy configuration.
	c.SCM = buildScmm(cfg, opts.HTTPTimeout)

	jenkinsFactory := func(cfg *config.Config) (*jenkins.Client, error) {
		baseURL := cfg.Jenkins.URL
		if baseURL == "" {
			// internal: cluster-DNS Service set by configurator
			baseURL = cfg.Jenkins.URLForScm
		}
		http := httpx.New(httpx.Options{
			Insecure:  cfg.Application.Insecure,
			Timeout:   opts.HTTPTimeout,
			CookieJar: true,
			BasicAuth: &httpx.BasicAuth{User: cfg.Jenkins.Username, Pass: cfg.Jenkins.Password},
			Retry:     httpx.RetryPolicy{MaxAttempts: 3},
		})
		client := &jenkins.Client{
			BaseURL: baseURL,
			User:    cfg.Jenkins.Username,
			Pass:    cfg.Jenkins.Password,
			HTTP:    http,
		}
		c.JenkinsClient = client
		return client, nil
	}

	images := imagePullAdapter{k: c.K8s}
	gitService := git.NewService()

	registry := feature.NewRegistry()
	registry.Add(
		// Registry bootstraps itself via helm (chicken-and-egg with
		// ArgoCD that hasn't been installed yet at this point).
		freg.Feature{Helm: helmStrategy},
		// SCM-Manager bootstraps the cluster — Argo CD does not exist
		// at install order 60, so go via helm imperatively.
		fscm.Feature{
			Deploy:        helmStrategy,
			Images:        images,
			Client:        c.SCM,
			JenkinsActive: func(c *config.Config) bool { return c.Jenkins.Active },
		},
		fjenkins.Feature{Deploy: c.Deploy, API: jenkinsFactory, Images: images},
		fmon.Feature{Deploy: c.Deploy, Images: images},
		// ArgoCD installs itself imperatively via helm; it cannot use
		// the ArgoCD strategy on its own first run.
		fargocd.Feature{
			Deploy: helmStrategy,
			Helm:   &c.Helm,
			Git:    gitService,
			SCM:    c.SCM, // SCM-Manager provider, may be nil for first-bootstrap edge cases
			Images: images,
		},
		fvault.Feature{Deploy: c.Deploy, Images: images},
		feso.Feature{Deploy: c.Deploy, Images: images},
		fing.Feature{Deploy: c.Deploy, Images: images},
		fcm.Feature{Deploy: c.Deploy, Images: images},
	)

	c.Runner = runner.Runner{
		Registry:      registry,
		PersistConfig: persistConfig(c.K8s),
	}

	c.Destroyer = destroy.New()
	c.Destroyer.Register(
		destroy.ArgoCDHandler{K8s: c.K8s, Helm: c.Helm},
	)
	if cfg.Jenkins.Active {
		client, err := jenkinsFactory(cfg)
		if err == nil {
			c.Destroyer.Register(destroy.JenkinsHandler{Client: client})
		}
	}
	if c.SCM != nil {
		c.Destroyer.Register(destroy.ScmmHandler{Client: c.SCM})
	}
	return c, nil
}

// defaultGopNamespace is the fallback namespace used when
// cfg.Application.GopNamespace is empty. Matches the Groovy
// Application.storeGopInformationInSecret() literal.
const defaultGopNamespace = "gop-job"

// gopConfigurationSecret is the name of the secret holding the resolved
// Config and the generated admin password. Matches the Groovy literal.
const gopConfigurationSecret = "gop-configuration"

// persistConfig returns the runner.PersistConfig closure that writes the
// resolved Config into a `gop-configuration` Secret. The closure is bound
// to k so each Build() call captures its own client. A nil k yields a
// closure that errors clearly instead of panicking — this lets dry-run
// callers still set the hook without a live cluster.
func persistConfig(k *k8s.Client) func(ctx context.Context, cfg *config.Config) error {
	return func(ctx context.Context, cfg *config.Config) error {
		if k == nil {
			return fmt.Errorf("persist gop configuration: k8s client not initialised (dry-run mode?)")
		}
		ns := cfg.Application.GopNamespace
		if ns == "" {
			ns = defaultGopNamespace
		}
		yamlBlob, err := cfg.ToYAML(true)
		if err != nil {
			return fmt.Errorf("serialising gop configuration: %w", err)
		}
		if err := k.EnsureNamespace(ctx, ns); err != nil {
			return fmt.Errorf("ensuring gop namespace %q: %w", ns, err)
		}
		data := map[string]string{
			"gop-initial-password": cfg.Application.Password,
			"gop-config":           yamlBlob,
		}
		if err := k.ApplyGenericSecret(ctx, ns, gopConfigurationSecret, data); err != nil {
			return fmt.Errorf("writing gop configuration secret %s/%s: %w", ns, gopConfigurationSecret, err)
		}
		return nil
	}
}

func buildScmm(cfg *config.Config, timeout time.Duration) *scmmanager.Client {
	scmm := &cfg.Scm.ScmManager
	base := scmm.URL
	if base == "" {
		base = scmm.UrlForJenkins
	}

	httpClient := httpx.New(httpx.Options{
		Insecure:  cfg.Application.Insecure,
		Timeout:   timeout,
		BasicAuth: &httpx.BasicAuth{User: scmm.Username, Pass: scmm.Password},
		Retry:     httpx.RetryPolicy{MaxAttempts: 3},
	})
	// scmmanager.Config expects the API base (the v2/v3 root, ending in
	// "/api/"). The SCM provider URL in the config typically points at
	// ".../scm"; we join with "/api/" so the caller can paste either form.
	apiBase := base
	if !endsWith(apiBase, "/api/") {
		apiBase = trimSuffix(apiBase, "/") + "/api/"
	}
	// User+password travel through the httpx BasicAuth transport; the
	// scmmanager.Config keeps URL plumbing only.
	return scmmanager.New(scmmanager.Config{
		APIBase:        apiBase,
		ClientBase:     base,
		InClusterBase:  base,
		NamePrefix:     cfg.Application.NamePrefix,
		GitOpsUsername: scmm.GitOpsUsername,
	}, httpClient)
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func trimSuffix(s, suffix string) string {
	if endsWith(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// imagePullAdapter is the smallest possible bridge between the feature
// helper interface and the K8s client. Each method returns an error when
// the underlying K8s client is nil so dry-run paths do not panic.
type imagePullAdapter struct {
	k *k8s.Client
}

func (a imagePullAdapter) EnsureNamespace(ctx context.Context, name string) error {
	if a.k == nil {
		return fmt.Errorf("k8s client not initialised (dry-run mode?)")
	}
	return a.k.EnsureNamespace(ctx, name)
}

func (a imagePullAdapter) CreateImagePullSecret(ctx context.Context, name, namespace, registryURL, user, password string) error {
	if a.k == nil {
		return fmt.Errorf("k8s client not initialised (dry-run mode?)")
	}
	// k8s.ApplyDockerConfigSecret takes namespace before name.
	return a.k.ApplyDockerConfigSecret(ctx, namespace, name, registryURL, user, password)
}
