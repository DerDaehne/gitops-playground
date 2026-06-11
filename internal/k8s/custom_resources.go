package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ListCustomResources returns every instance of the given GVR across the
// cluster (when namespace is empty) or in a single namespace.
//
// Replaces the Groovy K8sClient.getCustomResource(String) helper, which
// was tied to a kubectl subprocess. The Go version uses the dynamic
// client, with no shell-out and proper error wrapping.
func (c *Client) ListCustomResources(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	res := c.dyn.Resource(gvr)
	if namespace != "" {
		return res.Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	return res.List(ctx, metav1.ListOptions{})
}

// DeleteCustomResource removes a single namespaced or cluster-scoped
// resource. NotFound is treated as success so destroy is idempotent.
func (c *Client) DeleteCustomResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	res := c.dyn.Resource(gvr)
	var err error
	if namespace != "" {
		err = res.Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		err = res.Delete(ctx, name, metav1.DeleteOptions{})
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting %s %s/%s: %w", gvr.Resource, namespace, name, err)
	}
	return nil
}

// Common GVRs the destroy path uses. Exported so callers don't have to
// hardcode them.
var (
	ArgoCDApplicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	ArgoCDAppProjectGVR  = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "appprojects"}
)
