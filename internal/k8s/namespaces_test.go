package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestEnsureNamespace(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		existing  []string
		wantErr   bool
		wantErrIs error
	}{
		{name: "empty name", input: "", wantErr: true, wantErrIs: ErrInvalidNamespace},
		{name: "whitespace name", input: "   ", wantErr: true, wantErrIs: ErrInvalidNamespace},
		{name: "create new namespace", input: "argocd"},
		{name: "idempotent on existing", input: "argocd", existing: []string{"argocd"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(tc.existing))
			for _, n := range tc.existing {
				objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: n}})
			}
			c := newFakeClient("", "", objs...)
			err := c.EnsureNamespace(context.Background(), tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("expected error %v, got %v", tc.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, getErr := c.typed.CoreV1().Namespaces().Get(context.Background(), tc.input, metav1.GetOptions{}); getErr != nil {
				t.Fatalf("namespace not created: %v", getErr)
			}
		})
	}
}

func TestNamespaceExists(t *testing.T) {
	c := newFakeClient("", "", &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "existing"}})
	tests := []struct {
		in   string
		want bool
	}{
		{"existing", true},
		{"missing", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := c.NamespaceExists(context.Background(), tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestDeleteNamespace_NotFoundIsSuccess(t *testing.T) {
	c := newFakeClient("", "")
	if err := c.DeleteNamespace(context.Background(), "missing"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestEnsureNamespaces_StopsOnFirstError(t *testing.T) {
	c := newFakeClient("", "")
	err := c.EnsureNamespaces(context.Background(), []string{"good", "", "never"})
	if err == nil {
		t.Fatal("expected error from empty namespace name")
	}
	// "good" should have been created before the error.
	if _, err := c.typed.CoreV1().Namespaces().Get(context.Background(), "good", metav1.GetOptions{}); err != nil {
		t.Fatalf("good namespace not created: %v", err)
	}
	// "never" must NOT exist (we stopped before).
	if _, err := c.typed.CoreV1().Namespaces().Get(context.Background(), "never", metav1.GetOptions{}); err == nil {
		t.Fatal("'never' namespace was created despite the earlier error")
	}
}

func TestNamespaceAnnotation(t *testing.T) {
	withAnn := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-app",
			Annotations: map[string]string{
				OpenShiftUIDRangeAnnotation: "1000700000/10000",
				"other.example.com/key":     "hello",
			},
		},
	}
	noAnn := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "plain"}}
	c := newFakeClient("", "", withAnn, noAnn)

	tests := []struct {
		name      string
		namespace string
		key       string
		want      string
		wantErr   bool
		wantErrIs error
	}{
		{name: "present annotation", namespace: "openshift-app", key: OpenShiftUIDRangeAnnotation, want: "1000700000/10000"},
		{name: "other annotation", namespace: "openshift-app", key: "other.example.com/key", want: "hello"},
		{name: "missing annotation returns empty + nil", namespace: "openshift-app", key: "not.set", want: ""},
		{name: "namespace has no annotations map", namespace: "plain", key: OpenShiftUIDRangeAnnotation, want: ""},
		{name: "empty namespace name", namespace: "", key: "anything", wantErr: true, wantErrIs: ErrInvalidNamespace},
		{name: "namespace not found", namespace: "ghost", key: OpenShiftUIDRangeAnnotation, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.NamespaceAnnotation(context.Background(), tc.namespace, tc.key)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("expected error %v, got %v", tc.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestParseOpenShiftUIDRange(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantID int
		wantOK bool
	}{
		{name: "canonical openshift form", in: "1000700000/10000", wantID: 1000700000, wantOK: true},
		{name: "small uid", in: "1000/10", wantID: 1000, wantOK: true},
		{name: "no slash falls back to the whole value", in: "12345", wantID: 12345, wantOK: true},
		{name: "leading/trailing whitespace", in: "  1000700000/10000  ", wantID: 1000700000, wantOK: true},
		{name: "empty string", in: "", wantID: 0, wantOK: false},
		{name: "whitespace only", in: "   ", wantID: 0, wantOK: false},
		{name: "slash only", in: "/10000", wantID: 0, wantOK: false},
		{name: "non-integer left side", in: "abc/10000", wantID: 0, wantOK: false},
		{name: "negative uid is rejected only when not an int", in: "-1/10", wantID: -1, wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := ParseOpenShiftUIDRange(tc.in)
			if gotID != tc.wantID || gotOK != tc.wantOK {
				t.Fatalf("ParseOpenShiftUIDRange(%q) = (%d, %v), want (%d, %v)",
					tc.in, gotID, gotOK, tc.wantID, tc.wantOK)
			}
		})
	}
}
