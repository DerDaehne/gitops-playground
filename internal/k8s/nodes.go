package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// internalIPType is the AddressType reported by kubelet for the cluster-
// internal IP (mirrors the Groovy INTERNAL_IP_TYPE constant).
const internalIPType = "InternalIP"

// ListNodes returns the full node list. Read-only helper used by the
// Reader interface and a handful of features.
func (c *Client) ListNodes(ctx context.Context) (*corev1.NodeList, error) {
	list, err := c.typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	return list, nil
}

// WaitForNode blocks until at least one node is reported by the API
// server and returns its name. Equivalent to Groovy's waitForNode().
func (c *Client) WaitForNode(ctx context.Context, opts PollOptions) (string, error) {
	if opts.Description == "" {
		opts.Description = "first cluster node"
	}
	return Poll(ctx, opts, func(ctx context.Context) (string, bool, error) {
		list, err := c.typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			return "", false, err
		}
		if len(list.Items) == 0 {
			return "", false, nil
		}
		return list.Items[0].Name, true, nil
	})
}

// WaitForInternalNodeIP blocks until the first node exposes an InternalIP
// address. Equivalent to Groovy's waitForInternalNodeIp().
func (c *Client) WaitForInternalNodeIP(ctx context.Context, opts PollOptions) (string, error) {
	nodeName, err := c.WaitForNode(ctx, opts)
	if err != nil {
		return "", err
	}
	ipOpts := opts
	if ipOpts.Description == "" {
		ipOpts.Description = fmt.Sprintf("internal IP of node %s", nodeName)
	}
	return Poll(ctx, ipOpts, func(ctx context.Context) (string, bool, error) {
		node, err := c.typed.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return "", false, err
		}
		for _, addr := range node.Status.Addresses {
			if string(addr.Type) == internalIPType && addr.Address != "" {
				return addr.Address, true, nil
			}
		}
		return "", false, nil
	})
}
