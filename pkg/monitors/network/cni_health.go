// Package network provides network health monitoring capabilities.
package network

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/supporttools/node-doctor/pkg/types"
)

const (
	// Default CNI configuration paths
	defaultCNIConfigPath = "/etc/cni/net.d"

	// Common CNI interface prefixes
	calicoInterfacePrefix  = "cali"
	flannelInterfacePrefix = "flannel"
	weaveInterfacePrefix   = "weave"
	cniPrefix              = "cni"
	vethPrefix             = "veth"
)

// CNIHealthChecker validates CNI configuration and network interfaces.
type CNIHealthChecker interface {
	// CheckHealth performs CNI health validation.
	CheckHealth() *CNIHealthResult
	// CheckConfigFiles validates CNI config files exist and are readable.
	CheckConfigFiles() *CNIConfigResult
	// CheckInterfaces validates expected network interfaces exist.
	CheckInterfaces() *CNIInterfaceResult
}

// CNIHealthResult holds the overall CNI health status.
type CNIHealthResult struct {
	Healthy      bool
	ConfigResult *CNIConfigResult
	IfaceResult  *CNIInterfaceResult
	Issues       []string
}

// CNIConfigResult holds CNI configuration file check results.
type CNIConfigResult struct {
	Healthy       bool
	ConfigPath    string
	ConfigFiles   []string
	ValidConfigs  int
	InvalidFiles  []string
	Errors        []string
	PrimaryConfig string
	CNIType       string
}

// CNIInterfaceResult holds network interface check results.
type CNIInterfaceResult struct {
	Healthy            bool
	ExpectedInterfaces []string
	FoundInterfaces    []string
	MissingInterfaces  []string
	CNIInterfaces      []CNIInterface
	Errors             []string
}

// CNIInterface represents a discovered CNI-related network interface.
type CNIInterface struct {
	Name    string
	Type    string // e.g., "calico", "flannel", "weave", "bridge", "veth"
	Up      bool
	MTU     int
	HasIPv4 bool
	HasIPv6 bool
	Addrs   []string
}

// cniHealthChecker is the default implementation of CNIHealthChecker.
type cniHealthChecker struct {
	configPath         string
	checkInterfaces    bool
	expectedInterfaces []string
	mu                 sync.RWMutex
	lastResult         *CNIHealthResult
}

// NewCNIHealthChecker creates a new CNI health checker.
func NewCNIHealthChecker(config CNIHealthConfig) CNIHealthChecker {
	configPath := config.ConfigPath
	if configPath == "" {
		configPath = defaultCNIConfigPath
	}

	return &cniHealthChecker{
		configPath:         configPath,
		checkInterfaces:    config.CheckInterfaces,
		expectedInterfaces: config.ExpectedInterfaces,
	}
}

// CheckHealth performs comprehensive CNI health validation.
func (c *cniHealthChecker) CheckHealth() *CNIHealthResult {
	result := &CNIHealthResult{
		Healthy: true,
		Issues:  make([]string, 0),
	}

	// Check configuration files
	result.ConfigResult = c.CheckConfigFiles()
	if !result.ConfigResult.Healthy {
		result.Healthy = false
		result.Issues = append(result.Issues, result.ConfigResult.Errors...)
	}

	// Check interfaces if enabled
	if c.checkInterfaces || len(c.expectedInterfaces) > 0 {
		result.IfaceResult = c.CheckInterfaces()
		if !result.IfaceResult.Healthy {
			result.Healthy = false
			result.Issues = append(result.Issues, result.IfaceResult.Errors...)
		}
	}

	c.mu.Lock()
	c.lastResult = result
	c.mu.Unlock()

	return result
}

// CheckConfigFiles validates CNI configuration files.
func (c *cniHealthChecker) CheckConfigFiles() *CNIConfigResult {
	result := &CNIConfigResult{
		Healthy:      false,
		ConfigPath:   c.configPath,
		ConfigFiles:  make([]string, 0),
		InvalidFiles: make([]string, 0),
		Errors:       make([]string, 0),
	}

	// Check if config directory exists
	info, err := os.Stat(c.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Sprintf("CNI config directory does not exist: %s", c.configPath))
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("Error accessing CNI config directory: %v", err))
		}
		return result
	}

	if !info.IsDir() {
		result.Errors = append(result.Errors, fmt.Sprintf("CNI config path is not a directory: %s", c.configPath))
		return result
	}

	// List config files
	entries, err := os.ReadDir(c.configPath)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Error reading CNI config directory: %v", err))
		return result
	}

	// Process config files (CNI uses lexicographic ordering, first valid config wins)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// CNI config files are .conf, .conflist, or .json
		if !isCNIConfigFile(name) {
			continue
		}

		fullPath := filepath.Join(c.configPath, name)
		result.ConfigFiles = append(result.ConfigFiles, name)

		// Validate the config file is readable and has content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			result.InvalidFiles = append(result.InvalidFiles, name)
			result.Errors = append(result.Errors, fmt.Sprintf("Cannot read CNI config file %s: %v", name, err))
			continue
		}

		if len(content) == 0 {
			result.InvalidFiles = append(result.InvalidFiles, name)
			result.Errors = append(result.Errors, fmt.Sprintf("CNI config file %s is empty", name))
			continue
		}

		// Simple validation - check if it looks like JSON
		trimmed := strings.TrimSpace(string(content))
		if !strings.HasPrefix(trimmed, "{") {
			result.InvalidFiles = append(result.InvalidFiles, name)
			result.Errors = append(result.Errors, fmt.Sprintf("CNI config file %s does not appear to be valid JSON", name))
			continue
		}

		result.ValidConfigs++

		// First valid config is the primary
		if result.PrimaryConfig == "" {
			result.PrimaryConfig = name
			result.CNIType = detectCNIType(name, string(content))
		}
	}

	// At least one valid config file is required
	if result.ValidConfigs > 0 {
		result.Healthy = true
	} else {
		result.Errors = append(result.Errors, "No valid CNI configuration files found")
	}

	return result
}

// CheckInterfaces validates CNI-related network interfaces.
func (c *cniHealthChecker) CheckInterfaces() *CNIInterfaceResult {
	result := &CNIInterfaceResult{
		Healthy:            true,
		ExpectedInterfaces: c.expectedInterfaces,
		FoundInterfaces:    make([]string, 0),
		MissingInterfaces:  make([]string, 0),
		CNIInterfaces:      make([]CNIInterface, 0),
		Errors:             make([]string, 0),
	}

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		result.Healthy = false
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to list network interfaces: %v", err))
		return result
	}

	// Build map of found interfaces for quick lookup
	foundMap := make(map[string]bool)

	for _, iface := range interfaces {
		foundMap[iface.Name] = true

		// Check if this is a CNI-related interface
		cniType := classifyCNIInterface(iface.Name)
		if cniType != "" {
			cniIface := CNIInterface{
				Name: iface.Name,
				Type: cniType,
				Up:   iface.Flags&net.FlagUp != 0,
				MTU:  iface.MTU,
			}

			// Get addresses
			addrs, err := iface.Addrs()
			if err == nil {
				for _, addr := range addrs {
					addrStr := addr.String()
					cniIface.Addrs = append(cniIface.Addrs, addrStr)
					if strings.Contains(addrStr, ".") {
						cniIface.HasIPv4 = true
					}
					if strings.Contains(addrStr, ":") {
						cniIface.HasIPv6 = true
					}
				}
			}

			result.CNIInterfaces = append(result.CNIInterfaces, cniIface)
		}
	}

	// Check expected interfaces
	for _, expected := range c.expectedInterfaces {
		if foundMap[expected] {
			result.FoundInterfaces = append(result.FoundInterfaces, expected)
		} else {
			result.MissingInterfaces = append(result.MissingInterfaces, expected)
		}
	}

	// If there are expected interfaces and some are missing, mark as unhealthy
	if len(c.expectedInterfaces) > 0 && len(result.MissingInterfaces) > 0 {
		result.Healthy = false
		result.Errors = append(result.Errors, fmt.Sprintf("Missing expected CNI interfaces: %v", result.MissingInterfaces))
	}

	// If we have expected interfaces but none found at all, that's an error
	if len(c.expectedInterfaces) > 0 && len(result.FoundInterfaces) == 0 {
		result.Healthy = false
		result.Errors = append(result.Errors, "None of the expected CNI interfaces were found")
	}

	return result
}

// isCNIConfigFile checks if a filename is a valid CNI config file.
func isCNIConfigFile(name string) bool {
	return strings.HasSuffix(name, ".conf") ||
		strings.HasSuffix(name, ".conflist") ||
		strings.HasSuffix(name, ".json")
}

// detectCNIType attempts to determine the CNI plugin type from config file.
func detectCNIType(filename, content string) string {
	// Check filename first
	lowerName := strings.ToLower(filename)
	switch {
	case strings.Contains(lowerName, "calico"):
		return "calico"
	case strings.Contains(lowerName, "flannel"):
		return "flannel"
	case strings.Contains(lowerName, "weave"):
		return "weave"
	case strings.Contains(lowerName, "cilium"):
		return "cilium"
	case strings.Contains(lowerName, "canal"):
		return "canal"
	case strings.Contains(lowerName, "antrea"):
		return "antrea"
	case strings.Contains(lowerName, "multus"):
		return "multus"
	}

	// Check content for common patterns
	lowerContent := strings.ToLower(content)
	switch {
	case strings.Contains(lowerContent, "\"type\": \"calico\"") ||
		strings.Contains(lowerContent, "\"type\":\"calico\""):
		return "calico"
	case strings.Contains(lowerContent, "\"type\": \"flannel\"") ||
		strings.Contains(lowerContent, "\"type\":\"flannel\""):
		return "flannel"
	case strings.Contains(lowerContent, "\"type\": \"weave-net\"") ||
		strings.Contains(lowerContent, "\"type\":\"weave-net\""):
		return "weave"
	case strings.Contains(lowerContent, "\"type\": \"cilium-cni\"") ||
		strings.Contains(lowerContent, "\"type\":\"cilium-cni\""):
		return "cilium"
	case strings.Contains(lowerContent, "\"type\": \"bridge\"") ||
		strings.Contains(lowerContent, "\"type\":\"bridge\""):
		return "bridge"
	}

	return "unknown"
}

// classifyCNIInterface determines what type of CNI interface this is.
func classifyCNIInterface(name string) string {
	switch {
	case strings.HasPrefix(name, calicoInterfacePrefix):
		return "calico"
	case strings.HasPrefix(name, flannelInterfacePrefix) || name == "flannel.1":
		return "flannel"
	case strings.HasPrefix(name, weaveInterfacePrefix):
		return "weave"
	case name == "cilium_host" || name == "cilium_net" || strings.HasPrefix(name, "lxc"):
		return "cilium"
	case strings.HasPrefix(name, cniPrefix):
		return "cni-generic"
	case strings.HasPrefix(name, vethPrefix):
		return "veth"
	case name == "docker0":
		return "docker"
	case name == "cbr0":
		return "kubenet"
	}
	return ""
}

// AddCNIHealthToStatus adds CNI health check results to a Status object.
func AddCNIHealthToStatus(status *types.Status, result *CNIHealthResult) {
	if result == nil {
		return
	}

	// Add CNI config condition
	if result.ConfigResult != nil {
		if result.ConfigResult.Healthy {
			status.AddCondition(types.NewCondition(
				"CNIConfigValid",
				types.ConditionTrue,
				"ConfigFilesPresent",
				fmt.Sprintf("CNI configuration is valid (primary: %s, type: %s)",
					result.ConfigResult.PrimaryConfig, result.ConfigResult.CNIType),
			))
		} else {
			status.AddCondition(types.NewCondition(
				"CNIConfigValid",
				types.ConditionFalse,
				"ConfigFilesInvalid",
				fmt.Sprintf("CNI configuration issues: %v", result.ConfigResult.Errors),
			))
			status.AddEvent(types.NewEvent(
				types.EventError,
				"CNIConfigError",
				fmt.Sprintf("CNI configuration validation failed: %v", result.ConfigResult.Errors),
			))
		}
	}

	// Add interface condition if interface checking was performed
	if result.IfaceResult != nil {
		if result.IfaceResult.Healthy {
			msg := fmt.Sprintf("Found %d CNI interfaces", len(result.IfaceResult.CNIInterfaces))
			if len(result.IfaceResult.ExpectedInterfaces) > 0 {
				msg = fmt.Sprintf("All expected interfaces present (%d/%d)",
					len(result.IfaceResult.FoundInterfaces),
					len(result.IfaceResult.ExpectedInterfaces))
			}
			status.AddCondition(types.NewCondition(
				"CNIInterfacesHealthy",
				types.ConditionTrue,
				"InterfacesPresent",
				msg,
			))
		} else {
			status.AddCondition(types.NewCondition(
				"CNIInterfacesHealthy",
				types.ConditionFalse,
				"InterfacesMissing",
				fmt.Sprintf("Missing CNI interfaces: %v", result.IfaceResult.MissingInterfaces),
			))
			status.AddEvent(types.NewEvent(
				types.EventWarning,
				"CNIInterfaceWarning",
				fmt.Sprintf("Expected CNI interfaces not found: %v", result.IfaceResult.MissingInterfaces),
			))
		}
	}

	// Add overall CNI health condition
	if result.Healthy {
		status.AddCondition(types.NewCondition(
			"CNIHealthy",
			types.ConditionTrue,
			"CNIOperational",
			"CNI plugin configuration and interfaces are healthy",
		))
	} else {
		status.AddCondition(types.NewCondition(
			"CNIHealthy",
			types.ConditionFalse,
			"CNIIssuesDetected",
			fmt.Sprintf("CNI health issues detected: %v", result.Issues),
		))
	}
}

// GetLastResult returns the last health check result.
func (c *cniHealthChecker) GetLastResult() *CNIHealthResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastResult
}
