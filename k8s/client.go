package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corelisters "k8s.io/client-go/listers/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// nodeclaimGVRs lists the Karpenter NodeClaim GVRs to probe, newest first.
// The first one that successfully lists against the API server is used.
var nodeclaimGVRs = []schema.GroupVersionResource{
	{Group: "karpenter.sh", Version: "v1", Resource: "nodeclaims"},
	{Group: "karpenter.sh", Version: "v1beta1", Resource: "nodeclaims"},
}

// Options configures the k8s client connection.
type Options struct {
	Kubeconfig string // explicit path; "" = auto-detect via $KUBECONFIG or ~/.kube/config
	Context    string // explicit context name; "" = current-context in kubeconfig
}

// Client holds the kubernetes and (optional) metrics clientsets.
type Client struct {
	kube           *kubernetes.Clientset
	dynamic        dynamic.Interface
	nodeclaimGVR   *schema.GroupVersionResource // nil if Karpenter CRD not present / no RBAC
	metrics        *metricsclient.Clientset     // nil if metrics-server absent
	ClusterName    string
	ContextName    string

	// Informers and Listers
	informerFactory    informers.SharedInformerFactory
	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory
	nodeLister         corelisters.NodeLister
	podLister          corelisters.PodLister
	ncLister           interface {
		List(selector labels.Selector) ([]runtime.Object, error)
	}
	stopCh             chan struct{}
}

// New creates a Client from the given options (or defaults).
func New(opts Options) (*Client, error) {
	kubeconfig := opts.Kubeconfig
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("finding home dir: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		overrides,
	)

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building rest config: %w", err)
	}

	kube, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	// Use the context NAME as the display name (matches kubectl config get-contexts NAME column).
	currentCtx := rawConfig.CurrentContext
	if opts.Context != "" {
		currentCtx = opts.Context
	}
	clusterName := currentCtx

	// Metrics client is best-effort; not all clusters have metrics-server.
	var mc *metricsclient.Clientset
	if m, err := metricsclient.NewForConfig(restConfig); err == nil {
		mc = m
	}

	// Probe for Karpenter NodeClaim CRD.
	nodeclaimGVR := probeNodeClaimGVR(dynClient)

	// Informer Setup
	stopCh := make(chan struct{})
	factory := informers.NewSharedInformerFactory(kube, time.Minute*30)
	nodeLister := factory.Core().V1().Nodes().Lister()
	podLister := factory.Core().V1().Pods().Lister()

	var dynFactory dynamicinformer.DynamicSharedInformerFactory
	var ncLister interface {
		List(selector labels.Selector) ([]runtime.Object, error)
	}
	if nodeclaimGVR != nil {
		dynFactory = dynamicinformer.NewDynamicSharedInformerFactory(dynClient, time.Minute*30)
		ncLister = dynFactory.ForResource(*nodeclaimGVR).Lister()
	}

	c := &Client{
		kube:               kube,
		dynamic:            dynClient,
		nodeclaimGVR:       nodeclaimGVR,
		metrics:            mc,
		ClusterName:        clusterName,
		ContextName:        currentCtx,
		informerFactory:    factory,
		dynInformerFactory: dynFactory,
		nodeLister:         nodeLister,
		podLister:          podLister,
		ncLister:           ncLister,
		stopCh:             stopCh,
	}

	return c, nil
}

// Start launches the informers and waits for the initial cache sync.
func (c *Client) Start(ctx context.Context) error {
	// Start standard informers
	c.informerFactory.Start(c.stopCh)

	// Start dynamic informer for NodeClaims if GVR was found
	if c.dynInformerFactory != nil {
		c.dynInformerFactory.Start(c.stopCh)
	}

	// Wait for core caches to sync so the UI doesn't start empty.
	// We use a timeout to prevent hanging on unreachable clusters.
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		allSynced := true
		for _, synced := range c.informerFactory.WaitForCacheSync(c.stopCh) {
			if !synced {
				allSynced = false
				break
			}
		}
		if allSynced && c.dynInformerFactory != nil {
			for _, synced := range c.dynInformerFactory.WaitForCacheSync(c.stopCh) {
				if !synced {
					allSynced = false
					break
				}
			}
		}
		if allSynced {
			close(done)
		}
	}()

	select {
	case <-done:
		return nil
	case <-syncCtx.Done():
		return fmt.Errorf("timed out waiting for informer cache sync: %w", syncCtx.Err())
	}
}

// Stop shuts down the informer watchers.
func (c *Client) Stop() {
	close(c.stopCh)
}

// probeNodeClaimGVR tries each known Karpenter NodeClaim GVR and returns the
// first one that responds successfully. Returns nil if Karpenter is not
// installed or the service account lacks list permission.
func probeNodeClaimGVR(dynClient dynamic.Interface) *schema.GroupVersionResource {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, gvr := range nodeclaimGVRs {
		gvrCopy := gvr
		// A list with a limit of 1 is the cheapest probe — it hits the API
		// server with minimal data transfer and returns quickly.
		_, err := dynClient.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1})
		if err == nil {
			return &gvrCopy
		}
		// 404 = CRD not installed; 403 = no RBAC; both mean unusable.
		// Any other error (network timeout etc.) also means fall back.
	}
	return nil
}
