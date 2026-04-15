package k8s

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"knv/model"
)

// ── resolveStatus ─────────────────────────────────────────────────────────────

func makeNode(ready corev1.ConditionStatus, unschedulable bool) corev1.Node {
	return corev1.Node{
		Spec: corev1.NodeSpec{Unschedulable: unschedulable},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: ready},
			},
		},
	}
}

func TestResolveStatus(t *testing.T) {
	tests := []struct {
		name          string
		ready         corev1.ConditionStatus
		unschedulable bool
		want          model.Status
	}{
		{"ready", corev1.ConditionTrue, false, model.StatusReady},
		{"not-ready", corev1.ConditionFalse, false, model.StatusNotReady},
		{"unknown", corev1.ConditionUnknown, false, model.StatusUnknown},
		{"ready+cordoned", corev1.ConditionTrue, true, model.StatusReadySchedulingDisabled},
		{"not-ready+cordoned", corev1.ConditionFalse, true, model.StatusNotReadySchedulingDisabled},
		{"unknown+cordoned", corev1.ConditionUnknown, true, model.StatusUnknownSchedulingDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveStatus(makeNode(tt.ready, tt.unschedulable))
			if got != tt.want {
				t.Errorf("resolveStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── resolveNodeGroup ──────────────────────────────────────────────────────────

func TestResolveNodeGroup(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "eks nodegroup only",
			labels: map[string]string{
				"eks.amazonaws.com/nodegroup": "workers-prod",
			},
			want: "workers-prod",
		},
		{
			name: "eks long and short — picks shortest",
			labels: map[string]string{
				"eks.amazonaws.com/nodegroup":    "workers-prod-20240101-abc123",
				"alpha.eksctl.io/nodegroup-name": "workers-prod",
			},
			want: "workers-prod",
		},
		{
			name:   "gke nodepool",
			labels: map[string]string{"cloud.google.com/gke-nodepool": "default-pool"},
			want:   "default-pool",
		},
		{
			name:   "no nodegroup label",
			labels: map[string]string{"kubernetes.io/arch": "amd64"},
			want:   "",
		},
		{
			name:   "fuzzy match nodegroup key",
			labels: map[string]string{"my-company.io/nodegroup": "batch"},
			want:   "batch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNodeGroup(tt.labels)
			if got != tt.want {
				t.Errorf("resolveNodeGroup() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── resolveCapacityType ───────────────────────────────────────────────────────

func TestResolveCapacityType(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "eks ON_DEMAND",
			labels: map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"},
			want:   "on-demand",
		},
		{
			name:   "eks SPOT",
			labels: map[string]string{"eks.amazonaws.com/capacityType": "SPOT"},
			want:   "spot",
		},
		{
			name:   "karpenter on-demand",
			labels: map[string]string{"karpenter.sh/capacity-type": "on-demand"},
			want:   "on-demand",
		},
		{
			name:   "karpenter spot",
			labels: map[string]string{"karpenter.sh/capacity-type": "spot"},
			want:   "spot",
		},
		{
			name:   "gke spot label presence",
			labels: map[string]string{"cloud.google.com/gke-spot": "true"},
			want:   "spot",
		},
		{
			name:   "no capacity label",
			labels: map[string]string{"kubernetes.io/arch": "amd64"},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCapacityType(tt.labels)
			if got != tt.want {
				t.Errorf("resolveCapacityType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── formatAge ─────────────────────────────────────────────────────────────────

func TestFormatAge(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"45 days", now.Add(-45 * 24 * time.Hour), "45d"},
		{"1 day", now.Add(-24 * time.Hour), "1d"},
		{"3 hours", now.Add(-3 * time.Hour), "3h"},
		{"30 minutes", now.Add(-30 * time.Minute), "30m"},
		{"1 year", now.Add(-365 * 24 * time.Hour), "1y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.t)
			if got != tt.want {
				t.Errorf("formatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── karpenterStatusFromNodeClaim ─────────────────────────────────────────────

func TestKarpenterStatusFromNodeClaim_blocked(t *testing.T) {
	nc := unstructuredNC(map[string]interface{}{}, map[string]string{
		"karpenter.sh/do-not-disrupt": "true",
	}, nil, false)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Blocked" {
		t.Errorf("got %q, want Blocked", got.status)
	}
}

func TestKarpenterStatusFromNodeClaim_disrupting_via_deletionTimestamp(t *testing.T) {
	nc := unstructuredNC(map[string]interface{}{}, nil, nil, true)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Disrupting" {
		t.Errorf("got %q, want Disrupting", got.status)
	}
}

func TestKarpenterStatusFromNodeClaim_drifted_condition(t *testing.T) {
	conditions := []interface{}{
		map[string]interface{}{"type": "Drifted", "status": "True", "reason": "AMIDrift"},
	}
	nc := unstructuredNC(map[string]interface{}{
		"status": map[string]interface{}{"conditions": conditions},
	}, nil, nil, false)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Drifted" {
		t.Errorf("got %q, want Drifted", got.status)
	}
	if got := karpenterStatusFromNodeClaim(nc); got.reason != "AMIDrift" {
		t.Errorf("reason = %q, want AMIDrift", got.reason)
	}
}

func TestKarpenterStatusFromNodeClaim_lifecycle_condition(t *testing.T) {
	conditions := []interface{}{
		map[string]interface{}{"type": "Registered", "status": "True", "lastTransitionTime": "2026-04-15T08:29:56Z"},
		map[string]interface{}{"type": "Initialized", "status": "True", "lastTransitionTime": "2026-04-15T08:30:12Z"},
	}
	nc := unstructuredNC(map[string]interface{}{
		"status": map[string]interface{}{"conditions": conditions},
	}, nil, nil, false)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Initialized" {
		t.Errorf("got %q, want Initialized", got.status)
	}
}

func TestKarpenterStatusFromNodeClaim_latest_transition_wins(t *testing.T) {
	conditions := []interface{}{
		map[string]interface{}{"type": "Initialized", "status": "True", "lastTransitionTime": "2026-04-15T08:29:56Z"},
		map[string]interface{}{"type": "Consolidatable", "status": "True", "lastTransitionTime": "2026-04-15T08:31:30Z", "reason": "Consolidatable"},
		map[string]interface{}{"type": "Ready", "status": "True", "lastTransitionTime": "2026-04-15T08:29:56Z"},
	}
	nc := unstructuredNC(map[string]interface{}{
		"status": map[string]interface{}{"conditions": conditions},
	}, nil, nil, false)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Consolidatable" {
		t.Errorf("got %q, want Consolidatable", got.status)
	}
}

func TestKarpenterStatusFromNodeClaim_eligible(t *testing.T) {
	nc := unstructuredNC(map[string]interface{}{}, nil, nil, false)
	if got := karpenterStatusFromNodeClaim(nc); got.status != "Eligible" {
		t.Errorf("got %q, want Eligible", got.status)
	}
}

// unstructuredNC builds a minimal unstructured NodeClaim for testing.
func unstructuredNC(spec map[string]interface{}, annotations map[string]string, _ map[string]string, withDeletionTS bool) unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "karpenter.sh/v1",
		"kind":       "NodeClaim",
		"metadata":   map[string]interface{}{"name": "test-nc"},
	}
	for k, v := range spec {
		obj[k] = v
	}
	u := unstructured.Unstructured{Object: obj}
	if len(annotations) > 0 {
		u.SetAnnotations(annotations)
	}
	if withDeletionTS {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
	}
	return u
}
