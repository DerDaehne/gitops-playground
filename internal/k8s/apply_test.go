package k8s

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func testCtx() context.Context { return context.Background() }

func TestPatchType_ToK8sPatchType(t *testing.T) {
	tests := []struct {
		name string
		in   PatchType
		want types.PatchType
	}{
		{"empty defaults to merge", "", types.MergePatchType},
		{"explicit merge", PatchJSONMerge, types.MergePatchType},
		{"strategic", PatchStrategic, types.StrategicMergePatchType},
		{"json", PatchJSON, types.JSONPatchType},
		{"unknown falls back to merge", PatchType("bogus"), types.MergePatchType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.toK8sPatchType(); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestSplitYAMLDocs(t *testing.T) {
	body := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: b
`)
	docs, err := splitYAMLDocs(body)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 docs, got %d", len(docs))
	}
}

func TestSplitYAMLDocs_Empty(t *testing.T) {
	docs, err := splitYAMLDocs([]byte(""))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("want 0 docs, got %d", len(docs))
	}
}

func TestIsUnsupportedApply(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want bool
	}{
		{"nil", nil, false},
		{"random error", errBoom{}, false},
		{"unsupported patch type", errBoomMsg{msg: "PatchType is not supported"}, true},
		{"apply patches blocked", errBoomMsg{msg: "apply patches are not supported by this fake"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnsupportedApply(tc.in); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestApplyYAML_RequiresRealRESTConfig(t *testing.T) {
	c := newFakeClient("", "default")
	err := c.ApplyYAML(testCtx(), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"))
	if err == nil {
		t.Fatal("expected error because fake clients lack a REST config")
	}
}

// --- test helpers ---

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

type errBoomMsg struct{ msg string }

func (e errBoomMsg) Error() string { return e.msg }
