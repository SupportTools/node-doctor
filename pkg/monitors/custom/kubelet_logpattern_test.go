package custom

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// Test data for kubelet log patterns
var (
	// OOM test cases
	kubeletOOMPositive = []string{
		"kubelet[1234]: OOM killer activated for container",
		"kubelet: container out of memory condition detected",
		"kubelet[5678]: memory cgroup out of memory: container xyz",
		"systemd[1]: kubelet.service: Process OOM killed",
	}
	kubeletOOMNegative = []string{
		"containerd: OOM condition detected", // Not kubelet
		"normal log entry without OOM",
		"kubelet: memory usage normal",
		"kubelet: restart successful",
	}

	// PLEG unhealthy test cases
	kubeletPLEGPositive = []string{
		"PLEG is not healthy: pleg has yet to be successful",
		"kubelet[1234]: PLEG is not healthy after 3m",
		"relist operation taking too long: 5.2s",
		"PLEG: relist taking too long since startup",
	}
	kubeletPLEGNegative = []string{
		"PLEG operation completed successfully",
		"normal relist completed in 100ms",
		"kubelet: PLEG started successfully",
		"container list updated normally",
	}

	// Pod eviction test cases
	kubeletEvictionPositive = []string{
		"eviction manager: evicting pod default/nginx-abc123",
		"Evicting pod default/test-pod due to disk pressure",
		"kubelet[1234]: eviction manager evicting low priority pods",
		"Evicting pod: memory pressure threshold exceeded",
	}
	kubeletEvictionNegative = []string{
		"pod scheduling normal",
		"kubelet: pod creation successful",
		"container startup completed",
		"normal pod lifecycle events",
	}

	// Disk pressure test cases
	kubeletDiskPressurePositive = []string{
		"kubelet: disk pressure detected on node",
		"DiskPressure condition set to true",
		"node is experiencing out of disk space",
		"eviction manager: disk threshold exceeded",
	}
	kubeletDiskPressureNegative = []string{
		"disk usage normal",
		"kubelet: disk space available",
		"storage operations normal",
		"filesystem health check passed",
	}

	// Memory pressure test cases
	kubeletMemoryPressurePositive = []string{
		"kubelet: memory pressure detected",
		"MemoryPressure condition triggered",
		"eviction manager: memory threshold exceeded",
		"node experiencing memory pressure",
	}
	kubeletMemoryPressureNegative = []string{
		"memory usage normal",
		"kubelet: memory available",
		"memory allocation successful",
		"garbage collection completed",
	}

	// Image pull failure test cases
	kubeletImagePullPositive = []string{
		"Failed to pull image nginx:latest: timeout",
		"ErrImagePull: unable to pull image",
		"ImagePullBackOff: pull access denied",
		"kubelet: pull access denied for registry.io/image",
	}
	kubeletImagePullNegative = []string{
		"image pull completed successfully",
		"kubelet: image available locally",
		"container image updated",
		"registry connection established",
	}

	// CNI error test cases
	kubeletCNIPositive = []string{
		"CNI plugin error: failed to setup network",
		"network plugin returned error: timeout",
		"failed to set up sandbox: network unavailable",
		"kubelet: CNI error during pod creation",
	}
	kubeletCNINegative = []string{
		"CNI plugin loaded successfully",
		"network configuration applied",
		"sandbox setup completed",
		"pod networking established",
	}

	// Runtime error test cases
	kubeletRuntimePositive = []string{
		"container runtime error: failed to start",
		"runtime connection error: timeout",
		"failed to create container: insufficient resources",
		"Failed to start container xyz123",
	}
	kubeletRuntimeNegative = []string{
		"container runtime connected",
		"container creation successful",
		"runtime operation completed",
		"container started successfully",
	}

	// Certificate rotation test cases
	kubeletCertificatePositive = []string{
		"certificate rotation failed: expired certificate",
		"kubelet: certificate expired and rotation failed",
		"unable to rotate client certificate",
		"certificate renewal error: connection timeout",
	}
	kubeletCertificateNegative = []string{
		"certificate rotation successful",
		"certificate renewed automatically",
		"TLS connection established",
		"certificate validation passed",
	}

	// Node not ready test cases
	kubeletNodeNotReadyPositive = []string{
		"Node status set to NotReady",
		"kubelet: setting node status to NotReady",
		"node status update: NotReady due to runtime",
		"Node condition changed to NotReady",
	}
	kubeletNodeNotReadyNegative = []string{
		"Node status: Ready",
		"node health check passed",
		"kubelet: node ready for scheduling",
		"node registration successful",
	}

	// API connection error test cases
	kubeletAPIConnectionPositive = []string{
		"Unable to register node with API server",
		"failed to contact API server: connection refused",
		"connection refused: dial tcp apiserver:6443",
		"kubelet: API server unreachable",
	}
	kubeletAPIConnectionNegative = []string{
		"API server connection established",
		"node registration successful",
		"kubelet: heartbeat to API server OK",
		"API server authentication successful",
	}

	// Volume mount error test cases
	kubeletVolumeMountPositive = []string{
		"MountVolume.SetUp failed for volume pvc-123",
		"Unable to attach or mount volumes for pod",
		"failed to mount volume: permission denied",
		"volume mount timeout: device not ready",
	}
	kubeletVolumeMountNegative = []string{
		"volume mounted successfully",
		"MountVolume.SetUp completed",
		"volume attachment successful",
		"storage provisioning completed",
	}
)

// TestKubeletPatternCompilation tests that all kubelet patterns compile without errors
func TestKubeletPatternCompilation(t *testing.T) {
	kubeletPatterns := []LogPatternConfig{
		{Name: "kubelet-oom", Regex: "\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)"},
		{Name: "kubelet-pleg-unhealthy", Regex: "(?:PLEG is not healthy|relist.*taking too long)"},
		{Name: "kubelet-eviction", Regex: "(?:eviction manager.*evicting|Evicting pod)"},
		{Name: "kubelet-disk-pressure", Regex: "(?:disk pressure|DiskPressure|out of disk|eviction manager.*disk)"},
		{Name: "kubelet-memory-pressure", Regex: "(?:memory pressure|MemoryPressure|eviction manager.*memory)"},
		{Name: "kubelet-image-pull-failed", Regex: "(?:Failed to pull image|ErrImagePull|ImagePullBackOff|pull access denied)"},
		{Name: "kubelet-cni-error", Regex: "(?:CNI.*error|network plugin.*error|failed to set up sandbox)"},
		{Name: "kubelet-runtime-error", Regex: "(?:container runtime.*error|runtime.*error|failed to create container|Failed to start container)"},
		{Name: "kubelet-certificate-rotation", Regex: "(?:certificate.*(?:expired|rotation.*failed)|unable to rotate|certificate.*(?:renewal.*error|error))"},
		{Name: "kubelet-node-not-ready", Regex: "(?:Node.*NotReady|node status.*NotReady|setting node status.*NotReady)"},
		{Name: "kubelet-api-connection-error", Regex: "(?:Unable to register node|failed to contact API server|connection refused.*apiserver|API server.*(?:unreachable|error))"},
		{Name: "kubelet-volume-mount-error", Regex: "(?:MountVolume.SetUp failed|Unable to attach or mount|failed to mount volume|volume mount.*(?:timeout|error))"},
	}

	for _, pattern := range kubeletPatterns {
		t.Run(pattern.Name, func(t *testing.T) {
			// Test compilation
			compiled, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Fatalf("Failed to compile regex for %s: %v", pattern.Name, err)
			}

			// Basic sanity check - pattern should not be nil
			if compiled == nil {
				t.Fatalf("Compiled regex is nil for pattern %s", pattern.Name)
			}

			// Test that pattern doesn't match empty string (basic safety check)
			if compiled.MatchString("") {
				t.Errorf("Pattern %s matches empty string - may be too broad", pattern.Name)
			}

			// Test pattern length is reasonable
			if len(pattern.Regex) > 500 {
				t.Errorf("Pattern %s is very long (%d chars) - consider simplifying", pattern.Name, len(pattern.Regex))
			}
		})
	}
}

// TestKubeletPatternMatching tests positive and negative matches for each kubelet pattern
func TestKubeletPatternMatching(t *testing.T) {
	testCases := []struct {
		name            string
		pattern         string
		positiveMatches []string
		negativeMatches []string
	}{
		{
			name:            "kubelet-oom",
			pattern:         "\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)",
			positiveMatches: kubeletOOMPositive,
			negativeMatches: kubeletOOMNegative,
		},
		{
			name:            "kubelet-pleg-unhealthy",
			pattern:         "(?:PLEG is not healthy|relist.*taking too long)",
			positiveMatches: kubeletPLEGPositive,
			negativeMatches: kubeletPLEGNegative,
		},
		{
			name:            "kubelet-eviction",
			pattern:         "(?:eviction manager.*evicting|Evicting pod)",
			positiveMatches: kubeletEvictionPositive,
			negativeMatches: kubeletEvictionNegative,
		},
		{
			name:            "kubelet-disk-pressure",
			pattern:         "(?:disk pressure|DiskPressure|out of disk|eviction manager.*disk)",
			positiveMatches: kubeletDiskPressurePositive,
			negativeMatches: kubeletDiskPressureNegative,
		},
		{
			name:            "kubelet-memory-pressure",
			pattern:         "(?:memory pressure|MemoryPressure|eviction manager.*memory)",
			positiveMatches: kubeletMemoryPressurePositive,
			negativeMatches: kubeletMemoryPressureNegative,
		},
		{
			name:            "kubelet-image-pull-failed",
			pattern:         "(?:Failed to pull image|ErrImagePull|ImagePullBackOff|pull access denied)",
			positiveMatches: kubeletImagePullPositive,
			negativeMatches: kubeletImagePullNegative,
		},
		{
			name:            "kubelet-cni-error",
			pattern:         "(?:CNI.*error|network plugin.*error|failed to set up sandbox)",
			positiveMatches: kubeletCNIPositive,
			negativeMatches: kubeletCNINegative,
		},
		{
			name:            "kubelet-runtime-error",
			pattern:         "(?:container runtime.*error|runtime.*error|failed to create container|Failed to start container)",
			positiveMatches: kubeletRuntimePositive,
			negativeMatches: kubeletRuntimeNegative,
		},
		{
			name:            "kubelet-certificate-rotation",
			pattern:         "(?:certificate.*(?:expired|rotation.*failed)|unable to rotate|certificate.*(?:renewal.*error|error))",
			positiveMatches: kubeletCertificatePositive,
			negativeMatches: kubeletCertificateNegative,
		},
		{
			name:            "kubelet-node-not-ready",
			pattern:         "(?:Node.*NotReady|node status.*NotReady|setting node status.*NotReady)",
			positiveMatches: kubeletNodeNotReadyPositive,
			negativeMatches: kubeletNodeNotReadyNegative,
		},
		{
			name:            "kubelet-api-connection-error",
			pattern:         "(?:Unable to register node|failed to contact API server|connection refused.*apiserver|API server.*(?:unreachable|error))",
			positiveMatches: kubeletAPIConnectionPositive,
			negativeMatches: kubeletAPIConnectionNegative,
		},
		{
			name:            "kubelet-volume-mount-error",
			pattern:         "(?:MountVolume.SetUp failed|Unable to attach or mount|failed to mount volume|volume mount.*(?:timeout|error))",
			positiveMatches: kubeletVolumeMountPositive,
			negativeMatches: kubeletVolumeMountNegative,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := regexp.Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Failed to compile regex: %v", err)
			}

			// Test positive matches
			for i, logLine := range tc.positiveMatches {
				if !compiled.MatchString(logLine) {
					t.Errorf("Pattern %s should match positive case %d: %q", tc.name, i+1, logLine)
				}
			}

			// Test negative matches (should NOT match)
			for i, logLine := range tc.negativeMatches {
				if compiled.MatchString(logLine) {
					t.Errorf("Pattern %s should NOT match negative case %d: %q", tc.name, i+1, logLine)
				}
			}
		})
	}
}

// TestKubeletPatternSafety tests that patterns are safe from ReDoS attacks
func TestKubeletPatternSafety(t *testing.T) {
	kubeletPatterns := []string{
		"\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)",
		"(?:PLEG is not healthy|relist.*taking too long)",
		"(?:eviction manager.*evicting|Evicting pod)",
		"(?:disk pressure|DiskPressure|out of disk|eviction manager.*disk)",
		"(?:memory pressure|MemoryPressure|eviction manager.*memory)",
		"(?:Failed to pull image|ErrImagePull|ImagePullBackOff|pull access denied)",
		"(?:CNI.*error|network plugin.*error|failed to set up sandbox)",
		"(?:container runtime.*error|runtime.*error|failed to create container|Failed to start container)",
		"(?:certificate.*(?:expired|rotation.*failed)|unable to rotate|certificate.*(?:renewal.*error|error))",
		"(?:Node.*NotReady|node status.*NotReady|setting node status.*NotReady)",
		"(?:Unable to register node|failed to contact API server|connection refused.*apiserver|API server.*(?:unreachable|error))",
		"(?:MountVolume.SetUp failed|Unable to attach or mount|failed to mount volume|volume mount.*(?:timeout|error))",
	}

	for i, pattern := range kubeletPatterns {
		t.Run(pattern, func(t *testing.T) {
			// Test for nested quantifiers (ReDoS vulnerability)
			if strings.Contains(pattern, "+)+") || strings.Contains(pattern, "*)*") || strings.Contains(pattern, "+*") || strings.Contains(pattern, "*+") {
				t.Errorf("Pattern %d contains dangerous nested quantifiers: %s", i+1, pattern)
			}

			// Test for quantified adjacency (another ReDoS pattern)
			if strings.Contains(pattern, ".*.*") || strings.Contains(pattern, ".+.+") {
				t.Errorf("Pattern %d contains dangerous quantified adjacency: %s", i+1, pattern)
			}

			// Test compilation with timeout to detect catastrophic backtracking
			start := time.Now()
			compiled, err := regexp.Compile(pattern)
			compilationTime := time.Since(start)

			if err != nil {
				t.Errorf("Pattern %d failed to compile: %v", i+1, err)
				return
			}

			// Compilation should be fast
			if compilationTime > 100*time.Millisecond {
				t.Errorf("Pattern %d took too long to compile (%v): %s", i+1, compilationTime, pattern)
			}

			// Test with adversarial input to check for exponential backtracking
			adversarialInput := strings.Repeat("a", 100) + strings.Repeat("b", 100) + "!"
			start = time.Now()
			compiled.MatchString(adversarialInput)
			matchTime := time.Since(start)

			// Matching should be fast even with adversarial input
			if matchTime > 10*time.Millisecond {
				t.Errorf("Pattern %d shows potential ReDoS vulnerability (match time: %v): %s", i+1, matchTime, pattern)
			}
		})
	}
}

// TestKubeletPatternsInDefaultList tests that kubelet patterns are present in default patterns
func TestKubeletPatternsInDefaultList(t *testing.T) {
	expectedKubeletPatterns := []string{
		"kubelet-oom",
		"kubelet-pleg-unhealthy",
		"kubelet-eviction",
		"kubelet-disk-pressure",
		"kubelet-memory-pressure",
		"kubelet-image-pull-failed",
		"kubelet-cni-error",
		"kubelet-runtime-error",
		"kubelet-certificate-rotation",
		"kubelet-node-not-ready",
		"kubelet-api-connection-error",
		"kubelet-volume-mount-error",
	}

	defaults := GetDefaultPatterns()
	patternMap := make(map[string]LogPatternConfig)
	for _, pattern := range defaults {
		patternMap[pattern.Name] = pattern
	}

	for _, expectedPattern := range expectedKubeletPatterns {
		t.Run(expectedPattern, func(t *testing.T) {
			pattern, exists := patternMap[expectedPattern]
			if !exists {
				t.Errorf("Expected kubelet pattern %s not found in default patterns", expectedPattern)
				return
			}

			// Verify pattern has required fields
			if pattern.Regex == "" {
				t.Errorf("Pattern %s has empty regex", expectedPattern)
			}
			if pattern.Severity == "" {
				t.Errorf("Pattern %s has empty severity", expectedPattern)
			}
			if pattern.Description == "" {
				t.Errorf("Pattern %s has empty description", expectedPattern)
			}
			if pattern.Source != "journal" {
				t.Errorf("Pattern %s has incorrect source, expected 'journal', got '%s'", expectedPattern, pattern.Source)
			}
		})
	}
}

// TestKubeletPatternSeverityLevels tests that severity levels are appropriate
func TestKubeletPatternSeverityLevels(t *testing.T) {
	errorPatterns := []string{
		"kubelet-oom",
		"kubelet-pleg-unhealthy",
		"kubelet-cni-error",
		"kubelet-runtime-error",
		"kubelet-certificate-rotation",
		"kubelet-node-not-ready",
		"kubelet-api-connection-error",
		"kubelet-volume-mount-error",
	}

	warningPatterns := []string{
		"kubelet-eviction",
		"kubelet-disk-pressure",
		"kubelet-memory-pressure",
		"kubelet-image-pull-failed",
	}

	defaults := GetDefaultPatterns()
	patternMap := make(map[string]LogPatternConfig)
	for _, pattern := range defaults {
		patternMap[pattern.Name] = pattern
	}

	// Test error patterns
	for _, patternName := range errorPatterns {
		t.Run(patternName+"_severity", func(t *testing.T) {
			pattern, exists := patternMap[patternName]
			if !exists {
				t.Errorf("Pattern %s not found", patternName)
				return
			}
			if pattern.Severity != "error" {
				t.Errorf("Pattern %s should have 'error' severity, got '%s'", patternName, pattern.Severity)
			}
		})
	}

	// Test warning patterns
	for _, patternName := range warningPatterns {
		t.Run(patternName+"_severity", func(t *testing.T) {
			pattern, exists := patternMap[patternName]
			if !exists {
				t.Errorf("Pattern %s not found", patternName)
				return
			}
			if pattern.Severity != "warning" {
				t.Errorf("Pattern %s should have 'warning' severity, got '%s'", patternName, pattern.Severity)
			}
		})
	}
}

// TestKubeletPatternComplexity tests regex complexity to ensure patterns are efficient
func TestKubeletPatternComplexity(t *testing.T) {
	kubeletPatterns := []LogPatternConfig{
		{Name: "kubelet-oom", Regex: "\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)"},
		{Name: "kubelet-pleg-unhealthy", Regex: "(?:PLEG is not healthy|relist.*taking too long)"},
		{Name: "kubelet-eviction", Regex: "(?:eviction manager.*evicting|Evicting pod)"},
		{Name: "kubelet-disk-pressure", Regex: "(?:disk pressure|DiskPressure|out of disk|eviction manager.*disk)"},
		{Name: "kubelet-memory-pressure", Regex: "(?:memory pressure|MemoryPressure|eviction manager.*memory)"},
		{Name: "kubelet-image-pull-failed", Regex: "(?:Failed to pull image|ErrImagePull|ImagePullBackOff|pull access denied)"},
		{Name: "kubelet-cni-error", Regex: "(?:CNI.*error|network plugin.*error|failed to set up sandbox)"},
		{Name: "kubelet-runtime-error", Regex: "(?:container runtime.*error|runtime.*error|failed to create container|Failed to start container)"},
		{Name: "kubelet-certificate-rotation", Regex: "(?:certificate.*(?:expired|rotation.*failed)|unable to rotate|certificate.*(?:renewal.*error|error))"},
		{Name: "kubelet-node-not-ready", Regex: "(?:Node.*NotReady|node status.*NotReady|setting node status.*NotReady)"},
		{Name: "kubelet-api-connection-error", Regex: "(?:Unable to register node|failed to contact API server|connection refused.*apiserver|API server.*(?:unreachable|error))"},
		{Name: "kubelet-volume-mount-error", Regex: "(?:MountVolume.SetUp failed|Unable to attach or mount|failed to mount volume|volume mount.*(?:timeout|error))"},
	}

	for _, pattern := range kubeletPatterns {
		t.Run(pattern.Name+"_complexity", func(t *testing.T) {
			// Simple heuristic complexity scoring
			complexityScore := 0
			regex := pattern.Regex

			// Count various regex elements that increase complexity
			complexityScore += strings.Count(regex, ".*") * 3  // Greedy quantifiers
			complexityScore += strings.Count(regex, ".+") * 3  // Greedy quantifiers
			complexityScore += strings.Count(regex, "(?:") * 2 // Non-capturing groups
			complexityScore += strings.Count(regex, "(") * 1   // Capturing groups
			complexityScore += strings.Count(regex, "[") * 1   // Character classes
			complexityScore += strings.Count(regex, "\\b") * 1 // Word boundaries
			complexityScore += len(regex) / 10                 // Pattern length factor

			// Patterns should have reasonable complexity (under 50)
			if complexityScore > 50 {
				t.Errorf("Pattern %s has high complexity score %d (threshold: 50). Regex: %s",
					pattern.Name, complexityScore, regex)
			}
		})
	}
}

// TestKubeletPatternWordBoundaries tests that patterns use proper word boundaries where needed
func TestKubeletPatternWordBoundaries(t *testing.T) {
	testCases := []struct {
		name           string
		pattern        string
		shouldMatch    string
		shouldNotMatch string
	}{
		{
			name:           "kubelet-oom word boundary",
			pattern:        "\\bkubelet\\b.*(?:OOM|out of memory|memory cgroup out of memory)",
			shouldMatch:    "kubelet: OOM detected",
			shouldNotMatch: "mykubelet: OOM detected", // Should not match substring
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := regexp.Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Failed to compile regex: %v", err)
			}

			// Test positive match
			if !compiled.MatchString(tc.shouldMatch) {
				t.Errorf("Pattern should match: %q", tc.shouldMatch)
			}

			// Test negative match (should not match due to word boundary)
			if compiled.MatchString(tc.shouldNotMatch) {
				t.Errorf("Pattern should NOT match (word boundary): %q", tc.shouldNotMatch)
			}
		})
	}
}

// TestBackwardCompatibility tests that the old kubelet-error pattern is removed
// and replaced with specific patterns
func TestBackwardCompatibility(t *testing.T) {
	defaults := GetDefaultPatterns()
	patternNames := make(map[string]bool)
	for _, pattern := range defaults {
		patternNames[pattern.Name] = true
	}

	// The old generic kubelet-error pattern should be removed
	if patternNames["kubelet-error"] {
		t.Error("Generic 'kubelet-error' pattern should be replaced with specific kubelet patterns")
	}

	// All new specific patterns should exist
	expectedNewPatterns := []string{
		"kubelet-oom",
		"kubelet-pleg-unhealthy",
		"kubelet-eviction",
		"kubelet-disk-pressure",
		"kubelet-memory-pressure",
		"kubelet-image-pull-failed",
		"kubelet-cni-error",
		"kubelet-runtime-error",
		"kubelet-certificate-rotation",
		"kubelet-node-not-ready",
		"kubelet-api-connection-error",
		"kubelet-volume-mount-error",
	}

	missingPatterns := []string{}
	for _, pattern := range expectedNewPatterns {
		if !patternNames[pattern] {
			missingPatterns = append(missingPatterns, pattern)
		}
	}

	if len(missingPatterns) > 0 {
		t.Errorf("Missing expected kubelet patterns: %v", missingPatterns)
	}

	// Ensure we have exactly 12 new kubelet patterns
	kubeletPatternCount := 0
	for pattern := range patternNames {
		if strings.HasPrefix(pattern, "kubelet-") {
			kubeletPatternCount++
		}
	}

	if kubeletPatternCount != 12 {
		t.Errorf("Expected exactly 12 kubelet patterns, found %d", kubeletPatternCount)
	}
}

// TestKubeletPatternCoverage tests that patterns cover realistic log scenarios
func TestKubeletPatternCoverage(t *testing.T) {
	// Realistic log entries that should be caught by kubelet patterns
	realisticLogs := []struct {
		logEntry        string
		expectedPattern string
	}{
		{
			logEntry:        "Nov 16 10:00:00 node1 kubelet[1234]: E1116 10:00:00.123456 1234 kubelet.go:123] PLEG is not healthy: pleg has yet to be successful",
			expectedPattern: "kubelet-pleg-unhealthy",
		},
		{
			logEntry:        "Nov 16 10:01:00 node1 kubelet[1234]: I1116 10:01:00.234567 1234 eviction_manager.go:456] eviction manager: evicting pod default/nginx-abc123 due to disk pressure",
			expectedPattern: "kubelet-eviction",
		},
		{
			logEntry:        "Nov 16 10:02:00 node1 kubelet[1234]: E1116 10:02:00.345678 1234 kuberuntime.go:789] Failed to pull image \"nginx:latest\": rpc error: code = Unknown desc = failed to pull and unpack image \"docker.io/library/nginx:latest\": failed to resolve reference \"docker.io/library/nginx:latest\": failed to do request: Head \"https://registry-1.docker.io/v2/library/nginx/manifests/latest\": dial tcp: lookup registry-1.docker.io: no such host",
			expectedPattern: "kubelet-image-pull-failed",
		},
		{
			logEntry:        "Nov 16 10:03:00 node1 kubelet[1234]: E1116 10:03:00.456789 1234 pod_workers.go:123] container runtime error: failed to create container",
			expectedPattern: "kubelet-runtime-error",
		},
		{
			logEntry:        "Nov 16 10:04:00 node1 kubelet[1234]: W1116 10:04:00.567890 1234 kubelet_node_status.go:456] Node status: NotReady",
			expectedPattern: "kubelet-node-not-ready",
		},
	}

	// Get all default patterns and compile them
	defaults := GetDefaultPatterns()
	compiledPatterns := make(map[string]*regexp.Regexp)
	for _, pattern := range defaults {
		if strings.HasPrefix(pattern.Name, "kubelet-") {
			compiled, err := regexp.Compile(pattern.Regex)
			if err != nil {
				t.Fatalf("Failed to compile pattern %s: %v", pattern.Name, err)
			}
			compiledPatterns[pattern.Name] = compiled
		}
	}

	for i, testCase := range realisticLogs {
		t.Run(testCase.expectedPattern, func(t *testing.T) {
			pattern, exists := compiledPatterns[testCase.expectedPattern]
			if !exists {
				t.Fatalf("Expected pattern %s not found", testCase.expectedPattern)
			}

			if !pattern.MatchString(testCase.logEntry) {
				t.Errorf("Log entry %d should match pattern %s:\nLog: %s\nPattern: %s",
					i+1, testCase.expectedPattern, testCase.logEntry, pattern.String())
			}
		})
	}
}
