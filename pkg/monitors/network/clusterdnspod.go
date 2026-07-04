// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/clusterdns"
	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// clusterDNSPodMonitorType is the registered monitor type string.
	clusterDNSPodMonitorType = "network-cluster-dns-pod"

	defaultClusterDNSPodLabelSelector         = "app=node-doctor-overlay-test"
	defaultClusterDNSPodProbePort             = 8023
	defaultClusterDNSPodProbePath             = "/clusterdns"
	defaultClusterDNSPodTimeout               = 5 * time.Second
	defaultClusterDNSPodFailureCountThreshold = 3
	defaultClusterDNSPodMinSuccessPods        = 1

	// maxClusterDNSProbePeers caps how many overlay-test pods are probed per cycle,
	// bounding load on overlay-test pods when there are many nodes.
	maxClusterDNSProbePeers = 5
)

// ClusterDNSPodConfig holds the configuration for the cluster-DNS-via-pods monitor.
type ClusterDNSPodConfig struct {
	Enabled bool

	// LabelSelector selects the overlay-test pods to probe (default "app=node-doctor-overlay-test").
	LabelSelector string

	// Namespace to search for overlay-test pods (default from getNamespaceFromEnvOrDefault()).
	Namespace string

	// ProbePort is the overlay-test-server's HTTP port (default 8023).
	ProbePort int

	// ProbePath is the overlay-test-server's cluster-DNS endpoint (default "/clusterdns").
	ProbePath string

	// Timeout bounds each individual per-pod HTTP probe (default 5s). This is
	// intentionally separate from the BaseMonitor-level check timeout: it bounds a
	// single pod's probe so that one slow/hanging pod doesn't consume the entire
	// check-cycle timeout budget.
	Timeout time.Duration

	// FailureCountThreshold is the number of consecutive failed cycles before
	// reporting ClusterDNSDown (default 3).
	FailureCountThreshold int

	// MinSuccessPods is the number of overlay-test pods that must report
	// resolved=true in a cycle for cluster DNS to be considered healthy (default 1).
	MinSuccessPods int
}

// applyDefaults fills in unset ClusterDNSPodConfig fields with their default values.
func (c *ClusterDNSPodConfig) applyDefaults() {
	if c.LabelSelector == "" {
		c.LabelSelector = defaultClusterDNSPodLabelSelector
	}
	if c.Namespace == "" {
		c.Namespace = getNamespaceFromEnvOrDefault()
	}
	if c.ProbePort == 0 {
		c.ProbePort = defaultClusterDNSPodProbePort
	}
	if c.ProbePath == "" {
		c.ProbePath = defaultClusterDNSPodProbePath
	}
	if c.Timeout == 0 {
		c.Timeout = defaultClusterDNSPodTimeout
	}
	if c.FailureCountThreshold == 0 {
		c.FailureCountThreshold = defaultClusterDNSPodFailureCountThreshold
	}
	if c.MinSuccessPods == 0 {
		c.MinSuccessPods = defaultClusterDNSPodMinSuccessPods
	}
}

// parseClusterDNSPodConfig parses the cluster-DNS-pod monitor configuration from a map.
func parseClusterDNSPodConfig(configMap map[string]interface{}) (*ClusterDNSPodConfig, error) {
	config := &ClusterDNSPodConfig{}

	if configMap == nil {
		return config, nil
	}

	if v, ok := configMap["enabled"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("enabled must be a boolean")
		}
		config.Enabled = b
	}

	if v, ok := configMap["labelSelector"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("labelSelector must be a string")
		}
		config.LabelSelector = s
	}

	if v, ok := configMap["namespace"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("namespace must be a string")
		}
		config.Namespace = s
	}

	if v, ok := configMap["probePort"]; ok {
		switch n := v.(type) {
		case float64:
			config.ProbePort = int(n)
		case int:
			config.ProbePort = n
		default:
			return nil, fmt.Errorf("probePort must be an integer")
		}
	}

	if v, ok := configMap["probePath"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("probePath must be a string")
		}
		config.ProbePath = s
	}

	if v, ok := configMap["timeout"]; ok {
		d, err := parseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		config.Timeout = d
	}

	if v, ok := configMap["failureCountThreshold"]; ok {
		switch n := v.(type) {
		case float64:
			config.FailureCountThreshold = int(n)
		case int:
			config.FailureCountThreshold = n
		default:
			return nil, fmt.Errorf("failureCountThreshold must be an integer")
		}
	}

	if v, ok := configMap["minSuccessPods"]; ok {
		switch n := v.(type) {
		case float64:
			config.MinSuccessPods = int(n)
		case int:
			config.MinSuccessPods = n
		default:
			return nil, fmt.Errorf("minSuccessPods must be an integer")
		}
	}

	return config, nil
}

// ValidateClusterDNSPodConfig validates the cluster-DNS-pod monitor configuration.
func ValidateClusterDNSPodConfig(config types.MonitorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("monitor name is required")
	}

	if config.Type != clusterDNSPodMonitorType {
		return fmt.Errorf("invalid monitor type: expected %s, got %s", clusterDNSPodMonitorType, config.Type)
	}

	cfg, err := parseClusterDNSPodConfig(config.Config)
	if err != nil {
		return fmt.Errorf("failed to parse cluster-dns-pod config: %w", err)
	}
	cfg.applyDefaults()

	if cfg.FailureCountThreshold < 1 {
		return fmt.Errorf("failureCountThreshold must be at least 1, got %d", cfg.FailureCountThreshold)
	}

	if cfg.MinSuccessPods < 1 {
		return fmt.Errorf("minSuccessPods must be at least 1, got %d", cfg.MinSuccessPods)
	}

	if cfg.ProbePort < 1 || cfg.ProbePort > 65535 {
		return fmt.Errorf("probePort must be between 1 and 65535, got %d", cfg.ProbePort)
	}

	return nil
}

// clusterDNSPodHTTPClient is the minimal HTTP client interface used by
// ClusterDNSPodMonitor, allowing tests to inject a fake client.
type clusterDNSPodHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ClusterDNSPodMonitor verifies cluster DNS resolution via overlay-test pods running in
// pod-network context, working around the host-network Cilium ClusterIP routing gap.
//
// PeerDiscovery intentionally does NOT skip same-node pods for this monitor's purposes
// — overlay-test pods run on all nodes; other nodes' overlay-test pods are sufficient
// even though the underlying PeerDiscovery implementation excludes same-node pods via
// SelfNodeName. This is fine because we only need >= MinSuccessPods responses from ANY
// subset of overlay-test pods, not the node-doctor's own node's pod specifically.
type ClusterDNSPodMonitor struct {
	name       string
	config     *ClusterDNSPodConfig
	discovery  PeerDiscovery
	httpClient clusterDNSPodHTTPClient

	mu                  sync.Mutex
	consecutiveFailures int

	*monitors.BaseMonitor
}

// init registers the cluster-DNS-pod monitor with the monitor registry. DefaultConfig
// is intentionally left unset so ApplyDefaultMonitors never auto-enables this monitor,
// keeping it inert/dormant/code-only until explicitly configured.
func init() {
	monitors.MustRegister(monitors.MonitorInfo{
		Type:        clusterDNSPodMonitorType,
		Factory:     NewClusterDNSPodMonitor,
		Validator:   ValidateClusterDNSPodConfig,
		Description: "Verifies cluster DNS resolution via overlay-test pods running in pod-network context (works around host-network Cilium ClusterIP routing gaps)",
	})
}

// NewClusterDNSPodMonitorWithDiscovery creates a cluster-DNS-pod monitor using the
// supplied PeerDiscovery and HTTP client. This is a test-injection constructor mirroring
// NewKubernetesPeerDiscoveryWithClient's pattern.
func NewClusterDNSPodMonitorWithDiscovery(ctx context.Context, config types.MonitorConfig, discovery PeerDiscovery, httpClient clusterDNSPodHTTPClient) (types.Monitor, error) {
	cfg, err := parseClusterDNSPodConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cluster-dns-pod config: %w", err)
	}
	cfg.applyDefaults()

	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	monitor := &ClusterDNSPodMonitor{
		name:        config.Name,
		config:      cfg,
		discovery:   discovery,
		httpClient:  httpClient,
		BaseMonitor: baseMonitor,
	}

	if err := baseMonitor.SetCheckFunc(monitor.checkClusterDNSPod); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// NewClusterDNSPodMonitor is the standard registered factory for the cluster-DNS-pod
// monitor. It builds a real Kubernetes-backed PeerDiscovery and a real *http.Client.
func NewClusterDNSPodMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	cfg, err := parseClusterDNSPodConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cluster-dns-pod config: %w", err)
	}
	cfg.applyDefaults()

	discovery, err := NewKubernetesPeerDiscovery(&PeerDiscoveryConfig{
		Namespace:     cfg.Namespace,
		LabelSelector: cfg.LabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes peer discovery: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true, // Each probe should be independent.
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).DialContext,
		},
	}

	return NewClusterDNSPodMonitorWithDiscovery(ctx, config, discovery, httpClient)
}

// Start starts the cluster-DNS-pod monitor, beginning peer discovery before the base
// monitor's check loop.
func (m *ClusterDNSPodMonitor) Start() (<-chan *types.Status, error) {
	if err := m.discovery.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start peer discovery: %w", err)
	}

	return m.BaseMonitor.Start()
}

// Stop stops the cluster-DNS-pod monitor and its peer discovery.
func (m *ClusterDNSPodMonitor) Stop() {
	m.discovery.Stop()
	m.BaseMonitor.Stop()
}

// clusterDNSPodProbeOutcome is the per-peer result of a single cluster-DNS pod probe.
type clusterDNSPodProbeOutcome struct {
	resolved bool
}

// checkClusterDNSPod polls discovered overlay-test pods' /clusterdns endpoint and
// derives a ClusterDNSDown condition from the pod-sourced cluster-DNS resolution truth.
func (m *ClusterDNSPodMonitor) checkClusterDNSPod(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	peers := m.discovery.GetPeers()
	if len(peers) == 0 {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"NoOverlayTestPodsFound",
			"No overlay-test pods found for cluster DNS probing",
		))
		// Intentionally leave the consecutive-failure counter untouched: a transient
		// peer-discovery gap (e.g. right after monitor startup before the first
		// Refresh completes) is a discovery-layer problem, not evidence of an actual
		// cluster-DNS failure, so it must not be conflated into a false ClusterDNSDown.
		return status, nil
	}

	// Cap to at most maxClusterDNSProbePeers peers, sorted by Name for a deterministic
	// selection. This bounds load on overlay-test pods when there are many nodes.
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	selected := peers
	if len(selected) > maxClusterDNSProbePeers {
		selected = selected[:maxClusterDNSProbePeers]
	}

	outcomes := make(chan clusterDNSPodProbeOutcome, len(selected))
	var wg sync.WaitGroup

	for _, peer := range selected {
		wg.Add(1)
		go func(p Peer) {
			defer wg.Done()
			outcomes <- clusterDNSPodProbeOutcome{resolved: m.probePeer(ctx, p)}
		}(peer)
	}

	go func() {
		wg.Wait()
		close(outcomes)
	}()

	resolvedCount := 0
	totalProbed := len(selected)
	for outcome := range outcomes {
		if outcome.resolved {
			resolvedCount++
		}
	}

	m.mu.Lock()
	if resolvedCount >= m.config.MinSuccessPods {
		m.consecutiveFailures = 0
	} else {
		m.consecutiveFailures++
	}
	consecutiveFailures := m.consecutiveFailures
	m.mu.Unlock()

	// Toggle ClusterDNSDown True/False based on the consecutive-failure count, using
	// the same sticky-latched semantics as DNSMonitor.updateFailureTracking: True once
	// the threshold is reached, False only once the counter is back to exactly 0, and
	// left unset in between so a previously-latched condition stays as-is.
	switch {
	case consecutiveFailures >= m.config.FailureCountThreshold:
		status.AddCondition(types.NewCondition(
			"ClusterDNSDown",
			types.ConditionTrue,
			"RepeatedClusterDNSPodProbeFailures",
			fmt.Sprintf("Cluster DNS via overlay-test pods has failed %d consecutive times (threshold: %d); last cycle resolved %d/%d pods",
				consecutiveFailures, m.config.FailureCountThreshold, resolvedCount, totalProbed),
		))
	case consecutiveFailures == 0:
		status.AddCondition(types.NewCondition(
			"ClusterDNSDown",
			types.ConditionFalse,
			"ClusterDNSResolvedViaPods",
			fmt.Sprintf("Cluster DNS resolved via %d/%d overlay-test pod(s)", resolvedCount, totalProbed),
		))
	}

	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"ClusterDNSPodProbeSummary",
		fmt.Sprintf("Cluster DNS pod probe: %d/%d overlay-test pod(s) resolved cluster DNS", resolvedCount, totalProbed),
	))

	return status, nil
}

// probePeer issues a single HTTP probe to a peer's overlay-test-server /clusterdns
// endpoint and reports whether that pod resolved cluster DNS. Any error (timeout,
// connection failure, non-2xx status, malformed body) is treated as unresolved.
func (m *ClusterDNSPodMonitor) probePeer(ctx context.Context, peer Peer) bool {
	reqCtx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	defer cancel()

	url := "http://" + net.JoinHostPort(peer.PodIP, strconv.Itoa(m.config.ProbePort)) + m.config.ProbePath

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result clusterdns.ProbeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	return result.Resolved
}
