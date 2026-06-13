package scmmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	scmmclient "github.com/cloudogu/gitops-playground/go/internal/scm/scmmanager"
)

// withInternalScm returns a config with the bare-minimum scm.scmManager
// block populated so isInternal/buildValues have something to read.
func withInternalScm(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.New()
	cfg.Scm.ScmManager.URL = ""
	cfg.Scm.ScmManager.Username = "admin"
	cfg.Scm.ScmManager.Password = "admin"
	return cfg
}

func TestIsEnabled_InternalDefaultsToTrue(t *testing.T) {
	cfg := config.New()
	// No scm.Raw at all → treated as internal (default).
	if !isInternal(cfg) {
		t.Fatalf("isInternal should default to true when no scm block is set")
	}
	if !(Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should return true for a default config")
	}
}

func TestIsEnabled_EmptyURLIsInternal(t *testing.T) {
	cfg := withInternalScm(t)
	if !isInternal(cfg) {
		t.Fatalf("empty url should mean internal")
	}
	if !(Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should be true when url == \"\"")
	}
}

func TestIsEnabled_ExternalURLDisablesFeature(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.URL = "https://scmm.example.org"
	if isInternal(cfg) {
		t.Fatalf("non-empty url should switch off internal mode")
	}
	if (Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should be false when an external URL is set")
	}
}

func TestNamespace_UsesPrefixAndOverride(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Application.NamePrefix = "tenant-"
	if got, want := (Feature{}).Namespace(cfg), "tenant-scm-manager"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}

	cfg.Scm.ScmManager.Namespace = "scmm-custom"
	if got, want := (Feature{}).Namespace(cfg), "tenant-scmm-custom"; got != want {
		t.Errorf("Namespace (override) = %q, want %q", got, want)
	}
}

func TestValidate_RequiresCredentials(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.Password = ""
	if err := (Feature{}).Validate(context.Background(), cfg); err == nil {
		t.Fatalf("Validate should error on empty password")
	}
}

func TestValidate_ExternalSkipsChecks(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.URL = "https://scmm.example.org"
	cfg.Scm.ScmManager.Password = ""
	if err := (Feature{}).Validate(context.Background(), cfg); err != nil {
		t.Fatalf("Validate should be a no-op when external, got %v", err)
	}
}

func TestValidate_RequiresUrlForJenkinsWhenActive(t *testing.T) {
	cfg := withInternalScm(t)
	f := Feature{JenkinsActive: func(*config.Config) bool { return true }}
	if err := f.Validate(context.Background(), cfg); err == nil {
		t.Fatalf("Validate should error when jenkins is active and urlForJenkins is empty")
	}
	cfg.Scm.ScmManager.UrlForJenkins = "http://jenkins.example/"
	if err := f.Validate(context.Background(), cfg); err != nil {
		t.Fatalf("Validate should pass once urlForJenkins is set, got %v", err)
	}
}

func TestBuildValues_BaseShape(t *testing.T) {
	cfg := withInternalScm(t)
	v := buildValues(cfg)

	if got := v["fullnameOverride"]; got != releaseName {
		t.Errorf("fullnameOverride = %v, want %q", got, releaseName)
	}
	svc, ok := v["service"].(map[string]any)
	if !ok || svc["type"] != "NodePort" {
		t.Errorf("service = %+v", v["service"])
	}
	if _, ok := v["ingress"]; ok {
		t.Errorf("ingress must be absent when scmManager.ingress is empty")
	}
	env, _ := v["extraEnv"].(string)
	if !strings.Contains(env, `value: "admin"`) {
		t.Errorf("extraEnv should embed the configured user/password: %q", env)
	}
}

func TestBuildValues_IngressWithCertManager(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.Ingress = "scmm.example.org"
	cfg.Features.CertManager.Active = true
	cfg.Features.CertManager.Issuer = "letsencrypt"

	v := buildValues(cfg)
	ing, ok := v["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("ingress missing: %+v", v)
	}
	if ing["enabled"] != true || ing["path"] != "/" {
		t.Errorf("ingress base = %+v", ing)
	}
	hosts, _ := ing["hosts"].([]any)
	if len(hosts) != 1 || hosts[0] != "scmm.example.org" {
		t.Errorf("ingress.hosts = %+v", hosts)
	}
	ann, _ := ing["annotations"].(map[string]any)
	if ann["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Errorf("annotations = %+v", ann)
	}
	tls, _ := ing["tls"].([]any)
	if len(tls) != 1 {
		t.Fatalf("tls block missing: %+v", ing)
	}
	tlsEntry, _ := tls[0].(map[string]any)
	if tlsEntry["secretName"] != "scm-manager-tls" {
		t.Errorf("tls secretName = %v", tlsEntry["secretName"])
	}
}

func TestBuildValues_IngressWithoutCertManager(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.Ingress = "scmm.example.org"

	v := buildValues(cfg)
	ing, _ := v["ingress"].(map[string]any)
	if _, ok := ing["annotations"]; ok {
		t.Errorf("annotations must be absent when certManager is off")
	}
	if _, ok := ing["tls"]; ok {
		t.Errorf("tls must be absent when certManager is off")
	}
}

func newWaitFeature(t *testing.T, handler http.HandlerFunc) (Feature, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := scmmclient.New(scmmclient.Config{
		APIBase: srv.URL + "/scm/api/",
	}, srv.Client())
	return Feature{Client: client, PollInterval: 20 * time.Millisecond}, srv
}

func TestWaitForAvailable_Success(t *testing.T) {
	var calls int32
	f, _ := newWaitFeature(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := f.WaitForAvailable(ctx, time.Second); err != nil {
		t.Fatalf("WaitForAvailable: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected at least two polls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestWaitForAvailable_Timeout(t *testing.T) {
	f, _ := newWaitFeature(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	start := time.Now()
	err := f.WaitForAvailable(context.Background(), 150*time.Millisecond)
	if err == nil {
		t.Fatalf("WaitForAvailable should time out")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention timeout: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("WaitForAvailable should return promptly after timeout, took %s", time.Since(start))
	}
}

func TestWaitForAvailable_NoClient(t *testing.T) {
	if err := (Feature{}).WaitForAvailable(context.Background(), 10*time.Millisecond); err == nil {
		t.Errorf("WaitForAvailable without a client should error")
	}
}

// TestConfigureExternal_RunsAgainstExternalSCMM walks the external
// path end-to-end against an httptest server: WaitForAvailable
// succeeds, the plugin install loop fires, /v2/config is PUT
// (applySetupConfig), and the gitops + metrics users are created.
// This is the WP-B2 acceptance: an externally-provided SCMM must get
// its bootstrap config even though IsEnabled returns false.
func TestConfigureExternal_RunsAgainstExternalSCMM(t *testing.T) {
	var (
		pluginInstalls int32
		setConfigCalls int32
		userCreates    int32
		permGrants     int32
	)

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/scm/api/v2"):
			// availability probe
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v2/plugins/available/"):
			atomic.AddInt32(&pluginInstalls, 1)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/v2/config"):
			atomic.AddInt32(&setConfigCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/users"):
			atomic.AddInt32(&userCreates, 1)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/v2/users/") && strings.HasSuffix(r.URL.Path, "/permissions"):
			atomic.AddInt32(&permGrants, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Logf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)

	client := scmmclient.New(scmmclient.Config{
		APIBase:        srv.URL + "/scm/api/",
		GitOpsUsername: "gitops",
	}, srv.Client())

	cfg := config.New()
	cfg.Scm.ScmManager.URL = srv.URL + "/scm"
	cfg.Scm.ScmManager.Username = "admin"
	cfg.Scm.ScmManager.Password = "admin"
	cfg.Scm.ScmManager.GitOpsUsername = "gitops"
	// Skip the trailing restart so the test does not need to fake the
	// /v2 polling cycle that the plugin restart triggers.
	cfg.Scm.ScmManager.SkipRestart = true

	f := Feature{
		Client:       client,
		PollInterval: 10 * time.Millisecond,
	}

	// Sanity: IsEnabled stays false for the external case. WP-B2's
	// whole point is that ConfigureExternal still runs.
	if f.IsEnabled(cfg) {
		t.Fatalf("IsEnabled should be false for an external URL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.ConfigureExternal(ctx, cfg); err != nil {
		t.Fatalf("ConfigureExternal: %v", err)
	}

	if atomic.LoadInt32(&pluginInstalls) == 0 {
		t.Errorf("expected plugin installs to fire, got 0")
	}
	if got := atomic.LoadInt32(&setConfigCalls); got != 1 {
		t.Errorf("/v2/config PUT calls: want 1, got %d", got)
	}
	if got := atomic.LoadInt32(&userCreates); got < 2 {
		t.Errorf("expected at least gitops + metrics user creates, got %d", got)
	}
	if got := atomic.LoadInt32(&permGrants); got != 1 {
		t.Errorf("expected one metrics permission grant, got %d", got)
	}
}

// TestConfigureExternal_NoOpForInternal is the negative twin: when
// SCMM runs internally, Install drives Configure — ConfigureExternal
// must stay out of the way.
func TestConfigureExternal_NoOpForInternal(t *testing.T) {
	cfg := withInternalScm(t)
	if err := (Feature{}).ConfigureExternal(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureExternal on internal config should be a no-op, got %v", err)
	}
}

// TestConfigureExternal_NoClientIsNoOp documents the safe-call
// contract: an external URL with no API client wired in still returns
// nil so the runner can call this unconditionally.
func TestConfigureExternal_NoClientIsNoOp(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.ScmManager.URL = "https://scmm.example.org"
	if err := (Feature{}).ConfigureExternal(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureExternal without a client should be a no-op, got %v", err)
	}
}
