package k8s

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyConfigMap_CreateThenUpdate(t *testing.T) {
	c := newFakeClient("", "default")
	ctx := context.Background()

	if err := c.ApplyConfigMap(ctx, "default", "settings", map[string]string{"k1": "v1"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := c.ApplyConfigMap(ctx, "default", "settings", map[string]string{"k1": "v1-updated", "k2": "v2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := c.typed.CoreV1().ConfigMaps("default").Get(ctx, "settings", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Data["k1"] != "v1-updated" || got.Data["k2"] != "v2" {
		t.Fatalf("update failed: %#v", got.Data)
	}
}

func TestApplyConfigMap_RejectsEmptyName(t *testing.T) {
	c := newFakeClient("", "default")
	if err := c.ApplyConfigMap(context.Background(), "default", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetConfigMapValue(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mymap", Namespace: DefaultNamespace},
		Data:       map[string]string{"present": "yes"},
	}
	c := newFakeClient("", "", cm)
	ctx := context.Background()

	tests := []struct {
		name    string
		key     string
		want    string
		wantErr error
	}{
		{name: "key exists", key: "present", want: "yes"},
		{name: "key missing", key: "absent", wantErr: ErrConfigMapKeyNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.GetConfigMapValue(ctx, "mymap", tc.key)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestApplyConfigMapFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte("hello: world\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	c := newFakeClient("", "default")
	if err := c.ApplyConfigMapFromFile(context.Background(), "default", "from-file", path); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got, err := c.typed.CoreV1().ConfigMaps("default").Get(context.Background(), "from-file", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Data["values.yaml"] != "hello: world\n" {
		t.Fatalf("unexpected data: %#v", got.Data)
	}
}
