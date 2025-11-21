package network

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewKubernetesPeerDiscoveryWithClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *PeerDiscoveryConfig
		client  bool
		wantErr bool
	}{
		{
			name: "valid config with client",
			config: &PeerDiscoveryConfig{
				Namespace:       "test-ns",
				LabelSelector:   "app=test",
				RefreshInterval: 5 * time.Minute,
			},
			client:  true,
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			client:  true,
			wantErr: true,
		},
		{
			name: "nil client",
			config: &PeerDiscoveryConfig{
				Namespace: "test-ns",
			},
			client:  false,
			wantErr: true,
		},
		{
			name:    "empty config - defaults applied",
			config:  &PeerDiscoveryConfig{},
			client:  true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pd PeerDiscovery
			var err error

			if tt.client {
				client := fake.NewSimpleClientset()
				pd, err = NewKubernetesPeerDiscoveryWithClient(tt.config, client)
			} else {
				// Pass explicit nil to test nil client handling
				// (avoid Go's nil interface gotcha where typed-nil != nil)
				pd, err = NewKubernetesPeerDiscoveryWithClient(tt.config, nil)
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewKubernetesPeerDiscoveryWithClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && pd == nil {
				t.Error("NewKubernetesPeerDiscoveryWithClient() returned nil")
			}
		})
	}
}

func TestKubernetesPeerDiscovery_DiscoverPeers(t *testing.T) {
	tests := []struct {
		name         string
		selfNodeName string
		pods         []corev1.Pod
		wantPeers    int
		wantErr      bool
	}{
		{
			name:         "single peer on different node",
			selfNodeName: "node-1",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-abc",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.2",
						PodIP:  "10.0.0.2",
					},
				},
			},
			wantPeers: 1,
			wantErr:   false,
		},
		{
			name:         "exclude self node",
			selfNodeName: "node-1",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-abc",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-1", // Same as self
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.1",
						PodIP:  "10.0.0.1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-def",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.2",
						PodIP:  "10.0.0.2",
					},
				},
			},
			wantPeers: 1, // Only node-2 should be included
			wantErr:   false,
		},
		{
			name:         "skip non-running pods",
			selfNodeName: "node-1",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-pending",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodPending, // Not running
						HostIP: "10.0.0.2",
						PodIP:  "10.0.0.2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-running",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-3",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.3",
						PodIP:  "10.0.0.3",
					},
				},
			},
			wantPeers: 1, // Only the running pod
			wantErr:   false,
		},
		{
			name:         "skip pods without node IP",
			selfNodeName: "node-1",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-no-ip",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "", // No IP
						PodIP:  "",
					},
				},
			},
			wantPeers: 0,
			wantErr:   false,
		},
		{
			name:         "multiple peers",
			selfNodeName: "node-1",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-a",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.2",
						PodIP:  "10.0.0.2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-b",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-3",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.3",
						PodIP:  "10.0.0.3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node-doctor-c",
						Namespace: "node-doctor",
						Labels:    map[string]string{"app": "node-doctor"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-4",
					},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						HostIP: "10.0.0.4",
						PodIP:  "10.0.0.4",
					},
				},
			},
			wantPeers: 3,
			wantErr:   false,
		},
		{
			name:         "no pods",
			selfNodeName: "node-1",
			pods:         []corev1.Pod{},
			wantPeers:    0,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with pods
			client := fake.NewSimpleClientset()
			for _, pod := range tt.pods {
				_, err := client.CoreV1().Pods(pod.Namespace).Create(context.Background(), &pod, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create test pod: %v", err)
				}
			}

			config := &PeerDiscoveryConfig{
				Namespace:       "node-doctor",
				LabelSelector:   "app=node-doctor",
				RefreshInterval: 5 * time.Minute,
				SelfNodeName:    tt.selfNodeName,
			}

			pd, err := NewKubernetesPeerDiscoveryWithClient(config, client)
			if err != nil {
				t.Fatalf("Failed to create peer discovery: %v", err)
			}

			// Call Refresh to discover peers
			err = pd.Refresh(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Refresh() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			peers := pd.GetPeers()
			if len(peers) != tt.wantPeers {
				t.Errorf("GetPeers() returned %d peers, want %d", len(peers), tt.wantPeers)
			}
		})
	}
}

func TestKubernetesPeerDiscovery_GetPeers_ReturnsCopy(t *testing.T) {
	client := fake.NewSimpleClientset()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-doctor-test",
			Namespace: "node-doctor",
			Labels:    map[string]string{"app": "node-doctor"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-2",
		},
		Status: corev1.PodStatus{
			Phase:  corev1.PodRunning,
			HostIP: "10.0.0.2",
			PodIP:  "10.0.0.2",
		},
	}
	_, _ = client.CoreV1().Pods("node-doctor").Create(context.Background(), &pod, metav1.CreateOptions{})

	config := &PeerDiscoveryConfig{
		Namespace:     "node-doctor",
		LabelSelector: "app=node-doctor",
		SelfNodeName:  "node-1",
	}

	pd, _ := NewKubernetesPeerDiscoveryWithClient(config, client)
	_ = pd.Refresh(context.Background())

	peers1 := pd.GetPeers()
	peers2 := pd.GetPeers()

	// Modify peers1
	if len(peers1) > 0 {
		peers1[0].Name = "modified"
	}

	// peers2 should not be affected
	if len(peers2) > 0 && peers2[0].Name == "modified" {
		t.Error("GetPeers() should return a copy, not the internal slice")
	}
}

func TestStaticPeerDiscovery(t *testing.T) {
	initialPeers := []Peer{
		{Name: "peer-1", NodeName: "node-1", NodeIP: "10.0.0.1"},
		{Name: "peer-2", NodeName: "node-2", NodeIP: "10.0.0.2"},
	}

	pd := NewStaticPeerDiscovery(initialPeers)

	// Test GetPeers
	peers := pd.GetPeers()
	if len(peers) != 2 {
		t.Errorf("GetPeers() returned %d peers, want 2", len(peers))
	}

	// Test Refresh (should be no-op)
	err := pd.Refresh(context.Background())
	if err != nil {
		t.Errorf("Refresh() error = %v, want nil", err)
	}

	// Test Start (should be no-op)
	err = pd.Start(context.Background())
	if err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}

	// Test Stop (should be no-op, no panic)
	pd.Stop()
}

func TestStaticPeerDiscovery_SetPeers(t *testing.T) {
	initialPeers := []Peer{
		{Name: "peer-1", NodeName: "node-1", NodeIP: "10.0.0.1"},
	}

	pd := NewStaticPeerDiscovery(initialPeers).(*staticPeerDiscovery)

	// Update peers
	newPeers := []Peer{
		{Name: "peer-2", NodeName: "node-2", NodeIP: "10.0.0.2"},
		{Name: "peer-3", NodeName: "node-3", NodeIP: "10.0.0.3"},
	}
	pd.SetPeers(newPeers)

	// Verify update
	peers := pd.GetPeers()
	if len(peers) != 2 {
		t.Errorf("GetPeers() returned %d peers, want 2", len(peers))
	}
	if peers[0].Name != "peer-2" {
		t.Errorf("First peer name = %s, want peer-2", peers[0].Name)
	}
}

func TestKubernetesPeerDiscovery_StartStop(t *testing.T) {
	client := fake.NewSimpleClientset()
	config := &PeerDiscoveryConfig{
		Namespace:       "node-doctor",
		LabelSelector:   "app=node-doctor",
		RefreshInterval: 100 * time.Millisecond, // Short interval for testing
		SelfNodeName:    "node-1",
	}

	pd, err := NewKubernetesPeerDiscoveryWithClient(config, client)
	if err != nil {
		t.Fatalf("Failed to create peer discovery: %v", err)
	}

	// Test Start
	ctx := context.Background()
	err = pd.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Starting again should fail
	err = pd.Start(ctx)
	if err == nil {
		t.Error("Start() should fail when already running")
	}

	// Test Stop
	pd.Stop()

	// Stop again should be no-op (no panic)
	pd.Stop()
}

func TestGetNodeIP(t *testing.T) {
	tests := []struct {
		name   string
		pod    *corev1.Pod
		wantIP string
	}{
		{
			name: "has host IP",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					HostIP: "10.0.0.1",
					PodIP:  "192.168.1.1",
				},
			},
			wantIP: "10.0.0.1",
		},
		{
			name: "no host IP - use pod IP",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					HostIP: "",
					PodIP:  "192.168.1.1",
				},
			},
			wantIP: "192.168.1.1",
		},
		{
			name: "no IPs",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					HostIP: "",
					PodIP:  "",
				},
			},
			wantIP: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getNodeIP(tt.pod)
			if got != tt.wantIP {
				t.Errorf("getNodeIP() = %s, want %s", got, tt.wantIP)
			}
		})
	}
}

func TestGetNamespaceFromEnvOrDefault(t *testing.T) {
	// This function reads from environment and files, so we test the default case
	// Note: A full test would require setting up env vars and mock files

	// Clear env vars that might affect the test
	t.Setenv("POD_NAMESPACE", "")

	// Function should return "node-doctor" as default when no env/file is available
	ns := getNamespaceFromEnvOrDefault()
	if ns == "" {
		t.Error("getNamespaceFromEnvOrDefault() returned empty string")
	}

	// Test with env var set
	t.Setenv("POD_NAMESPACE", "test-namespace")
	ns = getNamespaceFromEnvOrDefault()
	if ns != "test-namespace" {
		t.Errorf("getNamespaceFromEnvOrDefault() = %s, want test-namespace", ns)
	}
}
