package monitoring

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
)

// stubAnnotations satisfies NamespaceAnnotationReader for the OpenShift
// UID resolution tests below.
type stubAnnotations struct {
	values map[string]map[string]string // namespace -> key -> value
	err    error
}

func (s stubAnnotations) NamespaceAnnotation(_ context.Context, namespace, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if ns, ok := s.values[namespace]; ok {
		return ns[key], nil
	}
	return "", nil
}

// -----------------------------------------------------------------------------
// Top-level wiring
// -----------------------------------------------------------------------------

func TestFeatureMeta(t *testing.T) {
	var f Feature
	if f.Name() != "monitoring" {
		t.Errorf("Name=%q", f.Name())
	}
	if f.Order() != 80 {
		t.Errorf("Order=%d, want 80", f.Order())
	}

	cfg := config.New()
	if f.IsEnabled(cfg) {
		t.Errorf("IsEnabled must be false on default config")
	}
	cfg.Features.Monitoring.Active = true
	if !f.IsEnabled(cfg) {
		t.Errorf("IsEnabled must be true when Monitoring.Active")
	}

	cfg.Application.NamePrefix = "tenant-"
	if got, want := f.Namespace(cfg), "tenant-monitoring"; got != want {
		t.Errorf("Namespace=%q, want %q", got, want)
	}
}

// -----------------------------------------------------------------------------
// buildValues — head/top-level block
// -----------------------------------------------------------------------------

func TestBuildValuesDefaults(t *testing.T) {
	cfg := config.New()
	v := Feature{}.buildValues(cfg)

	// skipCrds=false → no crds key.
	if _, ok := v["crds"]; ok {
		t.Errorf("crds must be absent when SkipCRDs=false")
	}
	// neither namespaceIsolation nor createImagePullSecrets → no global.
	if _, ok := v["global"]; ok {
		t.Errorf("global must be absent when neither flag is set")
	}
	// disabled subcharts present.
	for _, k := range []string{
		"kubeStateMetrics", "nodeExporter", "kubelet",
		"kubeControllerManager", "coreDns", "kubeDns",
		"kubeEtcd", "kubeScheduler", "kubeProxy", "alertmanager",
	} {
		m, ok := v[k].(map[string]any)
		if !ok {
			t.Fatalf("%s missing", k)
		}
		if m["enabled"] != false {
			t.Errorf("%s.enabled=%v, want false", k, m["enabled"])
		}
	}
	// defaultRules keeps `general` true.
	rules := v["defaultRules"].(map[string]any)["rules"].(map[string]any)
	if rules["general"] != true {
		t.Errorf("defaultRules.general=%v, want true", rules["general"])
	}
	if rules["alertmanager"] != false {
		t.Errorf("defaultRules.alertmanager=%v, want false", rules["alertmanager"])
	}
}

func TestBuildValuesSkipCRDs(t *testing.T) {
	cfg := config.New()
	cfg.Application.SkipCRDs = true
	v := Feature{}.buildValues(cfg)
	crds, ok := v["crds"].(map[string]any)
	if !ok {
		t.Fatalf("crds missing")
	}
	if crds["enabled"] != false {
		t.Errorf("crds.enabled=%v, want false", crds["enabled"])
	}
}

func TestBuildValuesGlobalAndKubeApiServer(t *testing.T) {
	cfg := config.New()
	cfg.Registry.CreateImagePullSecrets = true
	cfg.Application.NamespaceIsolation = true
	v := Feature{}.buildValues(cfg)

	global := v["global"].(map[string]any)
	if got := global["imagePullSecrets"].([]any); len(got) != 1 {
		t.Errorf("imagePullSecrets=%v", got)
	}
	if rbac := global["rbac"].(map[string]any); rbac["create"] != false {
		t.Errorf("global.rbac.create=%v", rbac["create"])
	}
	if api := v["kubeApiServer"].(map[string]any); api["enabled"] != false {
		t.Errorf("kubeApiServer.enabled=%v", api["enabled"])
	}
}

func TestBuildValuesGlobalImagePullOnly(t *testing.T) {
	cfg := config.New()
	cfg.Registry.CreateImagePullSecrets = true
	v := Feature{}.buildValues(cfg)
	global := v["global"].(map[string]any)
	if _, ok := global["rbac"]; ok {
		t.Errorf("global.rbac must be absent without NamespaceIsolation")
	}
	if _, ok := v["kubeApiServer"]; ok {
		t.Errorf("kubeApiServer must be absent without NamespaceIsolation")
	}
}

// -----------------------------------------------------------------------------
// prometheusOperatorValues
// -----------------------------------------------------------------------------

func TestPrometheusOperatorOpenShiftAndPodResources(t *testing.T) {
	cfg := config.New()
	cfg.Application.Openshift = true
	cfg.Application.PodResources = true
	cfg.Features.Monitoring.Helm.PrometheusOperatorImage = "quay.io/prometheus-operator/prometheus-operator:v0.74"
	cfg.Features.Monitoring.Helm.PrometheusConfigReloaderImage = "quay.io/prometheus-operator/prometheus-config-reloader:v0.74"

	op := prometheusOperatorValues(cfg)
	sec := op["securityContext"].(map[string]any)
	if sec["runAsUser"] != nil {
		t.Errorf("securityContext.runAsUser=%v, want nil", sec["runAsUser"])
	}
	res := op["resources"].(map[string]any)
	if res["limits"].(map[string]any)["cpu"] != "300m" {
		t.Errorf("operator resources wrong")
	}
	img := op["image"].(map[string]any)
	if img["registry"] != "quay.io" {
		t.Errorf("operator image registry=%v", img["registry"])
	}
	if img["repository"] != "prometheus-operator/prometheus-operator" {
		t.Errorf("operator image repo=%v", img["repository"])
	}
	if img["tag"] != "v0.74" {
		t.Errorf("operator image tag=%v", img["tag"])
	}
	reloader := op["prometheusConfigReloader"].(map[string]any)
	if _, ok := reloader["image"].(map[string]any); !ok {
		t.Errorf("config reloader image missing")
	}
	if _, ok := reloader["resources"].(map[string]any); !ok {
		t.Errorf("config reloader resources missing")
	}
}

func TestPrometheusOperatorNamespaceIsolation(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamespaceIsolation = true
	cfg.Application.Namespaces.DedicatedNamespaces = []string{"a", "b"}
	op := prometheusOperatorValues(cfg)
	if op["kubeletService"].(map[string]any)["enabled"] != false {
		t.Errorf("kubeletService.enabled must be false")
	}
	ns := op["namespaces"].(map[string]any)
	add := ns["additional"].([]any)
	if len(add) != 2 || add[0] != "a" || add[1] != "b" {
		t.Errorf("operator namespaces.additional=%v", add)
	}
}

// -----------------------------------------------------------------------------
// grafanaValues — including the mail/SMTP "alertmanager email" sub-block
// -----------------------------------------------------------------------------

func TestGrafanaValuesDefaults(t *testing.T) {
	cfg := config.New()
	g := Feature{}.grafanaValues(cfg)

	if g["adminUser"] != config.DefaultAdminUser {
		t.Errorf("adminUser=%v", g["adminUser"])
	}
	if g["defaultDashboardsEnabled"] != false {
		t.Errorf("defaultDashboardsEnabled must be false")
	}
	// Without an ingress URL, no ingress key.
	if _, ok := g["ingress"]; ok {
		t.Errorf("ingress must be absent without GrafanaURL")
	}
	// Mail off → no notifiers/alerting/env.
	for _, k := range []string{"notifiers", "alerting", "env", "smtp"} {
		if _, ok := g[k]; ok {
			t.Errorf("grafana[%s] must be absent when Mail.Active=false", k)
		}
	}
	// sidecar searchNamespace defaults to "ALL".
	dash := g["sidecar"].(map[string]any)["dashboards"].(map[string]any)
	if dash["searchNamespace"] != "ALL" {
		t.Errorf("searchNamespace=%v, want ALL", dash["searchNamespace"])
	}
}

func TestGrafanaValuesOpenShiftUID(t *testing.T) {
	cfg := config.New()
	cfg.Application.Openshift = true
	f := Feature{OpenShiftUID: "1000700000"}
	g := f.grafanaValues(cfg)
	sec := g["securityContext"].(map[string]any)
	if sec["runAsUser"] != "1000700000" {
		t.Errorf("runAsUser=%v", sec["runAsUser"])
	}
}

func TestGrafanaValuesIngressWithCertManager(t *testing.T) {
	cfg := config.New()
	cfg.Features.Monitoring.GrafanaURL = "https://grafana.example.org"
	cfg.Features.CertManager.Active = true
	cfg.Features.CertManager.Issuer = "letsencrypt"
	g := Feature{}.grafanaValues(cfg)
	ing := g["ingress"].(map[string]any)
	if ing["enabled"] != true {
		t.Errorf("ingress.enabled=%v", ing["enabled"])
	}
	hosts := ing["hosts"].([]any)
	if hosts[0] != "grafana.example.org" {
		t.Errorf("ingress.hosts=%v", hosts)
	}
	ann := ing["annotations"].(map[string]any)
	if ann["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Errorf("annotations=%v", ann)
	}
	tls := ing["tls"].([]any)[0].(map[string]any)
	if tls["secretName"] != "grafana-tls" {
		t.Errorf("tls.secretName=%v", tls["secretName"])
	}
}

func TestGrafanaValuesSearchNamespaceIsolation(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamespaceIsolation = true
	cfg.Application.Namespaces.DedicatedNamespaces = []string{"ns1", "ns2"}
	g := Feature{}.grafanaValues(cfg)
	dash := g["sidecar"].(map[string]any)["dashboards"].(map[string]any)
	if dash["searchNamespace"] != "ns1,ns2" {
		t.Errorf("searchNamespace=%v, want ns1,ns2", dash["searchNamespace"])
	}
}

func TestGrafanaValuesMailBlock(t *testing.T) {
	cfg := config.New()
	cfg.Features.Mail.Active = true
	cfg.Features.Mail.SMTPAddress = "smtp.example.org"
	cfg.Features.Mail.SMTPPort = 587
	cfg.Features.Mail.SMTPUser = "u"
	cfg.Features.Mail.SMTPPassword = "p"
	cfg.Features.Monitoring.GrafanaEmailFrom = "alerts@example.org"
	cfg.Features.Monitoring.GrafanaEmailTo = "team@example.org"

	g := Feature{}.grafanaValues(cfg)

	// notifiers
	n := g["notifiers"].(map[string]any)["notifiers.yaml"].(map[string]any)["notifiers"].([]any)[0].(map[string]any)
	if n["settings"].(map[string]any)["addresses"] != "team@example.org" {
		t.Errorf("notifier addresses=%v", n["settings"])
	}
	// alerting contact points
	cp := g["alerting"].(map[string]any)["contactpoints.yaml"].(map[string]any)["contactPoints"].([]any)[0].(map[string]any)
	rec := cp["receivers"].([]any)[0].(map[string]any)
	if rec["settings"].(map[string]any)["addresses"] != "team@example.org" {
		t.Errorf("contact addresses=%v", rec["settings"])
	}
	// smtp.existingSecret because user/password present
	smtp := g["smtp"].(map[string]any)
	if smtp["existingSecret"] != "grafana-email-secret" {
		t.Errorf("smtp.existingSecret=%v", smtp["existingSecret"])
	}
	// env block
	env := g["env"].(map[string]any)
	if env["GF_SMTP_ENABLED"] != true {
		t.Errorf("GF_SMTP_ENABLED=%v", env["GF_SMTP_ENABLED"])
	}
	if env["GF_SMTP_FROM_ADDRESS"] != "alerts@example.org" {
		t.Errorf("GF_SMTP_FROM_ADDRESS=%v", env["GF_SMTP_FROM_ADDRESS"])
	}
	if env["GF_SMTP_HOST"] != "smtp.example.org:587" {
		t.Errorf("GF_SMTP_HOST=%v, want host:port", env["GF_SMTP_HOST"])
	}
}

func TestGrafanaValuesMailNoSMTPSecret(t *testing.T) {
	cfg := config.New()
	cfg.Features.Mail.Active = true
	cfg.Features.Mail.SMTPAddress = "smtp.example.org"
	g := Feature{}.grafanaValues(cfg)
	if _, ok := g["smtp"]; ok {
		t.Errorf("smtp must be absent when no user/password set")
	}
	env := g["env"].(map[string]any)
	if env["GF_SMTP_HOST"] != "smtp.example.org" {
		t.Errorf("GF_SMTP_HOST without port=%v", env["GF_SMTP_HOST"])
	}
}

// -----------------------------------------------------------------------------
// prometheusValues
// -----------------------------------------------------------------------------

func TestPrometheusValuesEmptyNamespaces(t *testing.T) {
	cfg := config.New()
	spec := Feature{}.prometheusValues(cfg)["prometheusSpec"].(map[string]any)
	// secrets always present
	secs := spec["secrets"].([]any)
	if len(secs) != 2 {
		t.Errorf("secrets=%v", secs)
	}
	for _, key := range []string{
		"serviceMonitorNamespaceSelector",
		"podMonitorNamespaceSelector",
		"ruleNamespaceSelector",
		"probeNamespaceSelector",
	} {
		sel := spec[key].(map[string]any)
		me := sel["matchExpressions"].([]any)[0].(map[string]any)
		if vals := me["values"].([]any); len(vals) != 0 {
			t.Errorf("%s.values=%v, want empty", key, vals)
		}
	}
}

func TestPrometheusValuesWithNamespaces(t *testing.T) {
	cfg := config.New()
	cfg.Application.Namespaces.DedicatedNamespaces = []string{"x"}
	cfg.Application.Namespaces.TenantNamespaces = []string{"y"}
	spec := Feature{}.prometheusValues(cfg)["prometheusSpec"].(map[string]any)
	sel := spec["serviceMonitorNamespaceSelector"].(map[string]any)
	vals := sel["matchExpressions"].([]any)[0].(map[string]any)["values"].([]any)
	if len(vals) != 2 || vals[0] != "x" || vals[1] != "y" {
		t.Errorf("values=%v", vals)
	}
}

func TestPrometheusValuesScrapeConfigs(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamePrefix = "tenant-"
	cfg.Jenkins.Active = true
	f := Feature{
		SCMProviderType: "scm_manager",
		SCM:             MetricsEndpoint{Protocol: "http", Host: "scm:80", Path: "/scm/api/v2/metrics/prometheus"},
		Jenkins:         MetricsEndpoint{Protocol: "http", Host: "jenkins:8080", Path: "/prometheus", Username: "metrics"},
	}
	spec := f.prometheusValues(cfg)["prometheusSpec"].(map[string]any)
	jobs := spec["additionalScrapeConfigs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	scm := jobs[0].(map[string]any)
	if scm["job_name"] != "scm-manager" {
		t.Errorf("first job=%v", scm["job_name"])
	}
	if scm["basic_auth"].(map[string]any)["username"] != "tenant-metrics" {
		t.Errorf("scm username=%v", scm["basic_auth"])
	}
	jenkins := jobs[1].(map[string]any)
	if jenkins["job_name"] != "jenkins" {
		t.Errorf("second job=%v", jenkins["job_name"])
	}
}

func TestPrometheusValuesNoScrapeWhenDisabled(t *testing.T) {
	cfg := config.New()
	f := Feature{}
	spec := f.prometheusValues(cfg)["prometheusSpec"].(map[string]any)
	if _, ok := spec["additionalScrapeConfigs"]; ok {
		t.Errorf("additionalScrapeConfigs must be absent when nothing is configured")
	}
}

func TestPrometheusValuesOpenShift(t *testing.T) {
	cfg := config.New()
	cfg.Application.Openshift = true
	spec := Feature{}.prometheusValues(cfg)["prometheusSpec"].(map[string]any)
	if _, ok := spec["securityContext"].(map[string]any); !ok {
		t.Errorf("OpenShift securityContext missing")
	}
	if v, ok := spec["automountServiceAccountToken"]; !ok || v != nil {
		t.Errorf("automountServiceAccountToken must be present and nil")
	}
}

// -----------------------------------------------------------------------------
// alertManagerValues
// -----------------------------------------------------------------------------

func TestAlertManagerValuesDisabled(t *testing.T) {
	am := alertManagerValues()
	if am["enabled"] != false {
		t.Errorf("alertmanager.enabled=%v, want false", am["enabled"])
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func TestGrafanaHost(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"https://g.example.org":        "g.example.org",
		"https://g.example.org:8443/x": "g.example.org:8443",
	}
	for in, want := range cases {
		if got := grafanaHost(in); got != want {
			t.Errorf("grafanaHost(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestParseImage(t *testing.T) {
	cases := []struct {
		in                  string
		registry, repo, tag string
	}{
		{"foo:tag", "", "foo", "tag"},
		{"foo/bar:tag", "", "foo/bar", "tag"},
		{"docker.io/library/foo:v1", "docker.io", "library/foo", "v1"},
		{"quay.io/prometheus-operator/prometheus-operator:v0.74", "quay.io", "prometheus-operator/prometheus-operator", "v0.74"},
	}
	for _, c := range cases {
		reg, repo, tag := parseImage(c.in)
		if reg != c.registry || repo != c.repo || tag != c.tag {
			t.Errorf("parseImage(%q)=(%q,%q,%q), want (%q,%q,%q)",
				c.in, reg, repo, tag, c.registry, c.repo, c.tag)
		}
	}
}

func TestResolveOpenShiftUID(t *testing.T) {
	tests := []struct {
		name    string
		reader  stubAnnotations
		want    string
		wantErr bool
	}{
		{
			name: "canonical uid range",
			reader: stubAnnotations{values: map[string]map[string]string{
				"monitoring": {k8s.OpenShiftUIDRangeAnnotation: "1000700000/10000"},
			}},
			want: "1000700000",
		},
		{
			name: "empty annotation falls back to empty uid",
			reader: stubAnnotations{values: map[string]map[string]string{
				"monitoring": {k8s.OpenShiftUIDRangeAnnotation: ""},
			}},
			want: "",
		},
		{
			name:   "missing annotation falls back to empty uid",
			reader: stubAnnotations{values: map[string]map[string]string{"monitoring": {}}},
			want:   "",
		},
		{
			name:    "API error is surfaced",
			reader:  stubAnnotations{err: errors.New("boom")},
			wantErr: true,
		},
		{
			name: "garbage annotation falls back to empty uid",
			reader: stubAnnotations{values: map[string]map[string]string{
				"monitoring": {k8s.OpenShiftUIDRangeAnnotation: "not/a-number"},
			}},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOpenShiftUID(context.Background(), tc.reader, "monitoring")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("uid=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchNamespace(t *testing.T) {
	cfg := config.New()
	if got := searchNamespace(cfg); got != "ALL" {
		t.Errorf("searchNamespace default=%q, want ALL", got)
	}
	cfg.Application.NamespaceIsolation = true
	cfg.Application.Namespaces.DedicatedNamespaces = []string{"a"}
	cfg.Application.Namespaces.TenantNamespaces = []string{"b", "c"}
	if got := searchNamespace(cfg); got != "a,b,c" {
		t.Errorf("searchNamespace=%q, want a,b,c", got)
	}
}
