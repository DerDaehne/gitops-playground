package k8s

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"
)

// PatchType mirrors the small subset the Groovy code used. We expose our
// own enum so callers don't pull in apimachinery just to pick a patch
// type.
type PatchType string

const (
	PatchJSONMerge PatchType = "merge"
	PatchStrategic PatchType = "strategic"
	PatchJSON      PatchType = "json"
)

// toK8sPatchType maps the public enum to apimachinery's types.PatchType.
// An empty string defaults to merge-patch (matching Groovy createPatchContext).
func (p PatchType) toK8sPatchType() types.PatchType {
	switch p {
	case PatchStrategic:
		return types.StrategicMergePatchType
	case PatchJSON:
		return types.JSONPatchType
	case PatchJSONMerge, "":
		return types.MergePatchType
	default:
		return types.MergePatchType
	}
}

// ApplyYAML decodes a (possibly multi-document) YAML stream and
// create-or-updates each resource via the dynamic client.
//
// This is the replacement for the Groovy applyYaml / applyYamlStream
// pair. Unlike the original it never falls back to kubectl.
func (c *Client) ApplyYAML(ctx context.Context, body []byte) error {
	docs, err := splitYAMLDocs(body)
	if err != nil {
		return fmt.Errorf("splitting YAML: %w", err)
	}

	mapper, err := c.restMapper()
	if err != nil {
		return err
	}

	for i, doc := range docs {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		if err := c.applyOne(ctx, mapper, doc); err != nil {
			return fmt.Errorf("applying document %d: %w", i, err)
		}
	}
	return nil
}

func (c *Client) applyOne(ctx context.Context, mapper meta.RESTMapper, doc []byte) error {
	// YAML → JSON → Unstructured.
	jsonBytes, err := yaml.YAMLToJSON(doc)
	if err != nil {
		return fmt.Errorf("yaml→json: %w", err)
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		return fmt.Errorf("unmarshalling unstructured: %w", err)
	}

	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return errors.New("manifest is missing kind")
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("no REST mapping for %s: %w", gvk.String(), err)
	}

	resourceClient := c.dyn.Resource(mapping.Resource)
	namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
	ns := obj.GetNamespace()
	if namespaced && ns == "" {
		ns = c.CurrentNamespace()
		obj.SetNamespace(ns)
	}

	var nri dynamic.ResourceInterface
	if namespaced {
		nri = resourceClient.Namespace(ns)
	}

	// Server-side apply via dynamic.Patch with FieldManager "gop".
	// This avoids the get/update race the Groovy createOrReplace had.
	patchBody, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshalling object: %w", err)
	}
	patchOpts := metav1.PatchOptions{FieldManager: "gop", Force: ptrBool(true)}
	if namespaced {
		_, err = nri.Patch(ctx, obj.GetName(), types.ApplyPatchType, patchBody, patchOpts)
	} else {
		_, err = resourceClient.Patch(ctx, obj.GetName(), types.ApplyPatchType, patchBody, patchOpts)
	}
	if err != nil {
		// Some fake clients in tests don't implement server-side apply.
		// Fall back to create-or-update.
		if isUnsupportedApply(err) {
			return c.createOrUpdate(ctx, resourceClient, nri, obj, namespaced)
		}
		return fmt.Errorf("applying %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return nil
}

func (c *Client) createOrUpdate(
	ctx context.Context,
	cluster dynamic.NamespaceableResourceInterface,
	ns dynamic.ResourceInterface,
	obj *unstructured.Unstructured,
	namespaced bool,
) error {
	create := func() error {
		var err error
		if namespaced {
			_, err = ns.Create(ctx, obj, metav1.CreateOptions{})
		} else {
			_, err = cluster.Create(ctx, obj, metav1.CreateOptions{})
		}
		return err
	}
	if err := create(); err == nil {
		return nil
	} else if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}

	// Already exists → fetch current resourceVersion and Update.
	var existing *unstructured.Unstructured
	var err error
	if namespaced {
		existing, err = ns.Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		existing, err = cluster.Get(ctx, obj.GetName(), metav1.GetOptions{})
	}
	if err != nil {
		return fmt.Errorf("fetching existing %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	if namespaced {
		_, err = ns.Update(ctx, obj, metav1.UpdateOptions{})
	} else {
		_, err = cluster.Update(ctx, obj, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("updating %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return nil
}

// Patch applies a raw patch to an arbitrary resource referenced by GVR.
// The Groovy code accepted a "resource type string" plus name; here we
// expect the caller to supply the GVR explicitly so we don't need a
// switch/case ladder over resource names.
func (c *Client) Patch(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	namespace, name string,
	patchType PatchType,
	body []byte,
) error {
	ns := c.resolveNamespace(namespace)
	_, err := c.dyn.Resource(gvr).Namespace(ns).Patch(
		ctx, name, patchType.toK8sPatchType(), body, metav1.PatchOptions{FieldManager: "gop"},
	)
	if err != nil {
		return fmt.Errorf("patching %s %s/%s: %w", gvr.Resource, ns, name, err)
	}
	return nil
}

// restMapper builds a discovery-backed RESTMapper. Returns a clear error
// when no REST config is available (which is the case for tests using
// NewWithClients).
func (c *Client) restMapper() (meta.RESTMapper, error) {
	if c.restConfig == nil {
		return nil, errors.New("ApplyYAML/Patch require a real REST config; this client was built for tests")
	}
	dc, err := discovery.NewDiscoveryClientForConfig(c.restConfig)
	if err != nil {
		return nil, fmt.Errorf("building discovery client: %w", err)
	}
	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, fmt.Errorf("fetching API group resources: %w", err)
	}
	return restmapper.NewDiscoveryRESTMapper(groups), nil
}

// splitYAMLDocs splits a multi-document YAML stream into individual
// documents. Uses apimachinery's robust splitter so comments and
// indentation are handled correctly.
func splitYAMLDocs(body []byte) ([][]byte, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(body)))
	var docs [][]byte
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func ptrBool(b bool) *bool { return &b }

// isUnsupportedApply detects fake-client patch failures so tests can
// fall through to the create/update path.
func isUnsupportedApply(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "PatchType is not supported") ||
		strings.Contains(msg, "apply patches are not supported")
}
