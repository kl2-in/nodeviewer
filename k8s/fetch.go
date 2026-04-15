package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"knv/model"
)

// FetchNodes implements model.Loader. It fetches nodes, pod counts, and
// (if metrics-server is available) CPU/memory usage.
func (c *Client) FetchNodes() ([]model.Node, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use Lister for nodes instead of API List
	nodeList, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, c.ClusterName, fmt.Errorf("listing nodes: %w", err)
	}

	// Fetch all pods once; group by node name
	podsByNode := make(map[string][]corev1.Pod)
	// Use Lister for pods instead of API List
	podList, err := c.podLister.List(labels.Everything())
	if err == nil {
		for _, pod := range podList {
			if pod.Spec.NodeName == "" {
				continue
			}
			podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], *pod)
		}
	}

	// Fetch node events (best-effort; scoped to nodes via involvedObject.kind=Node)
	// Keep this as polling since events are high-churn and specific to the current view
	eventsByNode := make(map[string][]corev1.Event)
	evList, err := c.kube.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=Node",
	})
	if err == nil {
		for _, ev := range evList.Items {
			name := ev.InvolvedObject.Name
			eventsByNode[name] = append(eventsByNode[name], ev)
		}
	}

	// Metrics (best-effort — metrics-server may not be installed)
	metricsMap := make(map[string]metricsv1beta1.NodeMetrics)
	if c.metrics != nil {
		ml, err := c.metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, nm := range ml.Items {
				metricsMap[nm.Name] = nm
			}
		}
	}

	// NodeClaim disruption status (using the informer cache)
	nodeclaimStatus := c.fetchNodeClaimStatus(ctx)

	nodes := make([]model.Node, 0, len(nodeList))
	for _, n := range nodeList {
		nm := metricsMap[n.Name]
		nodes = append(nodes, convertNode(*n, podsByNode[n.Name], eventsByNode[n.Name], nm, nodeclaimStatus))
	}

	return nodes, c.ClusterName, nil
}

// nodeclaimInfo holds the disruption status and optional reason for a NodeClaim.
type nodeclaimInfo struct {
	status string
	reason string
}

// fetchNodeClaimStatus lists all NodeClaims and returns a map of
// nodeName → nodeclaimInfo. Returns an empty map on any error
// (missing CRD, no RBAC, network error) so callers never need to handle nil.
func (c *Client) fetchNodeClaimStatus(ctx context.Context) map[string]nodeclaimInfo {
	result := make(map[string]nodeclaimInfo)
	if c.ncLister == nil {
		return result
	}

	items, err := c.ncLister.List(labels.Everything())
	if err != nil {
		return result
	}

	for _, item := range items {
		// GenericLister returns runtime.Object, need to cast to unstructured.Unstructured
		nc, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		nodeName := nodeNameFromNodeClaim(*nc)
		if nodeName == "" {
			continue
		}
		result[nodeName] = karpenterStatusFromNodeClaim(*nc)
	}
	return result
}

// nodeNameFromNodeClaim extracts the bound node name from a NodeClaim's
// status.nodeName field. Returns "" if the NodeClaim is not yet bound.
func nodeNameFromNodeClaim(nc unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(nc.Object, "status", "nodeName")
	return name
}

// karpenterStatusFromNodeClaim derives the disruption status and reason for a
// NodeClaim. Returns ("Eligible", "") when no active disruption is found.
func karpenterStatusFromNodeClaim(nc unstructured.Unstructured) nodeclaimInfo {
	// 1. Explicit block annotation on the NodeClaim itself.
	annotations := nc.GetAnnotations()
	if annotations["karpenter.sh/do-not-disrupt"] == "true" {
		return nodeclaimInfo{status: "Blocked"}
	}

	// 2. DeletionTimestamp set → actively being terminated.
	if nc.GetDeletionTimestamp() != nil {
		return nodeclaimInfo{status: "Disrupting"}
	}

	conditions, _, _ := unstructured.NestedSlice(nc.Object, "status", "conditions")

	// 3. Pick the recognized condition with status=True and the most recent
	// lastTransitionTime.
	var chosen nodeclaimInfo
	var latestTransition time.Time
	chosenHasTimestamp := false

	for _, raw := range conditions {
		c, ok := raw.(map[string]interface{})
		if !ok || c["status"] != "True" {
			continue
		}

		typeName, _ := c["type"].(string)
		reason, _ := c["reason"].(string)

		var candidate nodeclaimInfo
		switch typeName {
		case "Drifted":
			candidate = nodeclaimInfo{status: "Drifted", reason: reason}
		case "Disrupted":
			if reason != "" {
				candidate = nodeclaimInfo{status: reason}
			} else {
				candidate = nodeclaimInfo{status: "Disrupting"}
			}
		case "Consolidatable", "Launched", "Registered", "Initialized", "ConsistentStateFound", "Ready":
			candidate = nodeclaimInfo{status: typeName, reason: reason}
		default:
			continue
		}

		lts, _ := c["lastTransitionTime"].(string)
		ts, err := time.Parse(time.RFC3339, lts)
		if err == nil {
			if !chosenHasTimestamp || ts.After(latestTransition) {
				latestTransition = ts
				chosen = candidate
				chosenHasTimestamp = true
			}
			continue
		}

		if !chosenHasTimestamp && chosen.status == "" {
			chosen = candidate
		}
	}

	if chosen.status != "" {
		return chosen
	}

	// No recognized active condition found.
	return nodeclaimInfo{status: "Eligible"}
}

// ── conversion ────────────────────────────────────────────────────────────────

func convertNode(n corev1.Node, pods []corev1.Pod, events []corev1.Event, nm metricsv1beta1.NodeMetrics, nodeclaimStatus map[string]nodeclaimInfo) model.Node {
	podCount := len(pods)
	status := resolveStatus(n)
	roles := resolveRoles(n.Labels)
	age := formatAge(n.CreationTimestamp.Time)

	arch := firstLabel(n.Labels, "kubernetes.io/arch", "beta.kubernetes.io/arch")
	zone := firstLabel(n.Labels, "topology.kubernetes.io/zone", "failure-domain.beta.kubernetes.io/zone")
	instance := firstLabel(n.Labels, "node.kubernetes.io/instance-type", "beta.kubernetes.io/instance-type")
	nodeGroup := resolveNodeGroup(n.Labels)
	capacityType := resolveCapacityType(n.Labels)
	ncInfo := nodeclaimStatus[n.Name]

	cpuCap := n.Status.Capacity[corev1.ResourceCPU]
	memCap := n.Status.Capacity[corev1.ResourceMemory]
	podCap := n.Status.Capacity[corev1.ResourcePods]

	var cpuPct, memPct float64
	cpuDisplay := cpuCap.String()
	memDisplay := formatBytes(memCap.Value())

	if nm.Name != "" {
		cpuUsage := nm.Usage[corev1.ResourceCPU]
		memUsage := nm.Usage[corev1.ResourceMemory]

		if cpuCap.MilliValue() > 0 {
			cpuPct = float64(cpuUsage.MilliValue()) / float64(cpuCap.MilliValue()) * 100
		}
		if memCap.Value() > 0 {
			memPct = float64(memUsage.Value()) / float64(memCap.Value()) * 100
		}
		cpuDisplay = fmt.Sprintf("%dm/%s", cpuUsage.MilliValue(), cpuCap.String())
		memDisplay = fmt.Sprintf("%s/%s", formatBytes(memUsage.Value()), formatBytes(memCap.Value()))
	}

	var internalIP, externalIP string
	for _, addr := range n.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			internalIP = addr.Address
		case corev1.NodeExternalIP:
			externalIP = addr.Address
		}
	}

	taints := make([]string, len(n.Spec.Taints))
	for i, t := range n.Spec.Taints {
		if t.Value != "" {
			taints[i] = fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect)
		} else {
			taints[i] = fmt.Sprintf("%s:%s", t.Key, t.Effect)
		}
	}

	// Sort labels for deterministic display
	labels := n.Labels

	conditions := make([]model.Condition, len(n.Status.Conditions))
	for i, c := range n.Status.Conditions {
		conditions[i] = model.Condition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		}
	}
	// Put Ready last so it's most prominent in the inspect view
	sort.Slice(conditions, func(i, j int) bool {
		if conditions[i].Type == "Ready" {
			return false
		}
		if conditions[j].Type == "Ready" {
			return true
		}
		return conditions[i].Type < conditions[j].Type
	})

	allocCPU := n.Status.Allocatable[corev1.ResourceCPU]
	allocMem := n.Status.Allocatable[corev1.ResourceMemory]

	return model.Node{
		Name:              n.Name,
		Status:            status,
		Roles:             roles,
		Age:               age,
		CreationTimestamp: n.CreationTimestamp.Time,
		Arch:              arch,
		NodeGroup:         nodeGroup,
		CapacityType:      capacityType,
		KarpenterStatus:   ncInfo.status,
		KarpenterReason:   ncInfo.reason,
		Instance:          instance,
		Zone:              zone,
		OSImage:           shortenOSImage(n.Status.NodeInfo.OSImage),
		CPU:               cpuDisplay,
		Mem:               memDisplay,
		CPUPercent:        cpuPct,
		MemPercent:        memPct,
		PodCount:          podCount,
		PodCapacity:       int(podCap.Value()),
		Cordoned:          n.Spec.Unschedulable,
		// Extended details for inspect panel
		KernelVersion:           n.Status.NodeInfo.KernelVersion,
		ContainerRuntimeVersion: n.Status.NodeInfo.ContainerRuntimeVersion,
		KubeletVersion:          n.Status.NodeInfo.KubeletVersion,
		InternalIP:              internalIP,
		ExternalIP:              externalIP,
		PodCIDR:                 n.Spec.PodCIDR,
		Taints:                  taints,
		Labels:                  labels,
		AllocatableCPU:          allocCPU.String(),
		AllocatableMem:          formatBytes(allocMem.Value()),
		Conditions:              conditions,
		Pods:                    convertPods(pods),
		Events:                  convertEvents(events),
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func resolveStatus(n corev1.Node) model.Status {
	ready := model.StatusUnknown
	for _, cond := range n.Status.Conditions {
		if cond.Type != corev1.NodeReady {
			continue
		}
		switch cond.Status {
		case corev1.ConditionTrue:
			ready = model.StatusReady
		case corev1.ConditionFalse:
			ready = model.StatusNotReady
		default:
			ready = model.StatusUnknown
		}
		break
	}

	if !n.Spec.Unschedulable {
		return ready
	}
	// Combine ready state with SchedulingDisabled — mirrors kubectl behaviour
	switch ready {
	case model.StatusReady:
		return model.StatusReadySchedulingDisabled
	case model.StatusNotReady:
		return model.StatusNotReadySchedulingDisabled
	default:
		return model.StatusUnknownSchedulingDisabled
	}
}

func resolveRoles(labels map[string]string) string {
	seen := map[string]bool{}
	var roles []string

	for k, v := range labels {
		var role string
		switch {
		case strings.HasPrefix(k, "node-role.kubernetes.io/"):
			// e.g. node-role.kubernetes.io/control-plane: ""
			role = strings.TrimPrefix(k, "node-role.kubernetes.io/")
		case k == "kubernetes.io/role":
			// legacy label: kubernetes.io/role: worker
			role = v
		}
		if role != "" && !seen[role] {
			seen[role] = true
			roles = append(roles, role)
		}
	}

	sort.Strings(roles)

	// Drop "master" when "control-plane" is also present (duplicate in older clusters)
	hasCP := false
	for _, r := range roles {
		if r == "control-plane" {
			hasCP = true
			break
		}
	}
	if hasCP {
		out := roles[:0]
		for _, r := range roles {
			if r != "master" {
				out = append(out, r)
			}
		}
		roles = out
	}

	if len(roles) == 0 {
		return "worker"
	}
	return strings.Join(roles, ",")
}

func resolveNodeGroup(labels map[string]string) string {
	// Collect all candidate values from well-known keys and fuzzy matches,
	// then return the shortest non-empty one. EKS often sets both
	// eks.amazonaws.com/nodegroup (full auto-generated name) and a custom
	// shorter label, so shortest is almost always the human-readable one.
	seen := map[string]bool{}
	var candidates []string

	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			candidates = append(candidates, v)
		}
	}

	wellKnown := []string{
		"eks.amazonaws.com/nodegroup",
		"alpha.eksctl.io/nodegroup-name",
		"cloud.google.com/gke-nodepool",
		"kops.k8s.io/instancegroup",
		"node.kubernetes.io/node-group",
		"agentpool",
		"kubernetes.azure.com/agentpool",
	}
	for _, k := range wellKnown {
		if v, ok := labels[k]; ok {
			add(v)
		}
	}

	// Fuzzy: any label key containing nodegroup/node-group/node_group
	for k, v := range labels {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "nodegroup") ||
			strings.Contains(kl, "node-group") ||
			strings.Contains(kl, "node_group") {
			add(v)
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Pick the shortest value — custom names are almost always shorter
	// than auto-generated ones (e.g. "workers-prod" vs "workers-prod-20240101120000-abc123")
	shortest := candidates[0]
	for _, c := range candidates[1:] {
		if len(c) < len(shortest) {
			shortest = c
		}
	}
	return shortest
}

func resolveCapacityType(labels map[string]string) string {
	// Check well-known exact keys first (most reliable)
	exactKeys := []string{
		"eks.amazonaws.com/capacityType",        // EKS managed nodegroup
		"karpenter.sh/capacity-type",            // Karpenter
		"kubernetes.azure.com/scalesetpriority", // AKS
		"node.kubernetes.io/capacity-type",      // generic
	}
	for _, k := range exactKeys {
		if v, ok := labels[k]; ok && v != "" {
			return normalizeCapacity(v)
		}
	}
	// GKE spot: presence of the label means spot, no value needed
	if _, ok := labels["cloud.google.com/gke-spot"]; ok {
		return "spot"
	}
	// Broad scan: any label whose key suffix (after last "/") contains
	// "capacitytype" or "capacity-type" or "capacity_type"
	for k, v := range labels {
		suffix := k
		if i := strings.LastIndex(k, "/"); i >= 0 {
			suffix = k[i+1:]
		}
		sl := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(suffix, "-", ""), "_", ""))
		if sl == "capacitytype" && v != "" {
			return normalizeCapacity(v)
		}
	}
	return ""
}

func normalizeCapacity(v string) string {
	v = strings.ToLower(v)
	// EKS returns "ON_DEMAND", Karpenter returns "on-demand"
	if strings.Contains(v, "demand") {
		return "on-demand"
	}
	if strings.Contains(v, "spot") {
		return "spot"
	}
	return v
}

func convertPods(pods []corev1.Pod) []model.Pod {
	out := make([]model.Pod, 0, len(pods))
	for _, p := range pods {
		// Count restarts across all containers
		var restarts int32
		allReady := true
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			if !cs.Ready {
				allReady = false
			}
		}
		if len(p.Status.ContainerStatuses) == 0 {
			allReady = false
		}

		img := ""
		if len(p.Spec.Containers) > 0 {
			img = shortenImage(p.Spec.Containers[0].Image)
		}

		out = append(out, model.Pod{
			Name:      p.Name,
			Namespace: p.Namespace,
			Phase:     string(p.Status.Phase),
			Ready:     allReady,
			Restarts:  restarts,
			Age:       formatAge(p.CreationTimestamp.Time),
			Image:     img,
		})
	}
	// Sort: non-running first (problems at the top), then alphabetical
	sort.Slice(out, func(i, j int) bool {
		ri, rj := phaseRank(out[i].Phase), phaseRank(out[j].Phase)
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func phaseRank(phase string) int {
	switch phase {
	case "Failed":
		return 0
	case "Unknown":
		return 1
	case "Pending":
		return 2
	case "Running":
		return 3
	default:
		return 4
	}
}

func convertEvents(events []corev1.Event) []model.Event {
	out := make([]model.Event, 0, len(events))
	for _, e := range events {
		t := e.LastTimestamp.Time
		if t.IsZero() {
			t = e.EventTime.Time
		}
		source := e.Source.Component
		if source == "" {
			source = e.Source.Host
		}
		out = append(out, model.Event{
			Type:    e.Type,
			Reason:  e.Reason,
			Message: e.Message,
			Count:   e.Count,
			Age:     formatAge(t),
			Source:  source,
		})
	}
	// Sort: Warning first, then most recent (smallest age string isn't reliable,
	// so we sort by LastTimestamp descending)
	sort.Slice(out, func(i, j int) bool {
		ei, ej := events[i], events[j]
		// Warnings bubble to top
		if ei.Type != ej.Type {
			return ei.Type == "Warning"
		}
		ti := ei.LastTimestamp.Time
		if ti.IsZero() {
			ti = ei.EventTime.Time
		}
		tj := ej.LastTimestamp.Time
		if tj.IsZero() {
			tj = ej.EventTime.Time
		}
		return ti.After(tj)
	})
	return out
}

func shortenImage(img string) string {
	// Drop registry prefix (e.g. "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:v1" → "myapp:v1")
	if i := strings.LastIndex(img, "/"); i >= 0 {
		img = img[i+1:]
	}
	// Truncate long tags
	if len(img) > 30 {
		img = img[:29] + "…"
	}
	return img
}

func firstLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := labels[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d.Hours() >= 24*365:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	case d.Hours() >= 24:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d.Hours() >= 1:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

func formatBytes(b int64) string {
	const (
		GiB = 1 << 30
		MiB = 1 << 20
		KiB = 1 << 10
	)
	switch {
	case b >= GiB:
		return fmt.Sprintf("%.1fGi", float64(b)/GiB)
	case b >= MiB:
		return fmt.Sprintf("%.0fMi", float64(b)/MiB)
	case b >= KiB:
		return fmt.Sprintf("%.0fKi", float64(b)/KiB)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func shortenOSImage(s string) string {
	for _, r := range []struct{ from, to string }{
		{"Container-Optimized OS", "COS"},
		{"Amazon Linux 2023", "AL2023"},
		{"Amazon Linux 2", "AL2"},
		{"Ubuntu 22.04", "Ubuntu 22"},
		{"Ubuntu 20.04", "Ubuntu 20"},
		{"Debian GNU/Linux", "Debian"},
		{"Red Hat Enterprise Linux", "RHEL"},
		{"Windows Server", "Win Server"},
	} {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}
