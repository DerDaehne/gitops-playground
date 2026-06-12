package wire

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dyfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
)

func newFakeK8sClient(t *testing.T, objects ...runtime.Object) *k8s.Client {
	t.Helper()
	typed := fake.NewSimpleClientset(objects...)
	dyn := dyfake.NewSimpleDynamicClient(scheme.Scheme)
	return k8s.NewWithClients(typed, dyn, "test-ctx", "default")
}

func TestPersistConfig_NilClient_ReturnsErrorNotPanic(t *testing.T) {
	fn := persistConfig(nil)
	cfg := &config.Config{}
	cfg.Application.Password = "topsecret"

	err := fn(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for nil k8s client, got nil")
	}
	if !strings.Contains(err.Error(), "k8s client not initialised") {
		t.Fatalf("error %q does not mention nil k8s client", err)
	}
}

func TestPersistConfig_WritesSecret_DefaultNamespace(t *testing.T) {
	k := newFakeK8sClient(t)
	fn := persistConfig(k)

	cfg := &config.Config{}
	cfg.Application.Password = "shh"
	// GopNamespace intentionally empty → must fall back to "gop-job".

	if err := fn(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	assertGopConfigSecret(t, k, defaultGopNamespace, "shh")
}

func TestPersistConfig_WritesSecret_CustomNamespace(t *testing.T) {
	k := newFakeK8sClient(t)
	fn := persistConfig(k)

	cfg := &config.Config{}
	cfg.Application.Password = "alpha"
	cfg.Application.GopNamespace = "gop-elsewhere"

	if err := fn(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	assertGopConfigSecret(t, k, "gop-elsewhere", "alpha")
}

func TestPersistConfig_OverwritesExistingSecret(t *testing.T) {
	// Pre-seed an existing secret to exercise the delete+recreate path
	// in ApplyGenericSecret.
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gopConfigurationSecret,
			Namespace: defaultGopNamespace,
		},
		StringData: map[string]string{"gop-initial-password": "stale"},
	}
	k := newFakeK8sClient(t, existing)
	fn := persistConfig(k)

	cfg := &config.Config{}
	cfg.Application.Password = "fresh"

	if err := fn(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfig: %v", err)
	}

	assertGopConfigSecret(t, k, defaultGopNamespace, "fresh")
}

// assertGopConfigSecret checks the secret exists with both expected keys.
func assertGopConfigSecret(t *testing.T, k *k8s.Client, ns, wantPassword string) {
	t.Helper()
	// Namespace must have been created.
	if _, err := k.Typed().CoreV1().Namespaces().Get(context.Background(), ns, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			t.Fatalf("namespace %q was not created", ns)
		}
		t.Fatalf("get namespace %q: %v", ns, err)
	}
	sec, err := k.GetSecret(context.Background(), ns, gopConfigurationSecret)
	if err != nil {
		t.Fatalf("get secret %s/%s: %v", ns, gopConfigurationSecret, err)
	}
	// Fake clientset preserves StringData verbatim; the real API server
	// would move it into Data after encoding.
	got := sec.StringData["gop-initial-password"]
	if got != wantPassword {
		t.Errorf("gop-initial-password: want %q, got %q", wantPassword, got)
	}
	yamlBlob, ok := sec.StringData["gop-config"]
	if !ok || yamlBlob == "" {
		t.Errorf("gop-config key missing or empty in StringData")
	}
}
