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
