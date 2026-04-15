package model

import "context"

// Loader is the interface any data source must implement.
// The k8s package satisfies this without creating a circular import.
type Loader interface {
	// FetchNodes returns the node list and the cluster/context name.
	FetchNodes() (nodes []Node, clusterName string, err error)
}

// Mutator is implemented by data sources that can mutate node state.
// The cast `loader.(Mutator)` is used to check capability at runtime.
type Mutator interface {
	// CordonNode marks a node unschedulable.
	CordonNode(ctx context.Context, name string) error
	// UncordonNode marks a node schedulable.
	UncordonNode(ctx context.Context, name string) error
	// DrainNode cordons then evicts all evictable pods from the node.
	// PodDisruptionBudgets are respected; bare pods without a controller
	// are NOT force-deleted.
	DrainNode(ctx context.Context, name string) error
}
