package k8s

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyGenericSecret(t *testing.T) {
	tests := []struct {
		name     string
		existing *corev1.Secret
		data     map[string]string
		wantErr  bool
	}{
		{
			name: "create new opaque secret",
			data: map[string]string{"user": "admin", "pass": "s3cret"},
		},
		{
			name: "replace existing secret with same name",
			existing: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default"},
				Data:       map[string][]byte{"oldKey": []byte("v")},
			},
			data: map[string]string{"user": "admin"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient("", "default")
			if tc.existing != nil {
				if _, err := c.typed.CoreV1().Secrets("default").Create(context.Background(), tc.existing, metav1.CreateOptions{}); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			err := c.ApplyGenericSecret(context.Background(), "default", "mysecret", tc.data)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v got %v", tc.wantErr, err)
			}
			got, getErr := c.typed.CoreV1().Secrets("default").Get(context.Background(), "mysecret", metav1.GetOptions{})
			if getErr != nil {
				t.Fatalf("get: %v", getErr)
			}
			if got.Type != corev1.SecretTypeOpaque {
				t.Errorf("want type Opaque, got %s", got.Type)
			}
			if len(got.StringData) != len(tc.data) {
				t.Errorf("want %d keys, got %d", len(tc.data), len(got.StringData))
			}
		})
	}
}

func TestApplyOpaqueSecretWithType_RewritesGeneric(t *testing.T) {
	c := newFakeClient("", "default")
	if err := c.ApplyOpaqueSecretWithType(context.Background(), "generic", "default", "s", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got, err := c.typed.CoreV1().Secrets("default").Get(context.Background(), "s", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != corev1.SecretTypeOpaque {
		t.Fatalf("want Opaque, got %s", got.Type)
	}
}

func TestApplyDockerConfigSecret(t *testing.T) {
	c := newFakeClient("", "default")
	if err := c.ApplyDockerConfigSecret(context.Background(), "", "regcred", "registry.example.com", "alice", "passw0rd"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got, err := c.typed.CoreV1().Secrets("default").Get(context.Background(), "regcred", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("want dockerconfigjson type, got %s", got.Type)
	}
	body, ok := got.StringData[dockerConfigJSONKey]
	if !ok {
		t.Fatal(".dockerconfigjson key missing")
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("alice:passw0rd"))
	if !strings.Contains(body, wantAuth) {
		t.Errorf("auth blob missing from %s", body)
	}
}

func TestApplyGenericSecret_RejectsEmptyName(t *testing.T) {
	c := newFakeClient("", "default")
	err := c.ApplyGenericSecret(context.Background(), "default", "", nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestDeleteSecret_NotFoundOK(t *testing.T) {
	c := newFakeClient("", "default")
	if err := c.DeleteSecret(context.Background(), "default", "missing"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestGetCredentialsFromSecret(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("alice"),
			"password": []byte("hunter2"),
		},
	}
	c := newFakeClient("", "ns", sec)
	cred, err := c.GetCredentialsFromSecret(context.Background(), "ns", "creds", "", "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cred.Username != "alice" || cred.Password != "hunter2" {
		t.Fatalf("unexpected credentials: %+v", cred)
	}
}

func TestGetCredentialsFromSecret_MissingKey(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "ns"},
		Data:       map[string][]byte{"username": []byte("alice")},
	}
	c := newFakeClient("", "ns", sec)
	_, err := c.GetCredentialsFromSecret(context.Background(), "ns", "creds", "", "")
	if err == nil {
		t.Fatal("expected error for missing password key")
	}
}
