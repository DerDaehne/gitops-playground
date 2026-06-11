package k8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrConfigMapKeyNotFound is returned by GetConfigMapValue when the key
// is missing.
var ErrConfigMapKeyNotFound = errors.New("config map key not found")

// GetConfigMap returns the raw typed object. Wraps the NotFound error.
func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	ns := c.resolveNamespace(namespace)
	cm, err := c.typed.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting configmap %s/%s: %w", ns, name, err)
	}
	return cm, nil
}

// GetConfigMapValue returns a single key from a ConfigMap in the default
// namespace. Mirrors the Groovy getConfigMap(name, key) method.
func (c *Client) GetConfigMapValue(ctx context.Context, name, key string) (string, error) {
	cm, err := c.GetConfigMap(ctx, DefaultNamespace, name)
	if err != nil {
		return "", err
	}
	v, ok := cm.Data[key]
	if !ok {
		return "", fmt.Errorf("%w: configmap %q key %q", ErrConfigMapKeyNotFound, name, key)
	}
	return v, nil
}

// ApplyConfigMap create-or-replaces a ConfigMap with the given string data.
func (c *Client) ApplyConfigMap(ctx context.Context, namespace, name string, data map[string]string) error {
	if name == "" {
		return errors.New("configmap name must not be empty")
	}
	ns := c.resolveNamespace(namespace)
	cm := &corev1.ConfigMap{
		ObjectMeta: objectMeta(ns, name),
		Data:       data,
	}

	_, err := c.typed.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating configmap %s/%s: %w", ns, name, err)
	}
	// Update with resourceVersion fetched from the existing CM.
	existing, getErr := c.typed.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if getErr != nil {
		return fmt.Errorf("fetching existing configmap %s/%s: %w", ns, name, getErr)
	}
	cm.ResourceVersion = existing.ResourceVersion
	if _, err := c.typed.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating configmap %s/%s: %w", ns, name, err)
	}
	return nil
}

// ApplyConfigMapFromFile is the analog of Groovy's createConfigMapFromFile.
// The file is read in full and stored under data[filepath.Base(path)].
func (c *Client) ApplyConfigMapFromFile(ctx context.Context, namespace, name, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file %q for configmap %s: %w", path, name, err)
	}
	return c.ApplyConfigMap(ctx, namespace, name, map[string]string{
		filepath.Base(path): string(body),
	})
}
