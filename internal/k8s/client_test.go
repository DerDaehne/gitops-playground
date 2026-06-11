package k8s

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dyfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

// testScheme returns the canonical client-go scheme; sufficient for the
// fake dynamic client's tracker.
func testScheme() *runtime.Scheme {
	return scheme.Scheme
}

func newFakeClient(contextName, namespace string, objects ...runtime.Object) *Client {
	typed := fake.NewSimpleClientset(objects...)
	dyn := dyfake.NewSimpleDynamicClient(testScheme())
	return NewWithClients(typed, dyn, contextName, namespace)
}

func TestCurrentNamespace_FallbackToDefault(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty namespace falls back to default", "", DefaultNamespace},
		{"explicit namespace is kept", "argocd", "argocd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient("test-ctx", tc.in)
			if got := c.CurrentNamespace(); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCurrentContext_DefaultsWhenEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"unset returns sentinel", "", "(current context not set)"},
		{"explicit value is returned verbatim", "k3d-gop", "k3d-gop"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient(tc.in, "")
			if got := c.CurrentContext(); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestResolveNamespace(t *testing.T) {
	c := newFakeClient("", "explicit-ns")
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to client default", "", "explicit-ns"},
		{"non-empty is kept", "other", "other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.resolveNamespace(tc.in); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
