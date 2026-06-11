package k8s

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// dockerConfigJSONKey is the canonical key inside a kubernetes.io/dockerconfigjson
// secret. Matches Groovy's DOCKER_CONFIG_JSON_KEY constant.
const dockerConfigJSONKey = ".dockerconfigjson"

// SecretCredentials mirrors the Groovy Credentials object returned by
// getCredentialsFromSecret. It only carries the decoded user/pass; the
// caller decides what to do with them.
type SecretCredentials struct {
	Username string
	Password string
}

// GetSecret returns the typed Secret. Errors are wrapped.
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	ns := c.resolveNamespace(namespace)
	sec, err := c.typed.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting secret %s/%s: %w", ns, name, err)
	}
	return sec, nil
}

// ApplyGenericSecret creates-or-replaces an Opaque secret with the given
// data. data values are stored as StringData so the API server takes care
// of base64 encoding.
//
// Behaviour matches the Groovy createSecret("generic", …) helper: the
// existing secret is deleted before being recreated, to avoid the
// "immutable field changed" trap.
func (c *Client) ApplyGenericSecret(ctx context.Context, namespace, name string, data map[string]string) error {
	return c.applySecret(ctx, namespace, name, corev1.SecretTypeOpaque, data)
}

// ApplyOpaqueSecretWithType is the typed equivalent of the Groovy
// createSecret(type, name, …) helper: callers pass the SecretType string
// ("kubernetes.io/tls", "kubernetes.io/basic-auth", …). "generic" is
// translated to corev1.SecretTypeOpaque for backwards compatibility.
func (c *Client) ApplyOpaqueSecretWithType(ctx context.Context, secretType, namespace, name string, data map[string]string) error {
	t := corev1.SecretType(secretType)
	if secretType == "generic" || secretType == "" {
		t = corev1.SecretTypeOpaque
	}
	return c.applySecret(ctx, namespace, name, t, data)
}

func (c *Client) applySecret(ctx context.Context, namespace, name string, t corev1.SecretType, data map[string]string) error {
	if name == "" {
		return errors.New("secret name must not be empty")
	}
	ns := c.resolveNamespace(namespace)
	secret := &corev1.Secret{
		ObjectMeta: objectMeta(ns, name),
		Type:       t,
		StringData: data,
	}

	if _, err := c.typed.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err == nil {
		return nil
	} else if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating secret %s/%s: %w", ns, name, err)
	}

	// Already there → delete + recreate (the Groovy code does the same).
	if err := c.typed.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting existing secret %s/%s: %w", ns, name, err)
	}
	if _, err := c.typed.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("recreating secret %s/%s: %w", ns, name, err)
	}
	return nil
}

// ApplyDockerConfigSecret creates-or-replaces a kubernetes.io/dockerconfigjson
// secret usable as an imagePullSecret. host/user/password are encoded into
// the canonical Docker auth JSON format.
func (c *Client) ApplyDockerConfigSecret(ctx context.Context, namespace, name, host, user, password string) error {
	if name == "" {
		return errors.New("secret name must not be empty")
	}
	ns := c.resolveNamespace(namespace)
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	dockerCfg := fmt.Sprintf(`{"auths":{%q:{"username":%q,"password":%q,"auth":%q}}}`,
		host, user, password, auth)

	secret := &corev1.Secret{
		ObjectMeta: objectMeta(ns, name),
		Type:       corev1.SecretTypeDockerConfigJson,
		StringData: map[string]string{dockerConfigJSONKey: dockerCfg},
	}
	return c.createOrReplaceSecret(ctx, ns, secret)
}

func (c *Client) createOrReplaceSecret(ctx context.Context, ns string, secret *corev1.Secret) error {
	_, err := c.typed.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating secret %s/%s: %w", ns, secret.Name, err)
	}
	if err := c.typed.CoreV1().Secrets(ns).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting existing secret %s/%s: %w", ns, secret.Name, err)
	}
	if _, err := c.typed.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("recreating secret %s/%s: %w", ns, secret.Name, err)
	}
	return nil
}

// DeleteSecret removes a secret. NotFound counts as success.
func (c *Client) DeleteSecret(ctx context.Context, namespace, name string) error {
	ns := c.resolveNamespace(namespace)
	if err := c.typed.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("deleting secret %s/%s: %w", ns, name, err)
	}
	return nil
}

// GetCredentialsFromSecret decodes the username/password keys of a secret
// and returns them as SecretCredentials. Default keys are "username" and
// "password" – pass empty strings to keep them.
func (c *Client) GetCredentialsFromSecret(ctx context.Context, namespace, name, usernameKey, passwordKey string) (SecretCredentials, error) {
	if usernameKey == "" {
		usernameKey = "username"
	}
	if passwordKey == "" {
		passwordKey = "password"
	}
	sec, err := c.GetSecret(ctx, namespace, name)
	if err != nil {
		return SecretCredentials{}, err
	}
	user, err := decodeKey(sec.Data, usernameKey)
	if err != nil {
		return SecretCredentials{}, fmt.Errorf("secret %s/%s: %w", namespace, name, err)
	}
	pass, err := decodeKey(sec.Data, passwordKey)
	if err != nil {
		return SecretCredentials{}, fmt.Errorf("secret %s/%s: %w", namespace, name, err)
	}
	return SecretCredentials{Username: user, Password: pass}, nil
}

// GetArgoCDNamespacesSecret waits for a secret containing the
// "namespaces" key and returns its (raw, base64-encoded) value.
// Equivalent to the Groovy getArgoCDNamespacesSecret method.
func (c *Client) GetArgoCDNamespacesSecret(ctx context.Context, namespace, name string, opts PollOptions) (string, error) {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("argo-cd namespaces secret %s/%s", c.resolveNamespace(namespace), name)
	}
	return Poll(ctx, opts, func(ctx context.Context) (string, bool, error) {
		sec, err := c.GetSecret(ctx, namespace, name)
		if err != nil {
			return "", false, err
		}
		raw, ok := sec.Data["namespaces"]
		if !ok || len(raw) == 0 {
			return "", false, nil
		}
		return string(raw), true, nil
	})
}

func decodeKey(data map[string][]byte, key string) (string, error) {
	v, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key %q missing", key)
	}
	// Secret.Data values from the API are already raw bytes (the API
	// server decodes them). No double-decoding needed.
	return string(v), nil
}
