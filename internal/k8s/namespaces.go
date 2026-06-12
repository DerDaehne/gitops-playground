package k8s

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrInvalidNamespace is returned by EnsureNamespace when name is empty.
var ErrInvalidNamespace = errors.New("namespace name must not be empty")

// OpenShiftUIDRangeAnnotation is the namespace annotation set by the
// OpenShift SCC admission controller. Its value has the shape
// "<startUID>/<size>", e.g. "1000700000/10000". Mirrors the Groovy
// Monitoring.findValidOpenShiftUid lookup.
const OpenShiftUIDRangeAnnotation = "openshift.io/sa.scc.uid-range"

// GetNamespace returns the Namespace object or wraps a NotFound error.
func (c *Client) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns, err := c.typed.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting namespace %q: %w", name, err)
	}
	return ns, nil
}

// NamespaceExists is the boolean form of GetNamespace. Returns false for
// NotFound errors and any other error verbatim.
func (c *Client) NamespaceExists(ctx context.Context, name string) (bool, error) {
	_, err := c.typed.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking namespace %q: %w", name, err)
}

// EnsureNamespace creates the namespace if absent. Idempotent.
func (c *Client) EnsureNamespace(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidNamespace
	}
	exists, err := c.NamespaceExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	ns := &corev1.Namespace{ObjectMeta: objectMeta("", name)}
	if _, err := c.typed.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating namespace %q: %w", name, err)
	}
	return nil
}

// EnsureNamespaces creates each name in order. Any individual failure
// stops the iteration and is returned.
func (c *Client) EnsureNamespaces(ctx context.Context, names []string) error {
	for _, name := range names {
		if err := c.EnsureNamespace(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// NamespaceAnnotation returns the value of a single annotation on the
// namespace, or ("", nil) when the annotation is absent. A missing
// namespace, or any other error from the API server, is wrapped and
// returned verbatim. Mirrors K8sClient.getAnnotation('namespace', …) from
// the Groovy original.
func (c *Client) NamespaceAnnotation(ctx context.Context, namespace, key string) (string, error) {
	if strings.TrimSpace(namespace) == "" {
		return "", ErrInvalidNamespace
	}
	ns, err := c.typed.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting namespace %q for annotation %q: %w", namespace, key, err)
	}
	if ns.Annotations == nil {
		return "", nil
	}
	return ns.Annotations[key], nil
}

// ParseOpenShiftUIDRange parses the value of the
// openshift.io/sa.scc.uid-range annotation ("1000700000/10000") and
// returns the start UID. ok is false for empty input, missing slash, or
// a non-integer left side. The Groovy original throws when the value is
// empty; callers (the runner) decide whether to surface that as an error
// or to fall back to "no UID set".
func ParseOpenShiftUIDRange(value string) (int, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, false
	}
	slash := strings.IndexByte(v, '/')
	var head string
	if slash < 0 {
		head = v
	} else {
		head = v[:slash]
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return 0, false
	}
	uid, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}
	return uid, true
}

// DeleteNamespace removes the namespace. NotFound is treated as success.
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidNamespace
	}
	if err := c.typed.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("deleting namespace %q: %w", name, err)
	}
	return nil
}
