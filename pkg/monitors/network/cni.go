// Package network provides network health monitoring capabilities.
package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/monitors"
	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default configuration values for CNI monitor
	defaultCNIPingCount             = 3
	defaultCNIPingTimeout           = 5 * time.Second
	defaultCNIWarningLatency        = 50 * time.Millisecond
	defaultCNICriticalLatency       = 200 * time.Millisecond
	defaultCNIFailureThreshold      = 3
	defaultCNIMinReachablePeers     = 80 // percentage
	defaultCNIRefreshInterval       = 5 * time.Minute
	defaultCNINamespace             = "node-doctor"
	defaultCNILabelSelector         = "app=node-doctor"
)

// CNIMonitorConfig holds the configuration for the CNI connectivity monitor.
type CNIMonitorConfig struct {
	// Discovery configuration
	Discovery DiscoveryConfig

	// Connectivity configuration
	Connectivity ConnectivityConfig

	// CNI health check configuration
	CNIHealth CNIHealthConfig
}

// DiscoveryConfig holds peer discovery configuration.
type DiscoveryConfig struct {
	// Method is the discovery method ("kubernetes" or "static").
	Method string
	// Namespace to search for peers (for kubernetes method).
	Namespace string
	// LabelSelector for filtering pods (for kubernetes method).
	LabelSelector string
	// RefreshInterval is how often to refresh the peer list.
	RefreshInterval time.Duration
	// StaticPeers is a list of static peer IPs (for static method).
	StaticPeers []string
}

// ConnectivityConfig holds connectivity check configuration.
type ConnectivityConfig struct {
	// PingCount is the number of pings to send per peer.
	PingCount int
	// PingTimeout is the timeout for each ping.
	PingTimeout time.Duration
	// WarningLatency is the latency threshold for warnings.
	WarningLatency time.Duration
	// CriticalLatency is the latency threshold for critical conditions.
	CriticalLatency time.Duration
	// FailureThreshold is consecutive failures before marking peer unreachable.
	FailureThreshold int
	// MinReachablePeers is the percentage of peers that must be reachable.
	MinReachablePeers int
}

// CNIHealthConfig holds CNI health check configuration.
type CNIHealthConfig struct {
	// Enabled indicates whether CNI health checks are enabled.
	Enabled bool
	// ConfigPath is the path to CNI configuration directory.
	ConfigPath string
	// CheckInterfaces enables interface health checking.
	CheckInterfaces bool
	// ExpectedInterfaces is a list of expected CNI interfaces.
	ExpectedInterfaces []string
}

// PeerStatus tracks the status of a single peer.
type PeerStatus struct {
	Peer             Peer
	Reachable        bool
	LastLatency      time.Duration
	AvgLatency       time.Duration
	FailureCount     int
	LastCheck        time.Time
	LastSuccess      time.Time
	ConsecutiveFails int
}

// CNIMonitor monitors CNI connectivity and cross-node health.
type CNIMonitor struct {
	name           string
	config         *CNIMonitorConfig
	peerDiscovery  PeerDiscovery
	pinger         Pinger
	cniHealthCheck CNIHealthChecker
	mu             sync.Mutex
	peerStatuses   map[string]*PeerStatus // keyed by node name
	started        bool

	*monitors.BaseMonitor
}

// init registers the CNI monitor with the monitor registry.
func init() {
	monitors.Register(monitors.MonitorInfo{
		Type:        "network-cni-check",
		Factory:     NewCNIMonitor,
		Validator:   ValidateCNIConfig,
		Description: "Monitors CNI connectivity and cross-node network health using peer discovery and ICMP ping",
	})
}

// NewCNIMonitor creates a new CNI connectivity monitor instance.
func NewCNIMonitor(ctx context.Context, config types.MonitorConfig) (types.Monitor, error) {
	// Parse CNI-specific configuration
	cniConfig, err := parseCNIConfig(config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CNI config: %w", err)
	}

	// Create base monitor for lifecycle management
	baseMonitor, err := monitors.NewBaseMonitor(config.Name, config.Interval, config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create base monitor: %w", err)
	}

	// Create peer discovery based on method
	var peerDiscovery PeerDiscovery
	switch cniConfig.Discovery.Method {
	case "kubernetes", "":
		pdConfig := &PeerDiscoveryConfig{
			Namespace:       cniConfig.Discovery.Namespace,
			LabelSelector:   cniConfig.Discovery.LabelSelector,
			RefreshInterval: cniConfig.Discovery.RefreshInterval,
		}
		peerDiscovery, err = NewKubernetesPeerDiscovery(pdConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes peer discovery: %w", err)
		}
	case "static":
		peers := make([]Peer, 0, len(cniConfig.Discovery.StaticPeers))
		for i, ip := range cniConfig.Discovery.StaticPeers {
			peers = append(peers, Peer{
				Name:     fmt.Sprintf("static-peer-%d", i),
				NodeName: fmt.Sprintf("node-%d", i),
				NodeIP:   ip,
				PodIP:    ip,
				LastSeen: time.Now(),
			})
		}
		peerDiscovery = NewStaticPeerDiscovery(peers)
	default:
		return nil, fmt.Errorf("invalid discovery method: %s", cniConfig.Discovery.Method)
	}

	// Create CNI health checker if enabled
	var cniHealthCheck CNIHealthChecker
	if cniConfig.CNIHealth.Enabled {
		cniHealthCheck = NewCNIHealthChecker(cniConfig.CNIHealth)
	}

	// Create CNI monitor
	monitor := &CNIMonitor{
		name:           config.Name,
		config:         cniConfig,
		peerDiscovery:  peerDiscovery,
		pinger:         newDefaultPinger(),
		cniHealthCheck: cniHealthCheck,
		peerStatuses:   make(map[string]*PeerStatus),
		BaseMonitor:    baseMonitor,
	}

	// Set the check function
	if err := baseMonitor.SetCheckFunc(monitor.checkCNI); err != nil {
		return nil, fmt.Errorf("failed to set check function: %w", err)
	}

	return monitor, nil
}

// Start starts the CNI monitor with peer discovery.
func (m *CNIMonitor) Start() (<-chan *types.Status, error) {
	// Start peer discovery first
	ctx := context.Background()
	if err := m.peerDiscovery.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start peer discovery: %w", err)
	}

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	// Start base monitor
	return m.BaseMonitor.Start()
}

// Stop stops the CNI monitor and peer discovery.
func (m *CNIMonitor) Stop() {
	m.mu.Lock()
	m.started = false
	m.mu.Unlock()

	// Stop peer discovery
	m.peerDiscovery.Stop()

	// Stop base monitor
	m.BaseMonitor.Stop()
}

// parseCNIConfig parses the CNI monitor configuration from a map.
func parseCNIConfig(configMap map[string]interface{}) (*CNIMonitorConfig, error) {
	config := &CNIMonitorConfig{
		Discovery: DiscoveryConfig{
			Method:          "kubernetes",
			Namespace:       defaultCNINamespace,
			LabelSelector:   defaultCNILabelSelector,
			RefreshInterval: defaultCNIRefreshInterval,
		},
		Connectivity: ConnectivityConfig{
			PingCount:         defaultCNIPingCount,
			PingTimeout:       defaultCNIPingTimeout,
			WarningLatency:    defaultCNIWarningLatency,
			CriticalLatency:   defaultCNICriticalLatency,
			FailureThreshold:  defaultCNIFailureThreshold,
			MinReachablePeers: defaultCNIMinReachablePeers,
		},
		CNIHealth: CNIHealthConfig{
			Enabled:    true,
			ConfigPath: "/etc/cni/net.d",
		},
	}

	if configMap == nil {
		return config, nil
	}

	// Parse discovery config
	if discoveryMap, ok := configMap["discovery"].(map[string]interface{}); ok {
		if method, ok := discoveryMap["method"].(string); ok {
			config.Discovery.Method = method
		}
		if namespace, ok := discoveryMap["namespace"].(string); ok {
			config.Discovery.Namespace = namespace
		}
		if labelSelector, ok := discoveryMap["labelSelector"].(string); ok {
			config.Discovery.LabelSelector = labelSelector
		}
		if refreshInterval, ok := discoveryMap["refreshInterval"]; ok {
			duration, err := parseDuration(refreshInterval)
			if err != nil {
				return nil, fmt.Errorf("invalid discovery.refreshInterval: %w", err)
			}
			config.Discovery.RefreshInterval = duration
		}
		if staticPeers, ok := discoveryMap["staticPeers"].([]interface{}); ok {
			for _, p := range staticPeers {
				if peerIP, ok := p.(string); ok {
					config.Discovery.StaticPeers = append(config.Discovery.StaticPeers, peerIP)
				}
			}
		}
	}

	// Parse connectivity config
	if connMap, ok := configMap["connectivity"].(map[string]interface{}); ok {
		if pingCount, ok := connMap["pingCount"]; ok {
			switch v := pingCount.(type) {
			case int:
				config.Connectivity.PingCount = v
			case float64:
				config.Connectivity.PingCount = int(v)
			}
		}
		if pingTimeout, ok := connMap["pingTimeout"]; ok {
			duration, err := parseDuration(pingTimeout)
			if err != nil {
				return nil, fmt.Errorf("invalid connectivity.pingTimeout: %w", err)
			}
			config.Connectivity.PingTimeout = duration
		}
		if warningLatency, ok := connMap["warningLatency"]; ok {
			duration, err := parseDuration(warningLatency)
			if err != nil {
				return nil, fmt.Errorf("invalid connectivity.warningLatency: %w", err)
			}
			config.Connectivity.WarningLatency = duration
		}
		if criticalLatency, ok := connMap["criticalLatency"]; ok {
			duration, err := parseDuration(criticalLatency)
			if err != nil {
				return nil, fmt.Errorf("invalid connectivity.criticalLatency: %w", err)
			}
			config.Connectivity.CriticalLatency = duration
		}
		if failureThreshold, ok := connMap["failureThreshold"]; ok {
			switch v := failureThreshold.(type) {
			case int:
				config.Connectivity.FailureThreshold = v
			case float64:
				config.Connectivity.FailureThreshold = int(v)
			}
		}
		if minReachable, ok := connMap["minReachablePeers"]; ok {
			switch v := minReachable.(type) {
			case int:
				config.Connectivity.MinReachablePeers = v
			case float64:
				config.Connectivity.MinReachablePeers = int(v)
			}
		}
	}

	// Parse CNI health config
	if cniMap, ok := configMap["cniHealth"].(map[string]interface{}); ok {
		if enabled, ok := cniMap["enabled"].(bool); ok {
			config.CNIHealth.Enabled = enabled
		}
		if configPath, ok := cniMap["configPath"].(string); ok {
			config.CNIHealth.ConfigPath = configPath
		}
		if checkInterfaces, ok := cniMap["checkInterfaces"].(bool); ok {
			config.CNIHealth.CheckInterfaces = checkInterfaces
		}
		if expectedInterfaces, ok := cniMap["expectedInterfaces"].([]interface{}); ok {
			for _, iface := range expectedInterfaces {
				if ifaceName, ok := iface.(string); ok {
					config.CNIHealth.ExpectedInterfaces = append(config.CNIHealth.ExpectedInterfaces, ifaceName)
				}
			}
		}
	}

	return config, nil
}

// ValidateCNIConfig validates the CNI monitor configuration.
func ValidateCNIConfig(config types.MonitorConfig) error {
	_, err := parseCNIConfig(config.Config)
	return err
}

// checkCNI performs the CNI connectivity check.
func (m *CNIMonitor) checkCNI(ctx context.Context) (*types.Status, error) {
	status := types.NewStatus(m.name)

	// Perform CNI health check if enabled
	if m.cniHealthCheck != nil {
		healthResult := m.cniHealthCheck.CheckHealth()
		AddCNIHealthToStatus(status, healthResult)
	}

	// Get current peers
	peers := m.peerDiscovery.GetPeers()

	// If no peers found, report a warning but don't fail
	if len(peers) == 0 {
		status.AddEvent(types.NewEvent(
			types.EventWarning,
			"NoPeersFound",
			"No peer node-doctor instances found for connectivity testing",
		))
		return status, nil
	}

	// Check connectivity to each peer
	reachableCount := 0
	unreachablePeers := make([]string, 0)
	highLatencyPeers := make([]string, 0)
	var totalLatency time.Duration

	for _, peer := range peers {
		peerStatus := m.checkPeerConnectivity(ctx, peer)

		m.mu.Lock()
		m.peerStatuses[peer.NodeName] = peerStatus
		m.mu.Unlock()

		if peerStatus.Reachable {
			reachableCount++
			totalLatency += peerStatus.AvgLatency

			// Check for high latency
			if peerStatus.AvgLatency > m.config.Connectivity.CriticalLatency {
				highLatencyPeers = append(highLatencyPeers, fmt.Sprintf("%s (%.2fms)", peer.NodeName, float64(peerStatus.AvgLatency)/float64(time.Millisecond)))
				status.AddEvent(types.NewEvent(
					types.EventWarning,
					"HighPeerLatency",
					fmt.Sprintf("High latency to peer %s: %.2fms (critical threshold: %.2fms)",
						peer.NodeName, float64(peerStatus.AvgLatency)/float64(time.Millisecond),
						float64(m.config.Connectivity.CriticalLatency)/float64(time.Millisecond)),
				))
			} else if peerStatus.AvgLatency > m.config.Connectivity.WarningLatency {
				highLatencyPeers = append(highLatencyPeers, fmt.Sprintf("%s (%.2fms)", peer.NodeName, float64(peerStatus.AvgLatency)/float64(time.Millisecond)))
			}
		} else {
			unreachablePeers = append(unreachablePeers, peer.NodeName)

			// Check if this is a persistent failure
			if peerStatus.ConsecutiveFails >= m.config.Connectivity.FailureThreshold {
				status.AddEvent(types.NewEvent(
					types.EventError,
					"PeerUnreachable",
					fmt.Sprintf("Peer %s has been unreachable for %d consecutive checks",
						peer.NodeName, peerStatus.ConsecutiveFails),
				))
			}
		}
	}

	// Calculate reachability percentage
	totalPeers := len(peers)
	reachablePercent := (reachableCount * 100) / totalPeers

	// Determine overall network health
	if reachablePercent < m.config.Connectivity.MinReachablePeers {
		// Network is partitioned
		status.AddCondition(types.NewCondition(
			"NetworkPartitioned",
			types.ConditionTrue,
			"InsufficientPeerReachability",
			fmt.Sprintf("Only %d%% of peers are reachable (threshold: %d%%). Unreachable: %v",
				reachablePercent, m.config.Connectivity.MinReachablePeers, unreachablePeers),
		))
	} else {
		// Network connectivity is healthy
		status.AddCondition(types.NewCondition(
			"NetworkPartitioned",
			types.ConditionFalse,
			"SufficientPeerReachability",
			fmt.Sprintf("%d%% of peers are reachable (%d/%d)", reachablePercent, reachableCount, totalPeers),
		))
	}

	// Add network degraded condition if there are high latency peers
	if len(highLatencyPeers) > 0 {
		status.AddCondition(types.NewCondition(
			"NetworkDegraded",
			types.ConditionTrue,
			"HighLatencyDetected",
			fmt.Sprintf("High latency detected to %d peers: %v", len(highLatencyPeers), highLatencyPeers),
		))
	} else {
		status.AddCondition(types.NewCondition(
			"NetworkDegraded",
			types.ConditionFalse,
			"NormalLatency",
			"Network latency is within acceptable thresholds",
		))
	}

	// Add summary info event
	var avgLatencyStr string
	if reachableCount > 0 {
		avgLatency := totalLatency / time.Duration(reachableCount)
		avgLatencyStr = fmt.Sprintf(", avg latency=%.2fms", float64(avgLatency)/float64(time.Millisecond))
	}
	status.AddEvent(types.NewEvent(
		types.EventInfo,
		"CNIConnectivitySummary",
		fmt.Sprintf("Peer connectivity: %d/%d reachable (%d%%)%s",
			reachableCount, totalPeers, reachablePercent, avgLatencyStr),
	))

	return status, nil
}

// checkPeerConnectivity checks connectivity to a single peer.
func (m *CNIMonitor) checkPeerConnectivity(ctx context.Context, peer Peer) *PeerStatus {
	m.mu.Lock()
	existingStatus, exists := m.peerStatuses[peer.NodeName]
	m.mu.Unlock()

	peerStatus := &PeerStatus{
		Peer:      peer,
		LastCheck: time.Now(),
	}

	if exists {
		peerStatus.ConsecutiveFails = existingStatus.ConsecutiveFails
		peerStatus.FailureCount = existingStatus.FailureCount
	}

	// Ping the peer
	results, err := m.pinger.Ping(ctx, peer.NodeIP, m.config.Connectivity.PingCount, m.config.Connectivity.PingTimeout)
	if err != nil {
		peerStatus.Reachable = false
		peerStatus.ConsecutiveFails++
		peerStatus.FailureCount++
		return peerStatus
	}

	// Analyze ping results
	successCount := 0
	var totalRTT time.Duration

	for _, result := range results {
		if result.Success {
			successCount++
			totalRTT += result.RTT
			peerStatus.LastLatency = result.RTT
		}
	}

	// Majority of pings must succeed
	if successCount > len(results)/2 {
		peerStatus.Reachable = true
		peerStatus.AvgLatency = totalRTT / time.Duration(successCount)
		peerStatus.LastSuccess = time.Now()
		peerStatus.ConsecutiveFails = 0
	} else {
		peerStatus.Reachable = false
		peerStatus.ConsecutiveFails++
		peerStatus.FailureCount++
	}

	return peerStatus
}

// GetPeerStatuses returns the current peer status map.
// This is useful for debugging and monitoring.
func (m *CNIMonitor) GetPeerStatuses() map[string]*PeerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return a copy
	statuses := make(map[string]*PeerStatus, len(m.peerStatuses))
	for k, v := range m.peerStatuses {
		statusCopy := *v
		statuses[k] = &statusCopy
	}
	return statuses
}
