//go:build integration

package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Integration tests for CNI monitor using a real Kubernetes cluster.
// Run with: go test -tags=integration -v ./pkg/monitors/network/...
//
// Requirements:
// - Valid kubeconfig at ~/.kube/config or KUBECONFIG env var
// - Cluster connectivity
// - For full testing: node-doctor deployed as DaemonSet

func getKubeClient(t *testing.T) kubernetes.Interface {
	t.Helper()

	// Try KUBECONFIG env var first, then default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home directory: %v", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to build kubeconfig: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	return client
}

func TestIntegration_ClusterConnectivity(t *testing.T) {
	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify cluster is reachable
	_, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes - cluster not reachable: %v", err)
	}

	t.Log("Cluster connectivity verified")
}

func TestIntegration_PeerDiscovery_RealCluster(t *testing.T) {
	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get list of nodes to understand cluster topology
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}
	t.Logf("Cluster has %d nodes", len(nodes.Items))

	for _, node := range nodes.Items {
		var nodeIP string
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIP = addr.Address
				break
			}
		}
		t.Logf("  Node: %s (IP: %s)", node.Name, nodeIP)
	}

	// Test peer discovery with default namespace
	config := &PeerDiscoveryConfig{
		Namespace:       "node-doctor",
		LabelSelector:   "app=node-doctor",
		RefreshInterval: 1 * time.Minute,
		SelfNodeName:    "", // Will discover all pods
	}

	pd, err := NewKubernetesPeerDiscoveryWithClient(config, client)
	if err != nil {
		t.Fatalf("Failed to create peer discovery: %v", err)
	}

	// Refresh to discover peers
	err = pd.Refresh(ctx)
	if err != nil {
		t.Logf("Peer discovery refresh returned error (expected if node-doctor not deployed): %v", err)
	}

	peers := pd.GetPeers()
	t.Logf("Discovered %d peers with label selector '%s' in namespace '%s'",
		len(peers), config.LabelSelector, config.Namespace)

	for _, peer := range peers {
		t.Logf("  Peer: %s on node %s (IP: %s)", peer.Name, peer.NodeName, peer.NodeIP)
	}

	// If no peers found, this might be expected if node-doctor isn't deployed
	if len(peers) == 0 {
		t.Log("No peers found - this is expected if node-doctor DaemonSet is not deployed")
		t.Log("To fully test peer discovery, deploy node-doctor first")
	}
}

func TestIntegration_PeerDiscovery_AllNamespaces(t *testing.T) {
	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if node-doctor namespace exists
	_, err := client.CoreV1().Namespaces().Get(ctx, "node-doctor", metav1.GetOptions{})
	if err != nil {
		t.Logf("node-doctor namespace doesn't exist: %v", err)
		t.Log("Creating test pods to verify peer discovery...")

		// Create a temporary test namespace
		testNS := "node-doctor-integration-test"
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNS,
			},
		}
		_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			t.Logf("Failed to create test namespace (may already exist): %v", err)
		}
		defer func() {
			// Cleanup
			_ = client.CoreV1().Namespaces().Delete(context.Background(), testNS, metav1.DeleteOptions{})
		}()

		// Test with the temporary namespace
		config := &PeerDiscoveryConfig{
			Namespace:       testNS,
			LabelSelector:   "app=node-doctor-test",
			RefreshInterval: 1 * time.Minute,
		}

		pd, err := NewKubernetesPeerDiscoveryWithClient(config, client)
		if err != nil {
			t.Fatalf("Failed to create peer discovery: %v", err)
		}

		err = pd.Refresh(ctx)
		if err != nil {
			t.Logf("Refresh error (expected with no pods): %v", err)
		}

		peers := pd.GetPeers()
		t.Logf("Discovered %d peers in test namespace", len(peers))
	}
}

func TestIntegration_CrossNodeConnectivity(t *testing.T) {
	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get all nodes and their IPs
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	if len(nodes.Items) < 2 {
		t.Skip("Need at least 2 nodes for cross-node connectivity test")
	}

	// Extract node IPs
	var nodeIPs []string
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIPs = append(nodeIPs, addr.Address)
				break
			}
		}
	}

	t.Logf("Testing connectivity to %d node IPs", len(nodeIPs))

	// Create a real pinger (requires elevated privileges)
	pinger := newDefaultPinger()

	// Test ping to each node
	successCount := 0
	for _, ip := range nodeIPs {
		results, err := pinger.Ping(ctx, ip, 2, 2*time.Second)
		if err != nil {
			t.Logf("  Ping to %s failed: %v (may require elevated privileges)", ip, err)
			continue
		}

		// Check results
		successful := 0
		var totalLatency time.Duration
		for _, r := range results {
			if r.Success {
				successful++
				totalLatency += r.RTT
			}
		}

		if successful > 0 {
			avgLatency := totalLatency / time.Duration(successful)
			t.Logf("  ✓ Node %s: %d/%d successful, avg latency: %v", ip, successful, len(results), avgLatency)
			successCount++
		} else {
			t.Logf("  ✗ Node %s: all pings failed", ip)
		}
	}

	t.Logf("Cross-node connectivity: %d/%d nodes reachable", successCount, len(nodeIPs))

	if successCount == 0 && len(nodeIPs) > 0 {
		t.Log("No nodes reachable - this test may require elevated privileges (CAP_NET_RAW)")
		t.Log("Try running with: sudo go test -tags=integration -v -run TestIntegration_CrossNodeConnectivity")
	}
}

func TestIntegration_CNIMonitor_EndToEnd(t *testing.T) {
	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get node count
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	if len(nodes.Items) < 2 {
		t.Skip("Need at least 2 nodes for end-to-end CNI monitor test")
	}

	// Create static peers from node IPs for testing
	var peers []Peer
	for _, node := range nodes.Items {
		var nodeIP string
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIP = addr.Address
				break
			}
		}
		if nodeIP != "" {
			peers = append(peers, Peer{
				Name:     "test-" + node.Name,
				NodeName: node.Name,
				NodeIP:   nodeIP,
			})
		}
	}

	t.Logf("Created %d static peers from cluster nodes", len(peers))

	// Create CNI monitor with static peer discovery
	monitorConfig := types.MonitorConfig{
		Name:     "cni-integration-test",
		Type:     "network-cni-check",
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Config: map[string]interface{}{
			"connectivity": map[string]interface{}{
				"pingCount":         2,
				"pingTimeout":       "2s",
				"warningLatency":    "100ms",
				"criticalLatency":   "500ms",
				"minReachablePeers": 50,
			},
			"discovery": map[string]interface{}{
				"method": "static",
			},
		},
	}

	monitor, err := NewCNIMonitor(ctx, monitorConfig)
	if err != nil {
		t.Fatalf("Failed to create CNI monitor: %v", err)
	}

	// Inject static peers
	cniMon := monitor.(*CNIMonitor)
	if staticPD, ok := cniMon.peerDiscovery.(*staticPeerDiscovery); ok {
		staticPD.SetPeers(peers)
	} else {
		t.Log("Warning: Could not inject static peers")
	}

	// Run a single check cycle
	t.Log("Running CNI monitor check cycle...")
	statusChan, err := monitor.Start()
	if err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}

	// Wait for first status or timeout
	select {
	case status := <-statusChan:
		t.Logf("Received status from monitor:")
		t.Logf("  Source: %s", status.Source)
		t.Logf("  Conditions: %d", len(status.Conditions))
		for _, c := range status.Conditions {
			t.Logf("    - %s: %s (%s)", c.Type, c.Status, c.Reason)
		}
		t.Logf("  Events: %d", len(status.Events))
		for _, e := range status.Events {
			t.Logf("    - [%s] %s: %s", e.Severity, e.Reason, e.Message)
		}
	case <-time.After(30 * time.Second):
		t.Log("Timeout waiting for monitor status (ping may require elevated privileges)")
	}

	// Stop the monitor
	monitor.Stop()

	// Check peer statuses
	peerStatuses := cniMon.GetPeerStatuses()
	t.Logf("Peer statuses (%d peers):", len(peerStatuses))
	for name, ps := range peerStatuses {
		t.Logf("  %s: reachable=%v, latency=%v, failures=%d",
			name, ps.Reachable, ps.LastLatency, ps.ConsecutiveFails)
	}
}

func TestIntegration_CNIHealthChecker_RealSystem(t *testing.T) {
	// Test CNI health checker against real system CNI configuration
	checker := NewCNIHealthChecker(CNIHealthConfig{
		Enabled:         true,
		ConfigPath:      "/etc/cni/net.d",
		CheckInterfaces: true,
	})

	result := checker.CheckHealth()

	t.Logf("CNI Health Check Results:")
	t.Logf("  Overall Healthy: %v", result.Healthy)

	if result.ConfigResult != nil {
		t.Logf("  Config Check:")
		t.Logf("    Config Path: %s", result.ConfigResult.ConfigPath)
		t.Logf("    Valid Configs: %d", result.ConfigResult.ValidConfigs)
		t.Logf("    Primary Config: %s", result.ConfigResult.PrimaryConfig)
		t.Logf("    CNI Type: %s", result.ConfigResult.CNIType)
		if len(result.ConfigResult.ConfigFiles) > 0 {
			t.Logf("    Config Files: %v", result.ConfigResult.ConfigFiles)
		}
		if len(result.ConfigResult.Errors) > 0 {
			t.Logf("    Errors: %v", result.ConfigResult.Errors)
		}
	}

	if result.IfaceResult != nil {
		t.Logf("  Interface Check:")
		t.Logf("    Healthy: %v", result.IfaceResult.Healthy)
		t.Logf("    CNI Interfaces Found: %d", len(result.IfaceResult.CNIInterfaces))
		for _, iface := range result.IfaceResult.CNIInterfaces {
			t.Logf("      - %s (type: %s, up: %v, mtu: %d)",
				iface.Name, iface.Type, iface.Up, iface.MTU)
		}
	}

	if len(result.Issues) > 0 {
		t.Logf("  Issues: %v", result.Issues)
	}
}

func TestIntegration_NetworkPartitionDetection(t *testing.T) {
	// This test simulates network partition detection logic
	// without actually causing a partition

	client := getKubeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	totalNodes := len(nodes.Items)
	if totalNodes < 3 {
		t.Skip("Need at least 3 nodes to test partition detection logic")
	}

	t.Logf("Testing partition detection with %d nodes", totalNodes)

	// Test various scenarios
	testCases := []struct {
		name              string
		reachableNodes    int
		minReachablePct   float64
		expectPartitioned bool
	}{
		{
			name:              "all nodes reachable",
			reachableNodes:    totalNodes,
			minReachablePct:   50,
			expectPartitioned: false,
		},
		{
			name:              "majority reachable",
			reachableNodes:    (totalNodes / 2) + 1,
			minReachablePct:   50,
			expectPartitioned: false,
		},
		{
			name:              "minority reachable - partitioned",
			reachableNodes:    totalNodes / 3,
			minReachablePct:   50,
			expectPartitioned: true,
		},
		{
			name:              "no nodes reachable - fully partitioned",
			reachableNodes:    0,
			minReachablePct:   50,
			expectPartitioned: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reachablePct := float64(tc.reachableNodes) / float64(totalNodes) * 100
			isPartitioned := reachablePct < tc.minReachablePct

			if isPartitioned != tc.expectPartitioned {
				t.Errorf("Partition detection mismatch: reachable=%d/%d (%.1f%%), threshold=%.1f%%, expected partitioned=%v, got=%v",
					tc.reachableNodes, totalNodes, reachablePct, tc.minReachablePct, tc.expectPartitioned, isPartitioned)
			} else {
				t.Logf("✓ %d/%d nodes (%.1f%%) with %.1f%% threshold -> partitioned=%v",
					tc.reachableNodes, totalNodes, reachablePct, tc.minReachablePct, isPartitioned)
			}
		})
	}
}
