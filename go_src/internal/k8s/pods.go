package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetPod fetches a Pod by namespace/name. Wraps NotFound errors.
func (c *Client) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	ns := c.resolveNamespace(namespace)
	pod, err := c.typed.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting pod %s/%s: %w", ns, name, err)
	}
	return pod, nil
}

// WaitForPodPhase blocks until the pod reaches desiredPhase or the
// context/PollOptions timeout fires.
//
// Replacement for the Groovy waitForResourcePhase("pod", …) call sites.
func (c *Client) WaitForPodPhase(ctx context.Context, namespace, name string, desiredPhase corev1.PodPhase, opts PollOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("pod %s/%s to reach phase %s",
			c.resolveNamespace(namespace), name, desiredPhase)
	}
	_, err := Poll(ctx, opts, func(ctx context.Context) (struct{}, bool, error) {
		pod, err := c.GetPod(ctx, namespace, name)
		if err != nil {
			return struct{}{}, false, err
		}
		if pod.Status.Phase == desiredPhase {
			return struct{}{}, true, nil
		}
		return struct{}{}, false, nil
	})
	return err
}

// WaitForPodReady blocks until the pod's PodReady condition is true.
// This is the canonical "the pod can serve traffic" check.
func (c *Client) WaitForPodReady(ctx context.Context, namespace, name string, opts PollOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("pod %s/%s ready", c.resolveNamespace(namespace), name)
	}
	_, err := Poll(ctx, opts, func(ctx context.Context) (struct{}, bool, error) {
		pod, err := c.GetPod(ctx, namespace, name)
		if err != nil {
			return struct{}{}, false, err
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return struct{}{}, true, nil
			}
		}
		return struct{}{}, false, nil
	})
	return err
}

// WaitUntilDeploymentReady blocks until the deployment reports
// readyReplicas == spec.replicas (and at least one replica is desired).
// Replaces the multiple ad-hoc deployment waits scattered across the
// Groovy features.
func (c *Client) WaitUntilDeploymentReady(ctx context.Context, namespace, name string, opts PollOptions) error {
	if opts.Description == "" {
		opts.Description = fmt.Sprintf("deployment %s/%s ready", c.resolveNamespace(namespace), name)
	}
	ns := c.resolveNamespace(namespace)
	_, err := Poll(ctx, opts, func(ctx context.Context) (struct{}, bool, error) {
		dep, err := c.typed.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return struct{}{}, false, err
		}
		if isDeploymentReady(dep) {
			return struct{}{}, true, nil
		}
		return struct{}{}, false, nil
	})
	return err
}

func isDeploymentReady(d *appsv1.Deployment) bool {
	if d.Spec.Replicas == nil {
		return d.Status.ReadyReplicas > 0
	}
	want := *d.Spec.Replicas
	return want > 0 &&
		d.Status.ReadyReplicas == want &&
		d.Status.UpdatedReplicas == want &&
		d.Status.ObservedGeneration >= d.Generation
}
