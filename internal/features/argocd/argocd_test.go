package argocd

// argocd_test.go covers the install-side helpers added in WP-B3:
//
//   - applyAdminPasswordSecret: bcrypt + merge-patch of argocd-secret
//     against a fake k8s client.
//   - installViaHelmWithValues: helm repo add / dependency build /
//     upgrade dispatched through a stubbed exec.Runner.
//   - readArgoHelmRepoURL: Chart.lock parser fall-through to the
//     canonical upstream URL.
//
// The bootstrap / repo-push side of Install is exercised by integration
// tests further up the stack; this file keeps the unit surface narrow
// so a sub-agent can run it without a live cluster.

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/exec"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
)

// fakeK8s implements the narrow argocd.K8s interface on top of an
// in-memory secret store. It records every Patch call so tests can
// assert on the body verbatim.
type fakeK8s struct {
	mu      sync.Mutex
	secrets map[string]*corev1.Secret // key = ns/name
	getErr  error
	patches []recordedPatch
}

type recordedPatch struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
	patchType k8s.PatchType
	body      string
}

func newFakeK8s() *fakeK8s {
	return &fakeK8s{secrets: map[string]*corev1.Secret{}}
}

func (f *fakeK8s) put(ns, name string, data map[string][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[ns+"/"+name] = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       data,
	}
}

func (f *fakeK8s) GetSecret(_ context.Context, namespace, name string) (*corev1.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	sec, ok := f.secrets[namespace+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, name)
	}
	return sec.DeepCopy(), nil
}

func (f *fakeK8s) Patch(_ context.Context, gvr schema.GroupVersionResource, namespace, name string, patchType k8s.PatchType, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches = append(f.patches, recordedPatch{
		gvr: gvr, namespace: namespace, name: name, patchType: patchType, body: string(body),
	})
	return nil
}

func TestApplyAdminPasswordSecret_PatchesBcryptHash(t *testing.T) {
	t.Parallel()

	const (
		ns   = "argocd"
		pw   = "s3cret"
		hash = "" // populated below
	)
	_ = hash

	fk := newFakeK8s()
	fk.put(ns, argoCDSecret, map[string][]byte{
		"admin.password": []byte("placeholder-from-helm-chart"),
	})

	cfg := &config.Config{}
	cfg.Application.Password = pw

	if err := applyAdminPasswordSecret(context.Background(), fk, cfg, ns); err != nil {
		t.Fatalf("applyAdminPasswordSecret: %v", err)
	}

	if got := len(fk.patches); got != 1 {
		t.Fatalf("want 1 patch, got %d", got)
	}
	p := fk.patches[0]
	if p.namespace != ns || p.name != argoCDSecret {
		t.Fatalf("patch target = %s/%s, want %s/%s", p.namespace, p.name, ns, argoCDSecret)
	}
	if p.patchType != k8s.PatchJSONMerge {
		t.Fatalf("patch type = %v, want %v", p.patchType, k8s.PatchJSONMerge)
	}
	if p.gvr.Resource != "secrets" || p.gvr.Version != "v1" || p.gvr.Group != "" {
		t.Fatalf("patch gvr = %v, want core/v1 secrets", p.gvr)
	}

	// The body is a JSON-merge patch with data.admin.password (base64 of
	// the bcrypt hash) and data.admin.passwordMtime (base64 of an ISO
	// timestamp). We verify the bcrypt by round-tripping.
	body := p.body
	hashB64 := extractDataField(t, body, "admin.password")
	hashBytes, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		t.Fatalf("decode admin.password base64: %v", err)
	}
	if err := VerifyAdminPassword(string(hashBytes), pw); err != nil {
		t.Fatalf("bcrypt hash does not validate against %q: %v", pw, err)
	}

	mtimeB64 := extractDataField(t, body, "admin.passwordMtime")
	mtimeBytes, err := base64.StdEncoding.DecodeString(mtimeB64)
	if err != nil {
		t.Fatalf("decode admin.passwordMtime base64: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, string(mtimeBytes)); err != nil {
		t.Fatalf("admin.passwordMtime is not RFC3339: %q (%v)", string(mtimeBytes), err)
	}
}

func TestApplyAdminPasswordSecret_RefusesEmptyInputs(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Application.Password = "x"

	tests := []struct {
		name    string
		kc      K8s
		cfg     *config.Config
		wantSub string
	}{
		{"nil k8s", nil, cfg, "K8s client is nil"},
		{"nil cfg", newFakeK8s(), nil, "config is nil"},
		{"empty password", newFakeK8s(), &config.Config{}, "Password is empty"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := applyAdminPasswordSecret(context.Background(), tc.kc, tc.cfg, "argocd")
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

func TestWaitForSecret_TimesOutWhenAbsent(t *testing.T) {
	t.Parallel()

	fk := newFakeK8s() // empty store: GetSecret always returns NotFound

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// waitForSecret's PollOptions have a 120 s Timeout; we rely on the
	// context to cut it short here. The error wrapping path is the
	// "context cancelled" branch of k8s.Poll.
	err := waitForSecret(ctx, fk, "argocd", argoCDSecret)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestWaitForSecret_PropagatesTransportError(t *testing.T) {
	t.Parallel()

	fk := newFakeK8s()
	fk.getErr = errors.New("api server exploded")

	// Use a short context so the test does not hang if Poll silently
	// swallows the error (it should not — non-NotFound errors are
	// fatal).
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForSecret(ctx, fk, "argocd", argoCDSecret); err == nil {
		t.Fatalf("want error, got nil")
	}
}

// scriptedRunner implements exec.Runner by replaying a queue of
// expected commands and recording the argv it saw.
type scriptedRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) (exec.Output, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	full := append([]string{name}, args...)
	r.calls = append(r.calls, full)
	return exec.Output{}, nil
}

func (r *scriptedRunner) RunCommand(ctx context.Context, c exec.Command) (exec.Output, error) {
	return r.Run(ctx, c.Name, c.Args...)
}

func (r *scriptedRunner) Pipe(_ context.Context, _ exec.Command, _ exec.Command) (exec.Output, error) {
	return exec.Output{}, errors.New("scriptedRunner.Pipe not supported in test")
}

func TestInstallViaHelmWithValues_DispatchesExpectedCommands(t *testing.T) {
	t.Parallel()

	// Materialise a fake umbrella chart so readArgoHelmRepoURL can
	// parse Chart.lock the same way it would against the embedded
	// tree.
	chartDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.lock"), []byte(`dependencies:
- name: argo-cd
  repository: https://argoproj.github.io/argo-helm
  version: 9.5.4
`), 0o644); err != nil {
		t.Fatalf("write Chart.lock: %v", err)
	}

	runner := &scriptedRunner{}
	hc := helm.New(runner)

	err := installViaHelmWithValues(context.Background(), &hc, chartDir, "argocd", "/tmp/values.yaml")
	if err != nil {
		t.Fatalf("installViaHelmWithValues: %v", err)
	}

	want := [][]string{
		{"helm", "repo", "add", "argo", "https://argoproj.github.io/argo-helm"},
		{"helm", "dependency", "build", chartDir},
		{"helm", "upgrade", "-i", "argocd", chartDir, "--namespace", "argocd", "--values", "/tmp/values.yaml", "--create-namespace"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %v", len(runner.calls), len(want), runner.calls)
	}
	for i, w := range want {
		if !stringSlicesEqual(runner.calls[i], w) {
			t.Fatalf("call %d:\n got  %v\n want %v", i, runner.calls[i], w)
		}
	}
}

func TestReadArgoHelmRepoURL_HandlesMissingLockGracefully(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() // no Chart.lock inside
	if _, err := readArgoHelmRepoURL(dir); err == nil {
		t.Fatal("want error on missing Chart.lock, got nil")
	}
}

func TestReadArgoHelmRepoURL_ReturnsFirstDependency(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.lock"), []byte(`dependencies:
- name: argo-cd
  repository: https://example.test/argo
  version: 1.2.3
`), 0o644); err != nil {
		t.Fatalf("write Chart.lock: %v", err)
	}
	got, err := readArgoHelmRepoURL(dir)
	if err != nil {
		t.Fatalf("readArgoHelmRepoURL: %v", err)
	}
	if got != "https://example.test/argo" {
		t.Fatalf("got %q, want %q", got, "https://example.test/argo")
	}
}

// extractDataField pulls a JSON-style string field out of the merge
// patch body. The body is small enough that a brute-force substring
// search beats spinning up a real JSON decoder; if either field is
// missing the test fails loudly.
func extractDataField(t *testing.T, body, key string) string {
	t.Helper()
	needle := `"` + key + `":`
	i := strings.Index(body, needle)
	if i < 0 {
		t.Fatalf("body missing %q: %s", key, body)
	}
	rest := body[i+len(needle):]
	// Skip whitespace.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		t.Fatalf("body field %q is not a JSON string: %s", key, body)
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("body field %q has no closing quote: %s", key, body)
	}
	return rest[:end]
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
