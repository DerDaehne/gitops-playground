package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWaitForNode_PicksFirstNode(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "k3d-server-0"}}
	c := newFakeClient("", "", node)
	name, err := c.WaitForNode(context.Background(), PollOptions{
		Interval: 5 * time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if name != "k3d-server-0" {
		t.Fatalf("want k3d-server-0, got %q", name)
	}
}

func TestWaitForNode_TimesOutWhenEmpty(t *testing.T) {
	c := newFakeClient("", "")
	_, err := c.WaitForNode(context.Background(), PollOptions{
		Interval: 5 * time.Millisecond, Timeout: 30 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestWaitForInternalNodeIP(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "k3d-server-0"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeHostName, Address: "host"},
				{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
			},
		},
	}
	c := newFakeClient("", "", node)
	ip, err := c.WaitForInternalNodeIP(context.Background(), PollOptions{
		Interval: 5 * time.Millisecond, Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ip != "10.0.0.5" {
		t.Fatalf("want 10.0.0.5, got %q", ip)
	}
}

func TestListNodes_EmptyCluster(t *testing.T) {
	c := newFakeClient("", "")
	list, err := c.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("want 0 nodes, got %d", len(list.Items))
	}
}
