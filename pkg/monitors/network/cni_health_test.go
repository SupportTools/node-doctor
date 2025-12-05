package network

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

func TestNewCNIHealthChecker(t *testing.T) {
	tests := []struct {
		name       string
		config     CNIHealthConfig
		wantPath   string
		wantCheck  bool
		wantIfaces []string
	}{
		{
			name:       "empty config uses defaults",
			config:     CNIHealthConfig{},
			wantPath:   "/etc/cni/net.d",
			wantCheck:  false,
			wantIfaces: nil,
		},
		{
			name: "custom config path",
			config: CNIHealthConfig{
				ConfigPath: "/custom/cni/path",
			},
			wantPath:   "/custom/cni/path",
			wantCheck:  false,
			wantIfaces: nil,
		},
		{
			name: "with interface checking enabled",
			config: CNIHealthConfig{
				ConfigPath:      "/etc/cni/net.d",
				CheckInterfaces: true,
			},
			wantPath:   "/etc/cni/net.d",
			wantCheck:  true,
			wantIfaces: nil,
		},
		{
			name: "with expected interfaces",
			config: CNIHealthConfig{
				ConfigPath:         "/etc/cni/net.d",
				ExpectedInterfaces: []string{"cni0", "flannel.1"},
			},
			wantPath:   "/etc/cni/net.d",
			wantCheck:  false,
			wantIfaces: []string{"cni0", "flannel.1"},
		},
		{
			name: "full config",
			config: CNIHealthConfig{
				Enabled:            true,
				ConfigPath:         "/custom/path",
				CheckInterfaces:    true,
				ExpectedInterfaces: []string{"calico0", "veth123"},
			},
			wantPath:   "/custom/path",
			wantCheck:  true,
			wantIfaces: []string{"calico0", "veth123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewCNIHealthChecker(tt.config)
			if checker == nil {
				t.Fatal("NewCNIHealthChecker returned nil")
			}

			// Type assert to access internal fields for verification
			impl, ok := checker.(*cniHealthChecker)
			if !ok {
				t.Fatal("Expected *cniHealthChecker type")
			}

			if impl.configPath != tt.wantPath {
				t.Errorf("configPath = %s, want %s", impl.configPath, tt.wantPath)
			}
			if impl.checkInterfaces != tt.wantCheck {
				t.Errorf("checkInterfaces = %v, want %v", impl.checkInterfaces, tt.wantCheck)
			}
			if len(impl.expectedInterfaces) != len(tt.wantIfaces) {
				t.Errorf("expectedInterfaces len = %d, want %d", len(impl.expectedInterfaces), len(tt.wantIfaces))
			}
		})
	}
}

func TestDetectCNIConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string // returns base temp dir (simulates /host)
		wantPath string                    // expected path suffix (not full path)
	}{
		{
			name: "prefers RKE2 path when it exists with configs",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				// Create RKE2 CNI path with config file
				rke2Path := filepath.Join(base, "var/lib/rancher/rke2/agent/etc/cni/net.d")
				if err := os.MkdirAll(rke2Path, 0755); err != nil {
					t.Fatalf("failed to create RKE2 path: %v", err)
				}
				if err := os.WriteFile(filepath.Join(rke2Path, "10-calico.conflist"), []byte(`{"test":"config"}`), 0644); err != nil {
					t.Fatalf("failed to create config file: %v", err)
				}
				return base
			},
			wantPath: "var/lib/rancher/rke2/agent/etc/cni/net.d",
		},
		{
			name: "prefers K3s path when it exists with configs",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				// Create K3s CNI path with config file
				k3sPath := filepath.Join(base, "var/lib/rancher/k3s/agent/etc/cni/net.d")
				if err := os.MkdirAll(k3sPath, 0755); err != nil {
					t.Fatalf("failed to create K3s path: %v", err)
				}
				if err := os.WriteFile(filepath.Join(k3sPath, "10-flannel.conflist"), []byte(`{"test":"config"}`), 0644); err != nil {
					t.Fatalf("failed to create config file: %v", err)
				}
				return base
			},
			wantPath: "var/lib/rancher/k3s/agent/etc/cni/net.d",
		},
		{
			name: "prefers standard path when distro paths have no configs",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				// Create all paths but only standard has config
				rke2Path := filepath.Join(base, "var/lib/rancher/rke2/agent/etc/cni/net.d")
				k3sPath := filepath.Join(base, "var/lib/rancher/k3s/agent/etc/cni/net.d")
				stdPath := filepath.Join(base, "etc/cni/net.d")
				for _, p := range []string{rke2Path, k3sPath, stdPath} {
					if err := os.MkdirAll(p, 0755); err != nil {
						t.Fatalf("failed to create path: %v", err)
					}
				}
				// Only standard path has config
				if err := os.WriteFile(filepath.Join(stdPath, "10-bridge.conf"), []byte(`{"test":"config"}`), 0644); err != nil {
					t.Fatalf("failed to create config file: %v", err)
				}
				return base
			},
			wantPath: "etc/cni/net.d",
		},
		{
			name: "RKE2 takes priority over K3s",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				// Create both RKE2 and K3s paths with configs
				rke2Path := filepath.Join(base, "var/lib/rancher/rke2/agent/etc/cni/net.d")
				k3sPath := filepath.Join(base, "var/lib/rancher/k3s/agent/etc/cni/net.d")
				for _, p := range []string{rke2Path, k3sPath} {
					if err := os.MkdirAll(p, 0755); err != nil {
						t.Fatalf("failed to create path: %v", err)
					}
					if err := os.WriteFile(filepath.Join(p, "10-calico.conflist"), []byte(`{"test":"config"}`), 0644); err != nil {
						t.Fatalf("failed to create config file: %v", err)
					}
				}
				return base
			},
			wantPath: "var/lib/rancher/rke2/agent/etc/cni/net.d",
		},
		{
			name: "ignores empty directories",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				// Create RKE2 path but empty, standard has config
				rke2Path := filepath.Join(base, "var/lib/rancher/rke2/agent/etc/cni/net.d")
				stdPath := filepath.Join(base, "etc/cni/net.d")
				for _, p := range []string{rke2Path, stdPath} {
					if err := os.MkdirAll(p, 0755); err != nil {
						t.Fatalf("failed to create path: %v", err)
					}
				}
				// Only standard has config file
				if err := os.WriteFile(filepath.Join(stdPath, "10-bridge.conf"), []byte(`{"test":"config"}`), 0644); err != nil {
					t.Fatalf("failed to create config file: %v", err)
				}
				return base
			},
			wantPath: "etc/cni/net.d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use detectCNIConfigPathWithRoot with the temp dir as host root
			// This properly tests the path detection logic
			base := tt.setup(t)
			expectedFull := filepath.Join(base, tt.wantPath)

			// Call the testable function with temp dir as host root
			result := detectCNIConfigPathWithRoot(base)

			if result != expectedFull {
				t.Errorf("detectCNIConfigPathWithRoot(%q) = %q, want %q", base, result, expectedFull)
			}
		})
	}
}

func TestDetectCNIConfigPathWithRoot_HostRootPrefix(t *testing.T) {
	// This test simulates the containerized deployment scenario where
	// the host filesystem is mounted at /host (or a custom prefix)
	tests := []struct {
		name      string
		hostRoot  string
		setup     func(t *testing.T, hostRoot string)
		wantPath  string
	}{
		{
			name:     "finds RKE2 config under host root",
			hostRoot: "/host",
			setup: func(t *testing.T, hostRoot string) {
				base := t.TempDir()
				// Create simulated /host/var/lib/rancher/rke2/... structure
				rke2Path := filepath.Join(base, "var/lib/rancher/rke2/agent/etc/cni/net.d")
				if err := os.MkdirAll(rke2Path, 0755); err != nil {
					t.Fatalf("failed to create path: %v", err)
				}
				if err := os.WriteFile(filepath.Join(rke2Path, "10-calico.conflist"), []byte(`{"test":"config"}`), 0644); err != nil {
					t.Fatalf("failed to create config file: %v", err)
				}
				// Override hostRoot to use temp dir
				t.Setenv("TEST_HOST_ROOT", base)
			},
			wantPath: "var/lib/rancher/rke2/agent/etc/cni/net.d",
		},
		{
			name:     "empty host root falls back to direct paths",
			hostRoot: "",
			setup: func(t *testing.T, hostRoot string) {
				// With empty host root, it should check direct paths
				// This simulates running directly on host (not in container)
			},
			wantPath: "/etc/cni/net.d", // Falls back to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t, tt.hostRoot)
			}

			if tt.name == "finds RKE2 config under host root" {
				// Use the temp dir set in setup
				base := os.Getenv("TEST_HOST_ROOT")
				result := detectCNIConfigPathWithRoot(base)
				expectedFull := filepath.Join(base, tt.wantPath)
				if result != expectedFull {
					t.Errorf("detectCNIConfigPathWithRoot(%q) = %q, want %q", base, result, expectedFull)
				}
			} else {
				// Test with empty host root
				result := detectCNIConfigPathWithRoot("")
				if result != tt.wantPath {
					t.Errorf("detectCNIConfigPathWithRoot(\"\") = %q, want %q", result, tt.wantPath)
				}
			}
		})
	}
}

func TestIsCNIConfigFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"10-calico.conflist", true},
		{"10-flannel.conf", true},
		{"bridge.json", true},
		{"calico-kubeconfig", false},
		{"README.md", false},
		{"config.yaml", false},
		{".conf", true},      // edge case - just extension
		{"test.CONF", false}, // case sensitive
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCNIConfigFile(tt.name)
			if got != tt.want {
				t.Errorf("isCNIConfigFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDetectCNIType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
		want     string
	}{
		// Filename-based detection
		{
			name:     "calico from filename",
			filename: "10-calico.conflist",
			content:  "{}",
			want:     "calico",
		},
		{
			name:     "flannel from filename",
			filename: "10-flannel.conflist",
			content:  "{}",
			want:     "flannel",
		},
		{
			name:     "weave from filename",
			filename: "10-weave.conf",
			content:  "{}",
			want:     "weave",
		},
		{
			name:     "cilium from filename",
			filename: "05-cilium.conf",
			content:  "{}",
			want:     "cilium",
		},
		{
			name:     "canal from filename",
			filename: "10-canal.conflist",
			content:  "{}",
			want:     "canal",
		},
		{
			name:     "antrea from filename",
			filename: "10-antrea.conflist",
			content:  "{}",
			want:     "antrea",
		},
		{
			name:     "multus from filename",
			filename: "00-multus.conf",
			content:  "{}",
			want:     "multus",
		},
		// Content-based detection
		{
			name:     "calico from content with spaces",
			filename: "10-cni.conflist",
			content:  `{"type": "calico", "name": "k8s-pod-network"}`,
			want:     "calico",
		},
		{
			name:     "calico from content no spaces",
			filename: "10-cni.conflist",
			content:  `{"type":"calico","name":"k8s-pod-network"}`,
			want:     "calico",
		},
		{
			name:     "flannel from content",
			filename: "10-cni.conflist",
			content:  `{"type": "flannel", "delegate": {}}`,
			want:     "flannel",
		},
		{
			name:     "weave from content",
			filename: "10-cni.conflist",
			content:  `{"type": "weave-net", "hairpinMode": true}`,
			want:     "weave",
		},
		{
			name:     "cilium from content",
			filename: "10-cni.conflist",
			content:  `{"type": "cilium-cni", "enable-debug": false}`,
			want:     "cilium",
		},
		{
			name:     "bridge from content",
			filename: "10-cni.conflist",
			content:  `{"type": "bridge", "bridge": "cni0"}`,
			want:     "bridge",
		},
		{
			name:     "unknown type",
			filename: "10-custom.conflist",
			content:  `{"type": "custom-cni", "name": "my-network"}`,
			want:     "unknown",
		},
		{
			name:     "empty content",
			filename: "10-unknown.conflist",
			content:  "",
			want:     "unknown",
		},
		// Case insensitivity
		{
			name:     "CALICO uppercase filename",
			filename: "10-CALICO.conflist",
			content:  "{}",
			want:     "calico",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCNIType(tt.filename, tt.content)
			if got != tt.want {
				t.Errorf("detectCNIType(%q, %q) = %q, want %q", tt.filename, tt.content, got, tt.want)
			}
		})
	}
}

func TestClassifyCNIInterface(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// Calico interfaces
		{"cali123abc", "calico"},
		{"cali0", "calico"},
		{"califabcdef12345", "calico"},
		// Flannel interfaces
		{"flannel0", "flannel"},
		{"flannel.1", "flannel"},
		{"flannel-vxlan", "flannel"},
		// Weave interfaces
		{"weave", "weave"},
		{"weave-bridge", "weave"},
		// Cilium interfaces
		{"cilium_host", "cilium"},
		{"cilium_net", "cilium"},
		{"lxc12345", "cilium"},
		{"lxcabcdef", "cilium"},
		// Generic CNI interfaces
		{"cni0", "cni-generic"},
		{"cni-bridge", "cni-generic"},
		// Veth interfaces
		{"veth123", "veth"},
		{"vethfabcde12", "veth"},
		// Docker
		{"docker0", "docker"},
		// Kubenet
		{"cbr0", "kubenet"},
		// Non-CNI interfaces (should return empty)
		{"eth0", ""},
		{"lo", ""},
		{"enp0s3", ""},
		{"bond0", ""},
		{"br-12345", ""},
		{"virbr0", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCNIInterface(tt.name)
			if got != tt.want {
				t.Errorf("classifyCNIInterface(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCheckConfigFiles(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) string // returns temp dir path
		wantHealthy bool
		wantConfigs int
		wantPrimary string
		wantCNIType string
		wantErrors  int
	}{
		{
			name: "valid calico config",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				content := `{"name": "k8s-pod-network", "type": "calico"}`
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true,
			wantConfigs: 1,
			wantPrimary: "10-calico.conflist",
			wantCNIType: "calico",
			wantErrors:  0,
		},
		{
			name: "valid flannel config",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				content := `{"name": "cbr0", "type": "flannel"}`
				if err := os.WriteFile(filepath.Join(dir, "10-flannel.conflist"), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true,
			wantConfigs: 1,
			wantPrimary: "10-flannel.conflist",
			wantCNIType: "flannel",
			wantErrors:  0,
		},
		{
			name: "multiple configs - first wins",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// First in lexicographic order
				if err := os.WriteFile(filepath.Join(dir, "05-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				// Second in lexicographic order
				if err := os.WriteFile(filepath.Join(dir, "10-flannel.conflist"), []byte(`{"type": "flannel"}`), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true,
			wantConfigs: 2,
			wantPrimary: "05-calico.conflist",
			wantCNIType: "calico",
			wantErrors:  0,
		},
		{
			name: "empty directory",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantHealthy: false,
			wantConfigs: 0,
			wantPrimary: "",
			wantCNIType: "",
			wantErrors:  1, // "No valid CNI configuration files found"
		},
		{
			name: "directory does not exist",
			setup: func(t *testing.T) string {
				return "/nonexistent/path/that/does/not/exist"
			},
			wantHealthy: false,
			wantConfigs: 0,
			wantPrimary: "",
			wantCNIType: "",
			wantErrors:  1,
		},
		{
			name: "empty config file",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "10-empty.conflist"), []byte(""), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: false,
			wantConfigs: 0,
			wantPrimary: "",
			wantCNIType: "",
			wantErrors:  2, // empty file error + no valid configs
		},
		{
			name: "invalid JSON config",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Not valid JSON (doesn't start with {)
				if err := os.WriteFile(filepath.Join(dir, "10-invalid.conflist"), []byte("not json"), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: false,
			wantConfigs: 0,
			wantPrimary: "",
			wantCNIType: "",
			wantErrors:  2, // invalid JSON error + no valid configs
		},
		{
			name: "mixed valid and invalid configs",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Valid config
				if err := os.WriteFile(filepath.Join(dir, "10-valid.conflist"), []byte(`{"type": "bridge"}`), 0644); err != nil {
					t.Fatal(err)
				}
				// Invalid config (empty)
				if err := os.WriteFile(filepath.Join(dir, "20-invalid.conf"), []byte(""), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true, // Has at least one valid config
			wantConfigs: 1,
			wantPrimary: "10-valid.conflist",
			wantCNIType: "bridge",
			wantErrors:  1, // Error for empty file
		},
		{
			name: "ignores non-config files",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				// Non-config files should be ignored
				if err := os.WriteFile(filepath.Join(dir, "calico-kubeconfig"), []byte("kubeconfig content"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# README"), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true,
			wantConfigs: 1,
			wantPrimary: "10-calico.conflist",
			wantCNIType: "calico",
			wantErrors:  0,
		},
		{
			name: "path is a file not directory",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				filePath := filepath.Join(dir, "not-a-dir")
				if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantHealthy: false,
			wantConfigs: 0,
			wantPrimary: "",
			wantCNIType: "",
			wantErrors:  1,
		},
		{
			name: "subdirectories are ignored",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				subDir := filepath.Join(dir, "subdir")
				if err := os.Mkdir(subDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Config in subdir should be ignored
				if err := os.WriteFile(filepath.Join(subDir, "10-flannel.conflist"), []byte(`{"type": "flannel"}`), 0644); err != nil {
					t.Fatal(err)
				}
				// Config in main dir
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantHealthy: true,
			wantConfigs: 1,
			wantPrimary: "10-calico.conflist",
			wantCNIType: "calico",
			wantErrors:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setup(t)

			checker := NewCNIHealthChecker(CNIHealthConfig{
				ConfigPath: configPath,
			})

			result := checker.CheckConfigFiles()

			if result.Healthy != tt.wantHealthy {
				t.Errorf("Healthy = %v, want %v", result.Healthy, tt.wantHealthy)
			}
			if result.ValidConfigs != tt.wantConfigs {
				t.Errorf("ValidConfigs = %d, want %d", result.ValidConfigs, tt.wantConfigs)
			}
			if result.PrimaryConfig != tt.wantPrimary {
				t.Errorf("PrimaryConfig = %q, want %q", result.PrimaryConfig, tt.wantPrimary)
			}
			if result.CNIType != tt.wantCNIType {
				t.Errorf("CNIType = %q, want %q", result.CNIType, tt.wantCNIType)
			}
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("Errors count = %d, want %d. Errors: %v", len(result.Errors), tt.wantErrors, result.Errors)
			}
		})
	}
}

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name              string
		setup             func(t *testing.T) string
		checkInterfaces   bool
		expectedIfaces    []string
		wantHealthy       bool
		wantConfigHealthy bool
		wantIfaceResult   bool
	}{
		{
			name: "healthy config no interface check",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			checkInterfaces:   false,
			expectedIfaces:    nil,
			wantHealthy:       true,
			wantConfigHealthy: true,
			wantIfaceResult:   false,
		},
		{
			name: "unhealthy config",
			setup: func(t *testing.T) string {
				return t.TempDir() // Empty directory
			},
			checkInterfaces:   false,
			expectedIfaces:    nil,
			wantHealthy:       false,
			wantConfigHealthy: false,
			wantIfaceResult:   false,
		},
		{
			name: "healthy config with interface check enabled",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			checkInterfaces:   true,
			expectedIfaces:    nil,
			wantHealthy:       true,
			wantConfigHealthy: true,
			wantIfaceResult:   true,
		},
		{
			name: "healthy config with expected interfaces (will fail due to missing)",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			checkInterfaces:   false,
			expectedIfaces:    []string{"nonexistent-interface-12345"},
			wantHealthy:       false, // Missing expected interface
			wantConfigHealthy: true,
			wantIfaceResult:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setup(t)

			checker := NewCNIHealthChecker(CNIHealthConfig{
				ConfigPath:         configPath,
				CheckInterfaces:    tt.checkInterfaces,
				ExpectedInterfaces: tt.expectedIfaces,
			})

			result := checker.CheckHealth()

			if result.Healthy != tt.wantHealthy {
				t.Errorf("Healthy = %v, want %v. Issues: %v", result.Healthy, tt.wantHealthy, result.Issues)
			}
			if result.ConfigResult == nil {
				t.Fatal("ConfigResult is nil")
			}
			if result.ConfigResult.Healthy != tt.wantConfigHealthy {
				t.Errorf("ConfigResult.Healthy = %v, want %v", result.ConfigResult.Healthy, tt.wantConfigHealthy)
			}
			if tt.wantIfaceResult && result.IfaceResult == nil {
				t.Error("IfaceResult is nil but expected")
			}
			if !tt.wantIfaceResult && result.IfaceResult != nil {
				t.Error("IfaceResult is set but not expected")
			}
		})
	}
}

func TestCheckInterfaces(t *testing.T) {
	// This test relies on actual system interfaces, so we test behavior
	// rather than specific interface names

	t.Run("no expected interfaces", func(t *testing.T) {
		checker := NewCNIHealthChecker(CNIHealthConfig{
			ConfigPath:         t.TempDir(),
			CheckInterfaces:    true,
			ExpectedInterfaces: nil,
		})

		result := checker.CheckInterfaces()

		if !result.Healthy {
			t.Errorf("Should be healthy with no expected interfaces. Errors: %v", result.Errors)
		}
		if len(result.MissingInterfaces) != 0 {
			t.Errorf("MissingInterfaces should be empty, got %v", result.MissingInterfaces)
		}
	})

	t.Run("expected interface that does not exist", func(t *testing.T) {
		checker := NewCNIHealthChecker(CNIHealthConfig{
			ConfigPath:         t.TempDir(),
			CheckInterfaces:    true,
			ExpectedInterfaces: []string{"nonexistent-interface-xyz123"},
		})

		result := checker.CheckInterfaces()

		if result.Healthy {
			t.Error("Should be unhealthy when expected interface is missing")
		}
		if len(result.MissingInterfaces) != 1 {
			t.Errorf("MissingInterfaces should have 1 entry, got %d", len(result.MissingInterfaces))
		}
		if len(result.FoundInterfaces) != 0 {
			t.Errorf("FoundInterfaces should be empty, got %v", result.FoundInterfaces)
		}
	})

	t.Run("lo interface exists on all systems", func(t *testing.T) {
		checker := NewCNIHealthChecker(CNIHealthConfig{
			ConfigPath:         t.TempDir(),
			CheckInterfaces:    true,
			ExpectedInterfaces: []string{"lo"},
		})

		result := checker.CheckInterfaces()

		if !result.Healthy {
			t.Errorf("Should be healthy when 'lo' interface exists. Errors: %v", result.Errors)
		}
		if len(result.FoundInterfaces) != 1 || result.FoundInterfaces[0] != "lo" {
			t.Errorf("Expected 'lo' in FoundInterfaces, got %v", result.FoundInterfaces)
		}
	})

	t.Run("mixed found and missing interfaces", func(t *testing.T) {
		checker := NewCNIHealthChecker(CNIHealthConfig{
			ConfigPath:         t.TempDir(),
			CheckInterfaces:    true,
			ExpectedInterfaces: []string{"lo", "nonexistent-xyz123"},
		})

		result := checker.CheckInterfaces()

		if result.Healthy {
			t.Error("Should be unhealthy when some expected interfaces are missing")
		}
		if len(result.FoundInterfaces) != 1 {
			t.Errorf("FoundInterfaces should have 1 entry (lo), got %v", result.FoundInterfaces)
		}
		if len(result.MissingInterfaces) != 1 {
			t.Errorf("MissingInterfaces should have 1 entry, got %v", result.MissingInterfaces)
		}
	})
}

func TestGetLastResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
		t.Fatal(err)
	}

	checker := NewCNIHealthChecker(CNIHealthConfig{
		ConfigPath: dir,
	})

	// Before CheckHealth, GetLastResult should return nil
	impl := checker.(*cniHealthChecker)
	if impl.GetLastResult() != nil {
		t.Error("GetLastResult should return nil before CheckHealth is called")
	}

	// After CheckHealth, GetLastResult should return the result
	result := checker.CheckHealth()
	lastResult := impl.GetLastResult()

	if lastResult == nil {
		t.Fatal("GetLastResult should not return nil after CheckHealth")
	}
	if lastResult.Healthy != result.Healthy {
		t.Errorf("GetLastResult().Healthy = %v, want %v", lastResult.Healthy, result.Healthy)
	}
}

func TestAddCNIHealthToStatus(t *testing.T) {
	tests := []struct {
		name               string
		result             *CNIHealthResult
		wantConditionCount int
		wantEventCount     int
	}{
		{
			name:               "nil result",
			result:             nil,
			wantConditionCount: 0,
			wantEventCount:     0,
		},
		{
			name: "healthy config only",
			result: &CNIHealthResult{
				Healthy: true,
				ConfigResult: &CNIConfigResult{
					Healthy:       true,
					PrimaryConfig: "10-calico.conflist",
					CNIType:       "calico",
				},
			},
			wantConditionCount: 2, // CNIConfigValid + CNIHealthy
			wantEventCount:     0,
		},
		{
			name: "unhealthy config",
			result: &CNIHealthResult{
				Healthy: false,
				ConfigResult: &CNIConfigResult{
					Healthy: false,
					Errors:  []string{"No valid CNI configuration files found"},
				},
				Issues: []string{"No valid CNI configuration files found"},
			},
			wantConditionCount: 2, // CNIConfigValid + CNIHealthy
			wantEventCount:     1, // CNIConfigError
		},
		{
			name: "healthy config with healthy interfaces",
			result: &CNIHealthResult{
				Healthy: true,
				ConfigResult: &CNIConfigResult{
					Healthy:       true,
					PrimaryConfig: "10-calico.conflist",
					CNIType:       "calico",
				},
				IfaceResult: &CNIInterfaceResult{
					Healthy:            true,
					CNIInterfaces:      []CNIInterface{{Name: "cni0"}},
					ExpectedInterfaces: []string{"cni0"},
					FoundInterfaces:    []string{"cni0"},
				},
			},
			wantConditionCount: 3, // CNIConfigValid + CNIInterfacesHealthy + CNIHealthy
			wantEventCount:     0,
		},
		{
			name: "healthy config with unhealthy interfaces",
			result: &CNIHealthResult{
				Healthy: false,
				ConfigResult: &CNIConfigResult{
					Healthy:       true,
					PrimaryConfig: "10-calico.conflist",
					CNIType:       "calico",
				},
				IfaceResult: &CNIInterfaceResult{
					Healthy:            false,
					MissingInterfaces:  []string{"cni0"},
					ExpectedInterfaces: []string{"cni0"},
				},
				Issues: []string{"Missing expected CNI interfaces"},
			},
			wantConditionCount: 3, // CNIConfigValid + CNIInterfacesHealthy + CNIHealthy
			wantEventCount:     1, // CNIInterfaceWarning
		},
		{
			name: "unhealthy config and interfaces",
			result: &CNIHealthResult{
				Healthy: false,
				ConfigResult: &CNIConfigResult{
					Healthy: false,
					Errors:  []string{"No configs found"},
				},
				IfaceResult: &CNIInterfaceResult{
					Healthy:           false,
					MissingInterfaces: []string{"cni0"},
				},
				Issues: []string{"No configs found", "Missing interfaces"},
			},
			wantConditionCount: 3, // All three conditions
			wantEventCount:     2, // CNIConfigError + CNIInterfaceWarning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := types.NewStatus("test-cni-health")

			AddCNIHealthToStatus(status, tt.result)

			if len(status.Conditions) != tt.wantConditionCount {
				t.Errorf("Condition count = %d, want %d. Conditions: %v", len(status.Conditions), tt.wantConditionCount, status.Conditions)
			}
			if len(status.Events) != tt.wantEventCount {
				t.Errorf("Event count = %d, want %d. Events: %v", len(status.Events), tt.wantEventCount, status.Events)
			}
		})
	}
}

func TestAddCNIHealthToStatus_ConditionTypes(t *testing.T) {
	t.Run("healthy result has correct condition types", func(t *testing.T) {
		status := types.NewStatus("test-cni-health")
		result := &CNIHealthResult{
			Healthy: true,
			ConfigResult: &CNIConfigResult{
				Healthy:       true,
				PrimaryConfig: "10-calico.conflist",
				CNIType:       "calico",
			},
			IfaceResult: &CNIInterfaceResult{
				Healthy:       true,
				CNIInterfaces: []CNIInterface{{Name: "cni0"}},
			},
		}

		AddCNIHealthToStatus(status, result)

		// Check for expected condition types
		foundTypes := make(map[string]bool)
		for _, c := range status.Conditions {
			foundTypes[c.Type] = true
			if c.Status != types.ConditionTrue {
				t.Errorf("Condition %s should be True for healthy result, got %s", c.Type, c.Status)
			}
		}

		expectedTypes := []string{"CNIConfigValid", "CNIInterfacesHealthy", "CNIHealthy"}
		for _, et := range expectedTypes {
			if !foundTypes[et] {
				t.Errorf("Missing expected condition type: %s", et)
			}
		}
	})

	t.Run("unhealthy result has correct condition statuses", func(t *testing.T) {
		status := types.NewStatus("test-cni-health")
		result := &CNIHealthResult{
			Healthy: false,
			ConfigResult: &CNIConfigResult{
				Healthy: false,
				Errors:  []string{"error"},
			},
		}

		AddCNIHealthToStatus(status, result)

		for _, c := range status.Conditions {
			if c.Status != types.ConditionFalse {
				t.Errorf("Condition %s should be False for unhealthy result, got %s", c.Type, c.Status)
			}
		}
	})
}

func TestCNIInterfaceResult_NoExpectedInterfaces(t *testing.T) {
	// When no expected interfaces are specified but CheckInterfaces is enabled,
	// result should still be healthy
	checker := NewCNIHealthChecker(CNIHealthConfig{
		ConfigPath:         t.TempDir(),
		CheckInterfaces:    true,
		ExpectedInterfaces: []string{}, // Empty
	})

	result := checker.CheckInterfaces()

	if !result.Healthy {
		t.Error("Should be healthy with empty expected interfaces list")
	}
	// Should still find CNI interfaces if any exist on the system
	// (this is informational)
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "10-calico.conflist"), []byte(`{"type": "calico"}`), 0644); err != nil {
		t.Fatal(err)
	}

	checker := NewCNIHealthChecker(CNIHealthConfig{
		ConfigPath:      dir,
		CheckInterfaces: true,
	})

	impl := checker.(*cniHealthChecker)

	// Run multiple goroutines accessing GetLastResult and CheckHealth
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = checker.CheckHealth()
				_ = impl.GetLastResult()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	// If we get here without a race condition panic, the test passes
}
