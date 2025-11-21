package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestContext creates a test context with timeout.
func TestContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx, cancel
}

// AssertNoError fails the test if err is not nil.
func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err != nil {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%v: %v", fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...), err)
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

// AssertError fails the test if err is nil.
func AssertError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err == nil {
		if len(msgAndArgs) > 0 {
			t.Fatalf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		} else {
			t.Fatal("expected error but got nil")
		}
	}
}

// AssertEqual fails the test if expected != actual.
func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if expected != actual {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%v: expected %v, got %v", fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...), expected, actual)
		} else {
			t.Fatalf("expected %v, got %v", expected, actual)
		}
	}
}

// AssertTrue fails the test if condition is false.
func AssertTrue(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if !condition {
		if len(msgAndArgs) > 0 {
			t.Fatalf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		} else {
			t.Fatal("expected true, got false")
		}
	}
}

// AssertFalse fails the test if condition is true.
func AssertFalse(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if condition {
		if len(msgAndArgs) > 0 {
			t.Fatalf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		} else {
			t.Fatal("expected false, got true")
		}
	}
}

// MockKubernetesClient is a mock implementation of a Kubernetes client for testing.
type MockKubernetesClient struct {
	mu sync.RWMutex

	// Node responses
	nodeResponses map[string]interface{}
	nodeError     error

	// Request tracking
	nodeRequests []string
	listRequests int

	// Delay simulation
	responseDelay time.Duration
}

// NewMockKubernetesClient creates a new mock Kubernetes client.
func NewMockKubernetesClient() *MockKubernetesClient {
	return &MockKubernetesClient{
		nodeResponses: make(map[string]interface{}),
		nodeRequests:  make([]string, 0),
	}
}

// SetNodeResponse sets the response for a specific node name.
func (m *MockKubernetesClient) SetNodeResponse(nodeName string, response interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeResponses[nodeName] = response
}

// SetNodeError sets an error to be returned for node requests.
func (m *MockKubernetesClient) SetNodeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeError = err
}

// SetResponseDelay sets a delay for all responses.
func (m *MockKubernetesClient) SetResponseDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responseDelay = delay
}

// GetNode retrieves a node by name.
func (m *MockKubernetesClient) GetNode(ctx context.Context, nodeName string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nodeRequests = append(m.nodeRequests, nodeName)

	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}

	if m.nodeError != nil {
		return nil, m.nodeError
	}

	response, exists := m.nodeResponses[nodeName]
	if !exists {
		return nil, fmt.Errorf("node %q not found", nodeName)
	}

	return response, nil
}

// ListNodes returns all configured nodes.
func (m *MockKubernetesClient) ListNodes(ctx context.Context) ([]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listRequests++

	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}

	if m.nodeError != nil {
		return nil, m.nodeError
	}

	nodes := make([]interface{}, 0, len(m.nodeResponses))
	for _, node := range m.nodeResponses {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetNodeRequests returns the list of node names requested.
func (m *MockKubernetesClient) GetNodeRequests() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.nodeRequests...)
}

// GetListRequestCount returns the number of list requests made.
func (m *MockKubernetesClient) GetListRequestCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listRequests
}

// Reset clears all state from the mock client.
func (m *MockKubernetesClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeResponses = make(map[string]interface{})
	m.nodeError = nil
	m.nodeRequests = make([]string, 0)
	m.listRequests = 0
	m.responseDelay = 0
}

// MockHTTPServer provides utilities for creating mock HTTP servers in tests.
type MockHTTPServer struct {
	*httptest.Server
	mu sync.RWMutex

	// Request tracking
	requests      []*http.Request
	requestBodies []string

	// Response configuration
	statusCode    int
	responseBody  string
	responseDelay time.Duration
}

// NewMockHTTPServer creates a new mock HTTP server.
func NewMockHTTPServer() *MockHTTPServer {
	mock := &MockHTTPServer{
		requests:      make([]*http.Request, 0),
		requestBodies: make([]string, 0),
		statusCode:    http.StatusOK,
		responseBody:  "{}",
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.mu.Lock()
		defer mock.mu.Unlock()

		// Track request
		mock.requests = append(mock.requests, r)

		// Read and track body
		if r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				mock.requestBodies = append(mock.requestBodies, string(body))
			} else {
				mock.requestBodies = append(mock.requestBodies, "")
			}
			_ = r.Body.Close()
		} else {
			mock.requestBodies = append(mock.requestBodies, "")
		}

		// Simulate delay
		if mock.responseDelay > 0 {
			time.Sleep(mock.responseDelay)
		}

		// Write response
		w.WriteHeader(mock.statusCode)
		w.Write([]byte(mock.responseBody))
	}))

	return mock
}

// SetResponse sets the status code and response body.
func (m *MockHTTPServer) SetResponse(statusCode int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCode = statusCode
	m.responseBody = body
}

// SetResponseDelay sets a delay for all responses.
func (m *MockHTTPServer) SetResponseDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responseDelay = delay
}

// GetRequests returns all requests received by the server.
func (m *MockHTTPServer) GetRequests() []*http.Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	requests := make([]*http.Request, len(m.requests))
	copy(requests, m.requests)
	return requests
}

// GetRequestBodies returns all request bodies received.
func (m *MockHTTPServer) GetRequestBodies() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bodies := make([]string, len(m.requestBodies))
	copy(bodies, m.requestBodies)
	return bodies
}

// GetRequestCount returns the number of requests received.
func (m *MockHTTPServer) GetRequestCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.requests)
}

// Reset clears all tracked requests.
func (m *MockHTTPServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = make([]*http.Request, 0)
	m.requestBodies = make([]string, 0)
}

// MockCommandExecutor is a mock implementation for executing system commands.
type MockCommandExecutor struct {
	mu sync.RWMutex

	// Command configuration
	commands map[string]*CommandConfig

	// Default behavior
	defaultOutput string
	defaultError  error

	// Request tracking
	executedCommands []ExecutedCommand
}

// CommandConfig defines the behavior for a specific command.
type CommandConfig struct {
	Output string
	Error  error
	Delay  time.Duration
}

// ExecutedCommand tracks an executed command.
type ExecutedCommand struct {
	Name string
	Args []string
	Time time.Time
}

// NewMockCommandExecutor creates a new mock command executor.
func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{
		commands:         make(map[string]*CommandConfig),
		executedCommands: make([]ExecutedCommand, 0),
	}
}

// SetCommand configures the behavior for a specific command.
func (m *MockCommandExecutor) SetCommand(name string, output string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands[name] = &CommandConfig{
		Output: output,
		Error:  err,
	}
}

// SetCommandWithDelay configures a command with a delay.
func (m *MockCommandExecutor) SetCommandWithDelay(name string, output string, err error, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands[name] = &CommandConfig{
		Output: output,
		Error:  err,
		Delay:  delay,
	}
}

// SetDefaultBehavior sets the default output and error for unknown commands.
func (m *MockCommandExecutor) SetDefaultBehavior(output string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultOutput = output
	m.defaultError = err
}

// Execute simulates command execution.
func (m *MockCommandExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Track execution
	m.executedCommands = append(m.executedCommands, ExecutedCommand{
		Name: name,
		Args: args,
		Time: time.Now(),
	})

	// Check for command-specific configuration
	config, exists := m.commands[name]
	if !exists {
		// Use default behavior
		return m.defaultOutput, m.defaultError
	}

	// Simulate delay
	if config.Delay > 0 {
		time.Sleep(config.Delay)
	}

	return config.Output, config.Error
}

// GetExecutedCommands returns all executed commands.
func (m *MockCommandExecutor) GetExecutedCommands() []ExecutedCommand {
	m.mu.RLock()
	defer m.mu.RUnlock()
	commands := make([]ExecutedCommand, len(m.executedCommands))
	copy(commands, m.executedCommands)
	return commands
}

// GetExecutedCommandCount returns the number of executed commands.
func (m *MockCommandExecutor) GetExecutedCommandCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.executedCommands)
}

// WasCommandExecuted checks if a specific command was executed.
func (m *MockCommandExecutor) WasCommandExecuted(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, cmd := range m.executedCommands {
		if cmd.Name == name {
			return true
		}
	}
	return false
}

// Reset clears all state from the executor.
func (m *MockCommandExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = make(map[string]*CommandConfig)
	m.executedCommands = make([]ExecutedCommand, 0)
	m.defaultOutput = ""
	m.defaultError = nil
}

// LoadTestDataFile loads a file from the testdata directory.
func LoadTestDataFile(filename string) ([]byte, error) {
	testDataPath := filepath.Join("testdata", filename)
	return os.ReadFile(testDataPath)
}

// LoadTestDataJSON loads and unmarshals a JSON file from testdata.
func LoadTestDataJSON(filename string, v interface{}) error {
	data, err := LoadTestDataFile(filename)
	if err != nil {
		return fmt.Errorf("failed to load test data file: %w", err)
	}
	return json.Unmarshal(data, v)
}

// TempDir creates a temporary directory for tests.
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "node-doctor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// TempFile creates a temporary file for tests.
func TempFile(t *testing.T, content string) string {
	t.Helper()
	dir := TempDir(t)
	path := filepath.Join(dir, "testfile")
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	return path
}

// TempConfigFile creates a temporary config file with minimal valid content for tests.
func TempConfigFile(t *testing.T, name string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: %s
settings:
  nodeName: test-node
  logLevel: info
  logFormat: text
  logOutput: stdout
  updateInterval: 30s
  resyncInterval: 5m
  heartbeatInterval: 30s
  qps: 5
  burst: 10
remediation:
  enabled: false
  cooldownPeriod: 5m
  maxAttemptsGlobal: 3
  historySize: 100
`, name)
	dir := TempDir(t)
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}
	return path
}

// Eventually retries a condition function until it returns true or timeout.
func Eventually(t *testing.T, condition func() bool, timeout time.Duration, interval time.Duration, msgAndArgs ...interface{}) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	if len(msgAndArgs) > 0 {
		t.Fatalf(msgAndArgs[0].(string), msgAndArgs[1:]...)
	} else {
		t.Fatal("condition not met within timeout")
	}
}
