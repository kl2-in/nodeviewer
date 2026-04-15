package model

import "time"

type Status string

const (
	StatusReady                      Status = "Ready"
	StatusNotReady                   Status = "NotReady"
	StatusUnknown                    Status = "Unknown"
	StatusSchedulingDisabled         Status = "SchedulingDisabled"
	StatusReadySchedulingDisabled    Status = "Ready,SchedulingDisabled"
	StatusNotReadySchedulingDisabled Status = "NotReady,SchedulingDisabled"
	StatusUnknownSchedulingDisabled  Status = "Unknown,SchedulingDisabled"
)

type Node struct {
	Name                    string            `json:"name"`
	Status                  Status            `json:"status"`
	Roles                   string            `json:"roles"`
	Age                     string            `json:"age"`
	CreationTimestamp       time.Time         `json:"creationTimestamp"`
	Arch                    string            `json:"arch"`
	NodeGroup               string            `json:"nodeGroup"`
	Instance                string            `json:"instance"`
	Zone                    string            `json:"zone"`
	CPU                     string            `json:"cpu"`
	Mem                     string            `json:"mem"`
	CPUPercent              float64           `json:"cpuPercent"`
	MemPercent              float64           `json:"memPercent"`
	PodCount                int               `json:"podCount"`
	PodCapacity             int               `json:"podCapacity"`
	Cordoned                bool              `json:"cordoned"`
	CapacityType            string            `json:"capacityType"`
	KarpenterStatus         string            `json:"karpenterStatus,omitempty"`
	KarpenterReason         string            `json:"karpenterReason,omitempty"`
	OSImage                 string            `json:"osImage"`
	KernelVersion           string            `json:"kernelVersion"`
	ContainerRuntimeVersion string            `json:"containerRuntimeVersion"`
	KubeletVersion          string            `json:"kubeletVersion"`
	InternalIP              string            `json:"internalIP"`
	ExternalIP              string            `json:"externalIP,omitempty"`
	PodCIDR                 string            `json:"podCIDR"`
	Taints                  []string          `json:"taints"`
	Labels                  map[string]string `json:"labels"`
	AllocatableCPU          string            `json:"allocatableCPU"`
	AllocatableMem          string            `json:"allocatableMem"`
	Conditions              []Condition       `json:"conditions"`
	Pods                    []Pod             `json:"pods,omitempty"`
	Events                  []Event           `json:"events,omitempty"`
}

// Event is a k8s event scoped to this node.
type Event struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Count   int32  `json:"count"`
	Age     string `json:"age"`
	Source  string `json:"source"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// Pod is a lightweight pod summary stored per-node.
type Pod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	Age       string `json:"age"`
	Image     string `json:"image"`
}
