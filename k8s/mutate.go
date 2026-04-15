package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CordonNode marks the node as unschedulable via a merge patch.
// Uses client-go directly — no kubectl binary required.
func (c *Client) CordonNode(ctx context.Context, name string) error {
	patch := []byte(`{"spec":{"unschedulable":true}}`)
	_, err := c.kube.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("cordoning node %s: %w", name, err)
	}
	return nil
}

// UncordonNode marks the node as schedulable via a merge patch.
// Uses client-go directly — no kubectl binary required.
func (c *Client) UncordonNode(ctx context.Context, name string) error {
	patch := []byte(`{"spec":{"unschedulable":false}}`)
	_, err := c.kube.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("uncordoning node %s: %w", name, err)
	}
	return nil
}

// DrainNode cordons the node then evicts all evictable pods using the official
// k8s.io/kubectl/pkg/drain library — the same logic kubectl drain uses.
//
// --force is intentionally NOT set: bare pods (no controller) are left in place
// rather than force-deleted, which would cause permanent data loss.
// --ignore-daemonsets is set because daemonset pods cannot be evicted.
// --delete-emptydir-data is set to allow eviction of pods using emptyDir volumes.
//
// PodDisruptionBudgets are respected; the drain will wait until each pod is
// safely evicted or the context deadline is reached.
func (c *Client) DrainNode(ctx context.Context, name string) error {
	node, err := c.kube.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting node %s: %w", name, err)
	}

	helper := &drain.Helper{
		Client:                c.kube,
		Ctx:                   ctx,
		Force:                 false, // never force-delete bare pods
		GracePeriodSeconds:    -1,    // use each pod's own grace period
		IgnoreAllDaemonSets:   true,
		DeleteEmptyDirData:    true,
		Out:                   io.Discard,
		ErrOut:                io.Discard,
		OnPodDeletedOrEvicted: func(_ *corev1.Pod, _ bool) {},
	}

	// Cordon first (drain helper does not cordon automatically).
	if err := drain.RunCordonOrUncordon(helper, node, true); err != nil {
		return fmt.Errorf("cordoning before drain: %w", err)
	}

	// Evict pods — honours PodDisruptionBudgets.
	if err := drain.RunNodeDrain(helper, name); err != nil {
		return fmt.Errorf("draining node %s: %w", name, err)
	}
	return nil
}

// validateKube is used by tests to check the client is wired up.
func validateKube(kube kubernetes.Interface) bool {
	return kube != nil
}
