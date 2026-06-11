package k8s

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWaitForPodPhase(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := newFakeClient("", "default", pod)
	err := c.WaitForPodPhase(context.Background(), "default", "p", corev1.PodRunning,
		PollOptions{Interval: 5 * time.Millisecond, Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestWaitForPodPhase_TimesOutOnWrongPhase(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	c := newFakeClient("", "default", pod)
	err := c.WaitForPodPhase(context.Background(), "default", "p", corev1.PodRunning,
		PollOptions{Interval: 5 * time.Millisecond, Timeout: 30 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestWaitForPodReady(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	c := newFakeClient("", "default", pod)
	if err := c.WaitForPodReady(context.Background(), "default", "p",
		PollOptions{Interval: 5 * time.Millisecond, Timeout: 500 * time.Millisecond}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestIsDeploymentReady(t *testing.T) {
	one := int32(1)
	tests := []struct {
		name string
		in   *appsv1.Deployment
		want bool
	}{
		{
			name: "ready",
			in: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "d", Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: &one},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 1, UpdatedReplicas: 1, ObservedGeneration: 2,
				},
			},
			want: true,
		},
		{
			name: "not enough ready replicas",
			in: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: &one},
				Status: appsv1.DeploymentStatus{ReadyReplicas: 0, UpdatedReplicas: 0},
			},
			want: false,
		},
		{
			name: "stale generation",
			in: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 3},
				Spec:       appsv1.DeploymentSpec{Replicas: &one},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 1, UpdatedReplicas: 1, ObservedGeneration: 2,
				},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDeploymentReady(tc.in); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestWaitUntilDeploymentReady(t *testing.T) {
	one := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "argo", Namespace: "argocd", Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1, UpdatedReplicas: 1, ObservedGeneration: 1,
		},
	}
	c := newFakeClient("", "argocd", dep)
	if err := c.WaitUntilDeploymentReady(context.Background(), "argocd", "argo",
		PollOptions{Interval: 5 * time.Millisecond, Timeout: 500 * time.Millisecond}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
