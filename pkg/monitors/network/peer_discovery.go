// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Peer represents a discovered node-doctor peer.
type Peer struct {
	// Name is the pod name.
	Name string
	// NodeName is the Kubernetes node the peer is running on.
	NodeName string
	// NodeIP is the IP address of the node (used for pinging since hostNetwork=true).
	NodeIP string
	// PodIP is the pod IP (same as NodeIP when using hostNetwork).
	PodIP string
	// LastSeen is when this peer was last seen in discovery.
	LastSeen time.Time
}

// PeerDiscovery handles discovery of node-doctor peer instances.
type PeerDiscovery interface {
	// GetPeers returns the current list of discovered peers.
	GetPeers() []Peer
	// Refresh forces an immediate refresh of the peer list.
	Refresh(ctx context.Context) error
	// Start begins background peer discovery.
	Start(ctx context.Context) error
	// Stop stops background peer discovery.
	Stop()
}

// PeerDiscoveryConfig holds configuration for peer discovery.
type PeerDiscoveryConfig struct {
	// Namespace to search for peers.
	Namespace string
	// LabelSelector for filtering pods (e.g., "app=node-doctor").
	LabelSelector string
	// RefreshInterval is how often to refresh the peer list.
	RefreshInterval time.Duration
	// SelfNodeName is the name of the current node (to exclude self).
	SelfNodeName string
	// Kubeconfig path (optional, uses in-cluster config if empty).
	Kubeconfig string
}

// kubernetesPeerDiscovery implements PeerDiscovery using Kubernetes API.
type kubernetesPeerDiscovery struct {
	config    *PeerDiscoveryConfig
	client    kubernetes.Interface
	mu        sync.RWMutex
	peers     []Peer
	stopChan  chan struct{}
	running   bool
	lastError error
}

// NewKubernetesPeerDiscovery creates a new peer discovery instance using Kubernetes API.
func NewKubernetesPeerDiscovery(config *PeerDiscoveryConfig) (PeerDiscovery, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Apply defaults
	if config.Namespace == "" {
		config.Namespace = getNamespaceFromEnvOrDefault()
	}
	if config.LabelSelector == "" {
		config.LabelSelector = "app=node-doctor"
	}
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 5 * time.Minute
	}
	if config.SelfNodeName == "" {
		config.SelfNodeName = os.Getenv("NODE_NAME")
	}

	// Create Kubernetes client
	client, err := createKubernetesClient(config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &kubernetesPeerDiscovery{
		config:   config,
		client:   client,
		peers:    make([]Peer, 0),
		stopChan: make(chan struct{}),
	}, nil
}

// NewKubernetesPeerDiscoveryWithClient creates a peer discovery instance with an existing client.
// This is useful for testing with mock clients.
func NewKubernetesPeerDiscoveryWithClient(config *PeerDiscoveryConfig, client kubernetes.Interface) (PeerDiscovery, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}

	// Apply defaults
	if config.Namespace == "" {
		config.Namespace = getNamespaceFromEnvOrDefault()
	}
	if config.LabelSelector == "" {
		config.LabelSelector = "app=node-doctor"
	}
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 5 * time.Minute
	}
	if config.SelfNodeName == "" {
		config.SelfNodeName = os.Getenv("NODE_NAME")
	}

	return &kubernetesPeerDiscovery{
		config:   config,
		client:   client,
		peers:    make([]Peer, 0),
		stopChan: make(chan struct{}),
	}, nil
}

// GetPeers returns the current list of discovered peers.
func (d *kubernetesPeerDiscovery) GetPeers() []Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Return a copy to prevent external modification
	peers := make([]Peer, len(d.peers))
	copy(peers, d.peers)
	return peers
}

// Refresh forces an immediate refresh of the peer list.
func (d *kubernetesPeerDiscovery) Refresh(ctx context.Context) error {
	peers, err := d.discoverPeers(ctx)
	if err != nil {
		d.mu.Lock()
		d.lastError = err
		d.mu.Unlock()
		return err
	}

	d.mu.Lock()
	d.peers = peers
	d.lastError = nil
	d.mu.Unlock()

	return nil
}

// Start begins background peer discovery.
func (d *kubernetesPeerDiscovery) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("peer discovery is already running")
	}
	d.running = true
	d.stopChan = make(chan struct{})
	d.mu.Unlock()

	// Perform initial discovery
	if err := d.Refresh(ctx); err != nil {
		// Log error but don't fail - we'll retry on the next tick
		d.mu.Lock()
		d.lastError = err
		d.mu.Unlock()
	}

	// Start background refresh goroutine
	go d.backgroundRefresh(ctx)

	return nil
}

// Stop stops background peer discovery.
func (d *kubernetesPeerDiscovery) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	close(d.stopChan)
	d.running = false
}

// backgroundRefresh runs periodic peer discovery.
func (d *kubernetesPeerDiscovery) backgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Create a timeout context for the refresh operation
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_ = d.Refresh(refreshCtx)
			cancel()
		}
	}
}

// discoverPeers queries the Kubernetes API for node-doctor pods.
func (d *kubernetesPeerDiscovery) discoverPeers(ctx context.Context) ([]Peer, error) {
	listOpts := metav1.ListOptions{
		LabelSelector: d.config.LabelSelector,
	}

	pods, err := d.client.CoreV1().Pods(d.config.Namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	peers := make([]Peer, 0, len(pods.Items))
	now := time.Now()

	for _, pod := range pods.Items {
		// Skip pods that aren't running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Skip self (pods on the same node)
		if pod.Spec.NodeName == d.config.SelfNodeName {
			continue
		}

		// Get node IP for hostNetwork pods
		nodeIP := getNodeIP(&pod)
		if nodeIP == "" {
			continue // Skip pods without a valid node IP
		}

		peer := Peer{
			Name:     pod.Name,
			NodeName: pod.Spec.NodeName,
			NodeIP:   nodeIP,
			PodIP:    pod.Status.PodIP,
			LastSeen: now,
		}

		peers = append(peers, peer)
	}

	return peers, nil
}

// getNodeIP extracts the node IP from a pod.
// For hostNetwork pods, this is the host IP.
func getNodeIP(pod *corev1.Pod) string {
	// For hostNetwork pods, HostIP is the node IP
	if pod.Status.HostIP != "" {
		return pod.Status.HostIP
	}
	// Fallback to PodIP (which should be the same for hostNetwork pods)
	return pod.Status.PodIP
}

// createKubernetesClient creates a Kubernetes client.
func createKubernetesClient(kubeconfig string) (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// Use provided kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
	} else {
		// Try in-cluster config first
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fallback to default kubeconfig
			kubeconfig = clientcmd.RecommendedHomeFile
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
			}
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return client, nil
}

// getNamespaceFromEnvOrDefault returns the namespace from environment or default.
func getNamespaceFromEnvOrDefault() string {
	// Try POD_NAMESPACE first (set by Kubernetes downward API)
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	// Try to read from service account namespace file
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return string(data)
	}
	// Default namespace
	return "node-doctor"
}

// staticPeerDiscovery implements PeerDiscovery with a static list of peers.
// This is useful for testing or when Kubernetes API is not available.
type staticPeerDiscovery struct {
	mu    sync.RWMutex
	peers []Peer
}

// NewStaticPeerDiscovery creates a peer discovery instance with a static list.
func NewStaticPeerDiscovery(peers []Peer) PeerDiscovery {
	return &staticPeerDiscovery{
		peers: peers,
	}
}

// GetPeers returns the static list of peers.
func (d *staticPeerDiscovery) GetPeers() []Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]Peer, len(d.peers))
	copy(peers, d.peers)
	return peers
}

// Refresh is a no-op for static discovery.
func (d *staticPeerDiscovery) Refresh(ctx context.Context) error {
	return nil
}

// Start is a no-op for static discovery.
func (d *staticPeerDiscovery) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for static discovery.
func (d *staticPeerDiscovery) Stop() {}

// SetPeers updates the static peer list (for testing).
func (d *staticPeerDiscovery) SetPeers(peers []Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peers = peers
}
