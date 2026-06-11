// Package k8s is the Go counterpart of the 1.224-LOC Groovy K8sClient.
// Instead of a single monolith it is split into a handful of files, each
// responsible for one Kubernetes domain (nodes, namespaces, secrets, …).
//
// Differences vs. the Groovy K8sClient:
//
//   - No instance fields SLEEPTIME / DEFAULT_RETRIES. Wait timings come from
//     PollOptions / context.Context. See wait.go.
//   - No silent kubectl subprocess fallback. The Groovy code occasionally
//     shelled out to kubectl when Fabric8 was awkward; here we use
//     client-go consistently. If something cannot be expressed with
//     client-go we surface an error instead of hiding it.
//   - All errors are wrapped with fmt.Errorf("…: %w", err); helpers used
//     by callers can therefore be inspected via errors.Is / errors.As.
//   - Reader/Writer interfaces (below) let feature packages depend on a
//     narrow surface; tests inject the fake clientsets from client-go.
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultNamespace is used whenever a caller passes an empty namespace.
const DefaultNamespace = "default"

// Reader is the small read-only surface tests and callers depend on.
// It is intentionally minimal; callers that need more should depend on
// the concrete Client.
type Reader interface {
	GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error)
	GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error)
	GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error)
	GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error)
	ListNodes(ctx context.Context) (*corev1.NodeList, error)
	CurrentContext() string
	CurrentNamespace() string
}

// Writer is the small write surface. Apply/Patch live in apply.go and
// use the dynamic client under the hood.
type Writer interface {
	EnsureNamespace(ctx context.Context, name string) error
	DeleteNamespace(ctx context.Context, name string) error
	ApplyGenericSecret(ctx context.Context, namespace, name string, data map[string]string) error
	ApplyDockerConfigSecret(ctx context.Context, namespace, name, host, user, password string) error
	DeleteSecret(ctx context.Context, namespace, name string) error
	ApplyConfigMap(ctx context.Context, namespace, name string, data map[string]string) error
	ApplyYAML(ctx context.Context, yaml []byte) error
}

// Compile-time guarantee that the production Client satisfies both
// interfaces.
var (
	_ Reader = (*Client)(nil)
	_ Writer = (*Client)(nil)
)

// Client is the concrete production implementation. It owns a typed
// clientset for the core API groups and a dynamic client for arbitrary
// resources (used by Apply/Patch).
type Client struct {
	typed       kubernetes.Interface
	dyn         dynamic.Interface
	restConfig  *rest.Config
	contextName string
	namespace   string
}

// Options tunes how New picks up a kubeconfig.
type Options struct {
	// KubeconfigPath overrides KUBECONFIG / ~/.kube/config when non-empty.
	KubeconfigPath string
	// Context selects a non-default context from the kubeconfig.
	Context string
	// Namespace overrides the namespace from the context (still falls back
	// to "default" if both are empty).
	Namespace string
	// InCluster forces use of the in-cluster service-account config and
	// ignores any kubeconfig. When false we still fall back to in-cluster
	// if no kubeconfig is found.
	InCluster bool
}

// New constructs a Client. It tries (in order):
//  1. opts.InCluster == true → rest.InClusterConfig
//  2. opts.KubeconfigPath if provided
//  3. $KUBECONFIG env var (may be a list of paths)
//  4. ~/.kube/config
//  5. in-cluster config (when running inside a pod)
func New(opts Options) (*Client, error) {
	cfg, ctxName, ns, err := loadRESTConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("loading kube config: %w", err)
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building typed clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building dynamic client: %w", err)
	}
	if opts.Namespace != "" {
		ns = opts.Namespace
	}
	if ns == "" {
		ns = DefaultNamespace
	}
	return &Client{
		typed:       typed,
		dyn:         dyn,
		restConfig:  cfg,
		contextName: ctxName,
		namespace:   ns,
	}, nil
}

// NewWithClients is a test-only constructor that wires in fake clientsets.
// It does not perform any network IO.
func NewWithClients(typed kubernetes.Interface, dyn dynamic.Interface, contextName, namespace string) *Client {
	if namespace == "" {
		namespace = DefaultNamespace
	}
	return &Client{
		typed:       typed,
		dyn:         dyn,
		contextName: contextName,
		namespace:   namespace,
	}
}

// Typed exposes the underlying clientset for callers that need raw access.
// Prefer the typed helpers in this package whenever possible.
func (c *Client) Typed() kubernetes.Interface { return c.typed }

// Dynamic exposes the dynamic client (used by apply.go and tests).
func (c *Client) Dynamic() dynamic.Interface { return c.dyn }

// RESTConfig returns the loaded REST config (nil in fake-client tests).
func (c *Client) RESTConfig() *rest.Config { return c.restConfig }

// CurrentContext returns the kubeconfig context the client was built from,
// or "(current context not set)" – mirroring the Groovy fallback string –
// when no context was resolved (e.g. in-cluster mode).
func (c *Client) CurrentContext() string {
	if c.contextName == "" {
		return "(current context not set)"
	}
	return c.contextName
}

// CurrentNamespace returns the namespace the client was configured with.
func (c *Client) CurrentNamespace() string {
	if c.namespace == "" {
		return DefaultNamespace
	}
	return c.namespace
}

// resolveNamespace mirrors the Groovy resolveNamespace() helper: empty
// strings collapse to the configured default namespace.
func (c *Client) resolveNamespace(ns string) string {
	if ns == "" {
		return c.CurrentNamespace()
	}
	return ns
}

// loadRESTConfig encapsulates the kubeconfig lookup so it can be tested.
func loadRESTConfig(opts Options) (*rest.Config, string, string, error) {
	if opts.InCluster {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, "", "", fmt.Errorf("in-cluster config: %w", err)
		}
		return cfg, "", "", nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.KubeconfigPath != "" {
		loadingRules.ExplicitPath = opts.KubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		// If we get here without an explicit kubeconfig and aren't in
		// cluster, fall back to InClusterConfig as a last resort, mirroring
		// the Fabric8 auto-detection.
		if opts.KubeconfigPath == "" && !pathExists(defaultKubeconfigPath()) {
			if inCfg, inErr := rest.InClusterConfig(); inErr == nil {
				return inCfg, "", "", nil
			}
		}
		return nil, "", "", fmt.Errorf("building REST config: %w", err)
	}

	// Resolve the context name (best-effort).
	rawCfg, rawErr := clientCfg.RawConfig()
	ctxName := ""
	ns := ""
	if rawErr == nil {
		if overrides.CurrentContext != "" {
			ctxName = overrides.CurrentContext
		} else {
			ctxName = rawCfg.CurrentContext
		}
		if ctx, ok := rawCfg.Contexts[ctxName]; ok && ctx != nil {
			ns = ctx.Namespace
		}
	}
	if nsFromCfg, _, nsErr := clientCfg.Namespace(); nsErr == nil && nsFromCfg != "" {
		ns = nsFromCfg
	}
	return restCfg, ctxName, ns, nil
}

func defaultKubeconfigPath() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// objectMeta builds a metav1.ObjectMeta with the given name/namespace.
// Kept in client.go because nearly every file uses it.
func objectMeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace}
}

// gvrFor is a small helper used by apply.go and the tests to assemble a
// GroupVersionResource from string parts.
func gvrFor(group, version, resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}
