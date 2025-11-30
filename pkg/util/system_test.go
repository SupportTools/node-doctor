// Package util provides utility functions for node-doctor.
package util

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"single digit", "5", true},
		{"multiple digits", "12345", true},
		{"with letter", "123a", false},
		{"with space", "123 ", false},
		{"with dash", "123-456", false},
		{"zero", "0", true},
		{"leading zeros", "00123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseKubernetesVersion(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		expectedMajor int
		expectedMinor int
		expectedPatch int
		expectedOK    bool
	}{
		{"standard version", "v1.28.0", 1, 28, 0, true},
		{"without v prefix", "1.28.0", 1, 28, 0, true},
		{"k3s version", "v1.28.1-k3s1", 1, 28, 1, true},
		{"rke2 version", "v1.28.2+rke2r1", 1, 28, 2, true},
		{"eks version", "v1.27.3-eks-a5565ad", 1, 27, 3, true},
		{"gke version", "v1.28.0-gke.1234", 1, 28, 0, true},
		{"major.minor only", "v1.28", 1, 28, 0, true},
		{"with plus suffix", "v1.28+", 1, 28, 0, true},
		{"invalid - single number", "v1", 0, 0, 0, false},
		{"invalid - letters", "abc", 0, 0, 0, false},
		{"invalid - empty", "", 0, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, ok := ParseKubernetesVersion(tt.version)
			if ok != tt.expectedOK {
				t.Errorf("ParseKubernetesVersion(%q) ok = %v, want %v", tt.version, ok, tt.expectedOK)
				return
			}
			if ok {
				if major != tt.expectedMajor || minor != tt.expectedMinor || patch != tt.expectedPatch {
					t.Errorf("ParseKubernetesVersion(%q) = (%d, %d, %d), want (%d, %d, %d)",
						tt.version, major, minor, patch, tt.expectedMajor, tt.expectedMinor, tt.expectedPatch)
				}
			}
		})
	}
}

func TestReadHostFile(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()

	// Create a test file
	testContent := []byte("test content\n")
	regularPath := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(regularPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a "host" mount path
	hostMountPath := filepath.Join(tempDir, "host")
	if err := os.MkdirAll(hostMountPath, 0755); err != nil {
		t.Fatalf("Failed to create host mount path: %v", err)
	}
	hostContent := []byte("host content\n")
	hostFilePath := filepath.Join(hostMountPath, "test.txt")
	if err := os.WriteFile(hostFilePath, hostContent, 0644); err != nil {
		t.Fatalf("Failed to create host file: %v", err)
	}

	tests := []struct {
		name          string
		path          string
		hostMountPath string
		expected      []byte
		expectError   bool
	}{
		{
			name:          "read from host mount path",
			path:          "/test.txt",
			hostMountPath: hostMountPath,
			expected:      hostContent,
			expectError:   false,
		},
		{
			name:          "fallback to regular path",
			path:          regularPath,
			hostMountPath: filepath.Join(tempDir, "nonexistent"),
			expected:      testContent,
			expectError:   false,
		},
		{
			name:          "empty host mount path",
			path:          regularPath,
			hostMountPath: "",
			expected:      testContent,
			expectError:   false,
		},
		{
			name:          "file not found",
			path:          "/nonexistent/file.txt",
			hostMountPath: "",
			expected:      nil,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := ReadHostFile(tt.path, tt.hostMountPath)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if string(data) != string(tt.expected) {
				t.Errorf("ReadHostFile() = %q, want %q", data, tt.expected)
			}
		})
	}
}

func TestGetOSRelease(t *testing.T) {
	// This test will only pass on systems with /etc/os-release
	result, err := GetOSRelease()
	if err != nil {
		// Skip if file doesn't exist (e.g., on macOS)
		if os.IsNotExist(err) {
			t.Skip("/etc/os-release not found, skipping")
		}
		t.Fatalf("GetOSRelease() error = %v", err)
	}

	// Verify some common keys exist
	if _, ok := result["ID"]; !ok {
		t.Error("Expected ID key in os-release")
	}
}

func TestCheckSystemdServiceStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with a non-existent service
	active, err := CheckSystemdServiceStatus(ctx, "nonexistent-service-12345")

	if !IsSystemctlAvailable() {
		// If systemctl isn't available, we should get ErrSystemctlNotAvailable
		if err != ErrSystemctlNotAvailable {
			t.Errorf("Expected ErrSystemctlNotAvailable when systemctl not available, got: %v", err)
		}
		return
	}

	// If systemctl is available, we should get a result (service not found or inactive)
	if err != nil && err != ErrServiceNotFound {
		t.Logf("CheckSystemdServiceStatus returned error (expected for nonexistent service): %v", err)
	}
	if active {
		t.Error("Expected nonexistent service to not be active")
	}
}

func TestCheckProcessRunning(t *testing.T) {
	// Test with a process that should always be running on Linux
	running, err := CheckProcessRunning("init")
	if err != nil {
		// This might fail on non-Linux systems
		t.Skipf("CheckProcessRunning error (expected on non-Linux): %v", err)
	}

	// init (PID 1) may have different names (systemd, init, etc.)
	// so we just verify the function works without error
	t.Logf("init running: %v", running)
}

func TestCheckProcessRunningByPattern(t *testing.T) {
	// Test with a pattern that should match something
	running, err := CheckProcessRunningByPattern("go")
	if err != nil {
		t.Skipf("CheckProcessRunningByPattern error: %v", err)
	}

	// The go test process itself should match
	t.Logf("'go' pattern found running: %v", running)
}

func TestGetServiceStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with systemd service and process fallback
	active, method, err := GetServiceStatus(ctx, "kubelet", []string{"kubelet", "hyperkube"})

	// The result depends on whether kubelet is running
	t.Logf("GetServiceStatus: active=%v, method=%s, err=%v", active, method, err)

	// Verify method is one of the expected values
	validMethods := map[string]bool{
		"systemd":         true,
		"process":         true,
		"process-pattern": true,
	}
	if !validMethods[method] {
		t.Errorf("Unexpected method: %s", method)
	}
}

func TestIsRunningInContainer(t *testing.T) {
	// This function should return a consistent result
	result1 := IsRunningInContainer()
	result2 := IsRunningInContainer()

	if result1 != result2 {
		t.Error("IsRunningInContainer returned inconsistent results")
	}

	t.Logf("IsRunningInContainer: %v", result1)
}

func TestIsSystemctlAvailable(t *testing.T) {
	// Just verify the function doesn't panic
	result := IsSystemctlAvailable()
	t.Logf("IsSystemctlAvailable: %v", result)
}

func TestGetContainerRuntime(t *testing.T) {
	runtime, socket := GetContainerRuntime()
	t.Logf("GetContainerRuntime: runtime=%s, socket=%s", runtime, socket)

	// If a runtime is detected, the socket should exist
	if runtime != "" {
		if _, err := os.Stat(socket); err != nil {
			t.Errorf("Detected runtime %s but socket %s not accessible: %v", runtime, socket, err)
		}
	}
}

func TestDetectKubernetesDistribution(t *testing.T) {
	distro := DetectKubernetesDistribution()
	t.Logf("DetectKubernetesDistribution: %s", distro)

	// Verify it returns a valid value
	validDistros := map[string]bool{
		"rke1":     true,
		"rke2":     true,
		"k3s":      true,
		"standard": true,
		"unknown":  true,
	}
	if !validDistros[distro] {
		t.Errorf("Unexpected distribution: %s", distro)
	}
}

func TestErrSystemctlNotAvailable(t *testing.T) {
	// Verify the error is not nil
	if ErrSystemctlNotAvailable == nil {
		t.Error("ErrSystemctlNotAvailable should not be nil")
	}
	if ErrSystemctlNotAvailable.Error() != "systemctl is not available" {
		t.Errorf("Unexpected error message: %s", ErrSystemctlNotAvailable.Error())
	}
}

func TestErrServiceNotFound(t *testing.T) {
	// Verify the error is not nil
	if ErrServiceNotFound == nil {
		t.Error("ErrServiceNotFound should not be nil")
	}
	if ErrServiceNotFound.Error() != "service not found" {
		t.Errorf("Unexpected error message: %s", ErrServiceNotFound.Error())
	}
}
