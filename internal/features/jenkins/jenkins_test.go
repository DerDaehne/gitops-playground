package jenkins

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	jenkinsapi "github.com/cloudogu/gitops-playground/go/internal/jenkins"
)

// -----------------------------------------------------------------------------
// Top-level wiring
// -----------------------------------------------------------------------------

func TestFeatureMeta(t *testing.T) {
	var f Feature
	if got, want := f.Name(), "jenkins"; got != want {
		t.Errorf("Name=%q, want %q", got, want)
	}
	if f.Order() != 90 {
		t.Errorf("Order=%d, want 90", f.Order())
	}
}

func TestIsEnabled(t *testing.T) {
	cfg := config.New()
	var f Feature
	if f.IsEnabled(cfg) {
		t.Errorf("IsEnabled must default to false")
	}
	cfg.Jenkins.Active = true
	if !f.IsEnabled(cfg) {
		t.Errorf("IsEnabled must follow Jenkins.Active")
	}
}

func TestNamespaceInternalOnly(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamePrefix = "tenant-"

	// Internal: URL empty → namespace prefixed.
	if got, want := (Feature{}).Namespace(cfg), "tenant-jenkins"; got != want {
		t.Errorf("internal Namespace=%q, want %q", got, want)
	}

	// External: URL set → no owned namespace.
	cfg.Jenkins.URL = "https://jenkins.example.org"
	if got := (Feature{}).Namespace(cfg); got != "" {
		t.Errorf("external Namespace=%q, want empty", got)
	}
}

// -----------------------------------------------------------------------------
// Validate
// -----------------------------------------------------------------------------

func TestValidate(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*config.Config)
		wantErr    bool
		wantSubstr []string
	}{
		{
			name:   "internal mode skips validation",
			mutate: func(c *config.Config) {},
		},
		{
			name: "external with full credentials",
			mutate: func(c *config.Config) {
				c.Jenkins.URL = "https://jenkins.example.org"
				c.Jenkins.Username = "u"
				c.Jenkins.Password = "p"
			},
		},
		{
			name: "external missing username",
			mutate: func(c *config.Config) {
				c.Jenkins.URL = "https://jenkins.example.org"
				c.Jenkins.Username = ""
				c.Jenkins.Password = "p"
			},
			wantErr:    true,
			wantSubstr: []string{"jenkins.username"},
		},
		{
			name: "external missing password",
			mutate: func(c *config.Config) {
				c.Jenkins.URL = "https://jenkins.example.org"
				c.Jenkins.Username = "u"
				c.Jenkins.Password = ""
			},
			wantErr:    true,
			wantSubstr: []string{"jenkins.password"},
		},
		{
			name: "external + monitoring requires metrics creds",
			mutate: func(c *config.Config) {
				c.Jenkins.URL = "https://jenkins.example.org"
				c.Jenkins.Username = "u"
				c.Jenkins.Password = "p"
				c.Jenkins.MetricsUsername = ""
				c.Jenkins.MetricsPassword = ""
				c.Features.Monitoring.Active = true
			},
			wantErr:    true,
			wantSubstr: []string{"jenkins.metricsUsername", "jenkins.metricsPassword"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.New()
			// New() seeds MetricsUsername/Password to "metrics" — clear
			// for the metrics-test so we can re-set selectively.
			cfg.Jenkins.MetricsUsername = ""
			cfg.Jenkins.MetricsPassword = ""
			tc.mutate(cfg)
			err := (Feature{}).Validate(context.Background(), cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate: expected error, got nil")
				}
				for _, s := range tc.wantSubstr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("Validate error %q missing %q", err.Error(), s)
					}
				}
			} else if err != nil {
				t.Errorf("Validate: unexpected error: %v", err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// buildValues
// -----------------------------------------------------------------------------

func TestBuildValuesDefaults(t *testing.T) {
	cfg := config.New()
	v := buildValues(cfg, "")

	if v["dockerClientVersion"] != cfg.Jenkins.InternalDockerClientVersion {
		t.Errorf("dockerClientVersion=%v", v["dockerClientVersion"])
	}

	ctrl, ok := v["controller"].(map[string]any)
	if !ok {
		t.Fatalf("controller missing")
	}
	if ctrl["installPlugins"] != false {
		t.Errorf("installPlugins=%v", ctrl["installPlugins"])
	}
	if ctrl["serviceType"] != "NodePort" {
		t.Errorf("serviceType=%v", ctrl["serviceType"])
	}
	if ctrl["numExecutors"] != 0 {
		t.Errorf("numExecutors=%v", ctrl["numExecutors"])
	}
	admin := ctrl["admin"].(map[string]any)
	if admin["existingSecret"] != "jenkins-credentials" {
		t.Errorf("admin.existingSecret=%v", admin["existingSecret"])
	}
	image := ctrl["image"].(map[string]any)
	if image["registry"] != "ghcr.io" || image["repository"] != "cloudogu/jenkins-helm" {
		t.Errorf("image=%v", image)
	}
	if image["tag"] != cfg.Jenkins.Helm.Version {
		t.Errorf("image tag should track chart version, got %v", image["tag"])
	}

	// No baseURL → no ingress.
	if _, ok := ctrl["ingress"]; ok {
		t.Errorf("ingress must be absent without baseUrl")
	}

	// No monitoring → no serviceMonitor.
	if _, ok := v["serviceMonitor"]; ok {
		t.Errorf("serviceMonitor must be absent without monitoring")
	}
}

func TestBuildValuesIngressWithoutCertManager(t *testing.T) {
	cfg := config.New()
	cfg.Application.BaseURL = "http://localhost"
	cfg.Jenkins.Ingress = "jenkins.local"

	ctrl := buildValues(cfg, "")["controller"].(map[string]any)
	ing, ok := ctrl["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("ingress missing")
	}
	if ing["enabled"] != true {
		t.Errorf("ingress.enabled=%v", ing["enabled"])
	}
	if ing["hostName"] != "jenkins.local" {
		t.Errorf("ingress.hostName=%v", ing["hostName"])
	}
	if _, ok := ing["annotations"]; ok {
		t.Errorf("annotations must be absent without certManager")
	}
	if _, ok := ing["tls"]; ok {
		t.Errorf("tls must be absent without certManager")
	}
}

func TestBuildValuesIngressWithCertManager(t *testing.T) {
	cfg := config.New()
	cfg.Application.BaseURL = "http://localhost"
	cfg.Jenkins.Ingress = "jenkins.example.org"
	cfg.Features.CertManager.Active = true
	cfg.Features.CertManager.Issuer = "letsencrypt"

	ctrl := buildValues(cfg, "")["controller"].(map[string]any)
	ing := ctrl["ingress"].(map[string]any)
	ann := ing["annotations"].(map[string]any)
	if ann["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Errorf("annotations=%v", ann)
	}
	tls := ing["tls"].([]any)[0].(map[string]any)
	if tls["secretName"] != "jenkins-tls" {
		t.Errorf("tls.secretName=%v", tls["secretName"])
	}
	hosts := tls["hosts"].([]any)
	if len(hosts) != 1 || hosts[0] != "jenkins.example.org" {
		t.Errorf("tls.hosts=%v", hosts)
	}
}

func TestBuildValuesPullSecret(t *testing.T) {
	cfg := config.New()
	cfg.Registry.CreateImagePullSecrets = true
	ctrl := buildValues(cfg, "")["controller"].(map[string]any)
	if ctrl["imagePullSecretName"] != "proxy-registry" {
		t.Errorf("imagePullSecretName=%v", ctrl["imagePullSecretName"])
	}
}

func TestBuildValuesServiceMonitor(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamePrefix = "tenant-"
	cfg.Features.Monitoring.Active = true
	v := buildValues(cfg, "")
	sm, ok := v["serviceMonitor"].(map[string]any)
	if !ok {
		t.Fatalf("serviceMonitor missing")
	}
	if sm["namespace"] != "tenant-monitoring" {
		t.Errorf("serviceMonitor.namespace=%v", sm["namespace"])
	}
	if sm["additionalLabels"].(map[string]any)["release"] != "kube-prometheus-stack" {
		t.Errorf("additionalLabels=%v", sm["additionalLabels"])
	}
}

func TestAgentValuesDockerGid(t *testing.T) {
	withGid := agentValues("999")
	if withGid["runAsUser"] != "1000" {
		t.Errorf("runAsUser with GID=%v, want 1000", withGid["runAsUser"])
	}
	if withGid["runAsGroup"] != "999" {
		t.Errorf("runAsGroup with GID=%v, want 999", withGid["runAsGroup"])
	}

	noGid := agentValues("")
	if noGid["runAsUser"] != "0" {
		t.Errorf("runAsUser without GID=%v, want 0", noGid["runAsUser"])
	}
	if noGid["runAsGroup"] != "133" {
		t.Errorf("runAsGroup without GID=%v, want 133", noGid["runAsGroup"])
	}
}

// -----------------------------------------------------------------------------
// Install dispatch
// -----------------------------------------------------------------------------

// stubDeploy records the spec the feature would have shipped to helm.
type stubDeploy struct {
	called bool
	spec   deployment.Spec
}

func (s *stubDeploy) Deploy(_ context.Context, spec deployment.Spec) error {
	s.called = true
	s.spec = spec
	return nil
}

func TestInstallExternalSkipsHelm(t *testing.T) {
	cfg := config.New()
	cfg.Jenkins.Active = true
	cfg.Jenkins.URL = "https://jenkins.example.org"
	cfg.Jenkins.Username = "u"
	cfg.Jenkins.Password = "p"

	d := &stubDeploy{}
	// API factory returns nil to mean "no configuration needed in this
	// test" — exercises the external-mode path without needing a real
	// Jenkins server.
	f := Feature{
		Deploy: d,
		API:    func(*config.Config) (*jenkinsapi.Client, error) { return nil, nil },
	}
	if err := f.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if d.called {
		t.Errorf("Deploy must not be called in external mode")
	}
}

func TestInstallInternalRequiresDeploy(t *testing.T) {
	cfg := config.New()
	cfg.Jenkins.Active = true // URL empty → internal

	f := Feature{
		Deploy: nil,
		API:    func(*config.Config) (*jenkinsapi.Client, error) { return nil, nil },
	}
	err := f.Install(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error when Deploy is nil")
	}
	if !strings.Contains(err.Error(), "Deploy strategy") {
		t.Errorf("error=%v", err)
	}
}

func TestInstallInternalShipsChartSpec(t *testing.T) {
	cfg := config.New()
	cfg.Jenkins.Active = true
	cfg.Application.NamePrefix = "tenant-"

	d := &stubDeploy{}
	f := Feature{
		Deploy: d,
		API:    func(*config.Config) (*jenkinsapi.Client, error) { return nil, nil },
	}
	if err := f.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !d.called {
		t.Fatalf("Deploy was not invoked")
	}
	if d.spec.ReleaseName != "jenkins" {
		t.Errorf("ReleaseName=%q", d.spec.ReleaseName)
	}
	if d.spec.Namespace != "tenant-jenkins" {
		t.Errorf("Namespace=%q", d.spec.Namespace)
	}
	if d.spec.ChartOrPath != cfg.Jenkins.Helm.Chart {
		t.Errorf("Chart=%q", d.spec.ChartOrPath)
	}
	if d.spec.RepoURL != cfg.Jenkins.Helm.RepoURL {
		t.Errorf("RepoURL=%q", d.spec.RepoURL)
	}
	if d.spec.Version != cfg.Jenkins.Helm.Version {
		t.Errorf("Version=%q", d.spec.Version)
	}
	if d.spec.RepoType != deployment.RepoHelm {
		t.Errorf("RepoType=%v", d.spec.RepoType)
	}
}

func TestInstallAPIFactoryErrorBubbles(t *testing.T) {
	cfg := config.New()
	cfg.Jenkins.Active = true
	cfg.Jenkins.URL = "https://jenkins.example.org" // external → no helm path
	cfg.Jenkins.Username = "u"
	cfg.Jenkins.Password = "p"

	boom := errors.New("boom")
	f := Feature{
		API: func(*config.Config) (*jenkinsapi.Client, error) { return nil, boom },
	}
	err := f.Install(context.Background(), cfg)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("Install error=%v, want wrapping of boom", err)
	}
}

func TestInstallSkippedAPIWhenFactoryNil(t *testing.T) {
	cfg := config.New()
	cfg.Jenkins.Active = true
	cfg.Jenkins.URL = "https://jenkins.example.org"
	cfg.Jenkins.Username = "u"
	cfg.Jenkins.Password = "p"

	f := Feature{API: nil}
	if err := f.Install(context.Background(), cfg); err != nil {
		t.Errorf("Install with nil API factory must be a no-op, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Disable
// -----------------------------------------------------------------------------

func TestDisableNoop(t *testing.T) {
	if err := (Feature{}).Disable(context.Background(), config.New()); err != nil {
		t.Errorf("Disable should be a no-op, got %v", err)
	}
}
