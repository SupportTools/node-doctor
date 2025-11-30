// Package util provides utility functions for node-doctor.
package util

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrSystemctlNotAvailable indicates systemctl is not available in the environment.
var ErrSystemctlNotAvailable = errors.New("systemctl is not available")

// ErrServiceNotFound indicates the requested service was not found.
var ErrServiceNotFound = errors.New("service not found")

var (
	// Cache container detection result
	isContainerOnce   sync.Once
	isContainerResult bool
)

// IsRunningInContainer detects if the current process is running inside a container.
// It uses multiple detection methods for reliability.
func IsRunningInContainer() bool {
	isContainerOnce.Do(func() {
		isContainerResult = detectContainer()
	})
	return isContainerResult
}

// detectContainer performs the actual container detection.
func detectContainer() bool {
	// Method 1: Check for /.dockerenv (Docker-specific)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Method 2: Check for /run/.containerenv (Podman)
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}

	// Method 3: Check cgroup v1 for docker/containerd/kubepods
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "containerd") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "lxc") {
			return true
		}
	}

	// Method 4: Check cgroup v2 for container indicators
	if data, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		content := string(data)
		if strings.Contains(content, "/docker/") ||
			strings.Contains(content, "/containerd/") ||
			strings.Contains(content, "/kubepods/") {
			return true
		}
	}

	// Method 5: Check for container runtime environment variables
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	return false
}

// IsSystemctlAvailable checks if systemctl is available in PATH.
func IsSystemctlAvailable() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// CheckSystemdServiceStatus checks the status of a systemd service.
// Returns (active, error). If systemctl is not available, returns ErrSystemctlNotAvailable.
func CheckSystemdServiceStatus(ctx context.Context, serviceName string) (bool, error) {
	if !IsSystemctlAvailable() {
		return false, ErrSystemctlNotAvailable
	}

	cmd := exec.CommandContext(ctx, "systemctl", "is-active", serviceName)
	output, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(output))

	if err != nil {
		// Check for known states
		switch status {
		case "inactive":
			return false, nil // Service is stopped (not an error)
		case "failed":
			return false, nil // Service has failed (not an error in checking)
		case "activating", "deactivating", "reloading":
			return false, nil // Transitional state, treat as inactive
		case "unknown":
			return false, ErrServiceNotFound
		default:
			// For any other error (including exec errors), check if it's exec-related
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				// Exit code non-zero typically means inactive/failed
				return false, nil
			}
			return false, err
		}
	}

	return status == "active", nil
}

// CheckProcessRunning checks if a process with the given name is running.
// This provides an alternative to systemd checks when running in containers.
func CheckProcessRunning(processName string) (bool, error) {
	// Read /proc to find running processes
	procDir := "/proc"
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		// Only check numeric directories (PIDs)
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if !isNumeric(pid) {
			continue
		}

		// Read the process name from /proc/[pid]/comm
		commPath := filepath.Join(procDir, pid, "comm")
		data, err := os.ReadFile(commPath)
		if err != nil {
			continue // Process may have exited
		}

		name := strings.TrimSpace(string(data))
		if name == processName {
			return true, nil
		}

		// Also check cmdline for the process name (some processes have different comm names)
		cmdlinePath := filepath.Join(procDir, pid, "cmdline")
		cmdData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// cmdline is null-separated
		cmdline := string(cmdData)
		if strings.Contains(cmdline, processName) {
			return true, nil
		}
	}

	return false, nil
}

// CheckProcessRunningByPattern checks if any process matches the given pattern.
// This is useful for processes with dynamic names or arguments.
func CheckProcessRunningByPattern(pattern string) (bool, error) {
	procDir := "/proc"
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if !isNumeric(pid) {
			continue
		}

		// Read cmdline
		cmdlinePath := filepath.Join(procDir, pid, "cmdline")
		data, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// Replace null bytes with spaces for easier matching
		cmdline := strings.ReplaceAll(string(data), "\x00", " ")
		if strings.Contains(cmdline, pattern) {
			return true, nil
		}
	}

	return false, nil
}

// GetServiceStatus returns a unified service status using the best available method.
// It tries systemd first, then falls back to process detection.
// processNames is a list of process names to look for if systemd is unavailable.
func GetServiceStatus(ctx context.Context, serviceName string, processNames []string) (active bool, method string, err error) {
	// Try systemd first if available
	if IsSystemctlAvailable() {
		active, err := CheckSystemdServiceStatus(ctx, serviceName)
		if err == nil || !errors.Is(err, ErrSystemctlNotAvailable) {
			return active, "systemd", err
		}
	}

	// Fall back to process detection
	for _, procName := range processNames {
		running, err := CheckProcessRunning(procName)
		if err != nil {
			continue
		}
		if running {
			return true, "process", nil
		}
	}

	// Also check by pattern (useful for scripts or wrapped processes)
	for _, procName := range processNames {
		running, err := CheckProcessRunningByPattern(procName)
		if err != nil {
			continue
		}
		if running {
			return true, "process-pattern", nil
		}
	}

	return false, "process", nil
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ReadHostFile reads a file from the host filesystem when running in a container.
// If hostPath is mounted (e.g., at /host), it reads from there.
// Otherwise, it reads from the regular path.
func ReadHostFile(path string, hostMountPath string) ([]byte, error) {
	// Try host mount path first
	if hostMountPath != "" {
		hostFilePath := filepath.Join(hostMountPath, path)
		if data, err := os.ReadFile(hostFilePath); err == nil {
			return data, nil
		}
	}

	// Fall back to regular path
	return os.ReadFile(path)
}

// GetContainerRuntime returns information about detected container runtime.
// Returns the runtime name and socket path if detected.
func GetContainerRuntime() (runtime string, socket string) {
	// Check for common container runtime sockets
	runtimes := []struct {
		name   string
		socket string
	}{
		{"containerd", "/run/containerd/containerd.sock"},
		{"containerd", "/var/run/containerd/containerd.sock"},
		{"cri-dockerd", "/var/run/cri-dockerd.sock"},
		{"cri-dockerd", "/run/cri-dockerd.sock"},
		{"docker", "/var/run/docker.sock"},
		{"docker", "/run/docker.sock"},
		{"crio", "/var/run/crio/crio.sock"},
		{"crio", "/run/crio/crio.sock"},
		// K3s/RKE2 specific
		{"k3s-containerd", "/run/k3s/containerd/containerd.sock"},
	}

	for _, rt := range runtimes {
		if _, err := os.Stat(rt.socket); err == nil {
			return rt.name, rt.socket
		}
	}

	return "", ""
}

// ParseKubernetesVersion parses a Kubernetes version string.
func ParseKubernetesVersion(version string) (major, minor, patch int, ok bool) {
	// Remove 'v' prefix if present
	version = strings.TrimPrefix(version, "v")

	// Split by dots
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}

	// Parse major
	var n int
	if _, err := parseIntFromString(parts[0], &n); err != nil {
		return 0, 0, 0, false
	}
	major = n

	// Parse minor (may have suffix like "28+")
	minorStr := strings.TrimRight(parts[1], "+")
	if _, err := parseIntFromString(minorStr, &n); err != nil {
		return 0, 0, 0, false
	}
	minor = n

	// Parse patch if present
	if len(parts) >= 3 {
		// Extract numeric part (e.g., "1-k3s1" -> 1)
		patchStr := parts[2]
		for i, c := range patchStr {
			if c < '0' || c > '9' {
				patchStr = patchStr[:i]
				break
			}
		}
		if patchStr != "" {
			if _, err := parseIntFromString(patchStr, &n); err == nil {
				patch = n
			}
		}
	}

	return major, minor, patch, true
}

// parseIntFromString is a simple int parser.
func parseIntFromString(s string, result *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
	}
	*result = n
	return n, nil
}

// DetectKubernetesDistribution detects the Kubernetes distribution.
// Returns one of: "rke1", "rke2", "k3s", "standard", "unknown"
func DetectKubernetesDistribution() string {
	// Check for K3s
	if _, err := os.Stat("/run/k3s/containerd/containerd.sock"); err == nil {
		// Could be K3s or RKE2 (RKE2 uses K3s's containerd)
		// Check for RKE2-specific files
		if _, err := os.Stat("/var/lib/rancher/rke2"); err == nil {
			return "rke2"
		}
		if _, err := os.Stat("/var/lib/rancher/k3s"); err == nil {
			return "k3s"
		}
	}

	// Check for RKE1 (uses Docker + cri-dockerd)
	if _, err := os.Stat("/var/run/cri-dockerd.sock"); err == nil {
		// RKE1 typically has Docker as the runtime with cri-dockerd adapter
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			// Check if kubelet is running in a container (RKE1 style)
			if running, _ := CheckProcessRunningByPattern("kubelet"); !running {
				// Kubelet not running as a process, might be in container
				return "rke1"
			}
		}
	}

	// Check for standard Kubernetes with systemd-managed kubelet
	if IsSystemctlAvailable() {
		if active, _ := CheckSystemdServiceStatus(context.Background(), "kubelet"); active {
			return "standard"
		}
	}

	// Check for kubelet process (could be any distribution)
	if running, _ := CheckProcessRunning("kubelet"); running {
		return "standard"
	}

	return "unknown"
}

// GetOSRelease reads and parses /etc/os-release.
func GetOSRelease() (map[string]string, error) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			value := strings.Trim(parts[1], "\"")
			result[key] = value
		}
	}

	return result, scanner.Err()
}
