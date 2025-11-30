package custom

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// mockFileReader implements FileReader for testing
type mockFileReader struct {
	mu          sync.RWMutex
	files       map[string]string
	readErrors  map[string]error
	openedFiles map[string]*mockFile
}

// mockFile implements os.File interface for testing
type mockFile struct {
	content string
	pos     int
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if f.pos >= len(f.content) {
		return 0, fmt.Errorf("EOF")
	}
	n = copy(p, f.content[f.pos:])
	f.pos += n
	return n, nil
}

func (f *mockFile) Close() error {
	return nil
}

// newMockFileReader creates a new mock file reader
func newMockFileReader() *mockFileReader {
	return &mockFileReader{
		files:       make(map[string]string),
		readErrors:  make(map[string]error),
		openedFiles: make(map[string]*mockFile),
	}
}

// setFile sets the content for a file path
func (m *mockFileReader) setFile(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = content
}

// setReadError sets an error to return when reading a file
func (m *mockFileReader) setReadError(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readErrors[path] = err
}

// ReadFile implements FileReader.ReadFile
func (m *mockFileReader) ReadFile(path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, exists := m.readErrors[path]; exists {
		return nil, err
	}

	content, exists := m.files[path]
	if !exists {
		return nil, os.ErrNotExist
	}

	return []byte(content), nil
}

// Open implements FileReader.Open
func (m *mockFileReader) Open(path string) (*os.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, exists := m.readErrors[path]; exists {
		return nil, err
	}

	content, exists := m.files[path]
	if !exists {
		return nil, os.ErrNotExist
	}

	m.openedFiles[path] = &mockFile{content: content, pos: 0}
	// Note: returning nil here as we can't create real *os.File for testing
	// This is acceptable as the monitor doesn't use Open() in production code
	return nil, nil
}

// mockKmsgReader implements KmsgReader for testing
type mockKmsgReader struct {
	mu         sync.RWMutex
	content    map[string]string
	readErrors map[string]error
}

// newMockKmsgReader creates a new mock kmsg reader
func newMockKmsgReader() *mockKmsgReader {
	return &mockKmsgReader{
		content:    make(map[string]string),
		readErrors: make(map[string]error),
	}
}

// setContent sets the kmsg content for a path
func (m *mockKmsgReader) setContent(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.content[path] = content
}

// setReadError sets an error to return when reading from a path
func (m *mockKmsgReader) setReadError(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readErrors[path] = err
}

// ReadKmsg implements KmsgReader.ReadKmsg
func (m *mockKmsgReader) ReadKmsg(ctx context.Context, path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, exists := m.readErrors[path]; exists {
		return nil, err
	}

	content, exists := m.content[path]
	if !exists {
		return nil, os.ErrNotExist
	}

	return []byte(content), nil
}

// mockCommandExecutor implements CommandExecutor for testing
type mockCommandExecutor struct {
	outputs map[string]string // command key -> output
	errors  map[string]error  // command key -> error
	calls   []commandCall     // Track all calls for verification
}

// commandCall records a command execution
type commandCall struct {
	command string
	args    []string
}

// newMockCommandExecutor creates a new mock command executor
func newMockCommandExecutor() *mockCommandExecutor {
	return &mockCommandExecutor{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
		calls:   make([]commandCall, 0),
	}
}

// setOutput sets the output for a command
func (m *mockCommandExecutor) setOutput(command string, args []string, output string) {
	key := m.makeKey(command, args)
	m.outputs[key] = output
}

// setError sets an error to return for a command
func (m *mockCommandExecutor) setError(command string, args []string, err error) {
	key := m.makeKey(command, args)
	m.errors[key] = err
}

// ExecuteCommand implements CommandExecutor.ExecuteCommand
func (m *mockCommandExecutor) ExecuteCommand(ctx context.Context, command string, args []string) ([]byte, error) {
	// Record the call
	m.calls = append(m.calls, commandCall{
		command: command,
		args:    args,
	})

	key := m.makeKey(command, args)

	// Check for error first
	if err, exists := m.errors[key]; exists {
		return nil, err
	}

	// Return output if exists
	if output, exists := m.outputs[key]; exists {
		return []byte(output), nil
	}

	// Default: return empty output
	return []byte{}, nil
}

// getCalls returns all recorded command calls
func (m *mockCommandExecutor) getCalls() []commandCall {
	return m.calls
}

// wasCalled checks if a command was called with specific args
func (m *mockCommandExecutor) wasCalled(command string, args []string) bool {
	key := m.makeKey(command, args)
	for _, call := range m.calls {
		if m.makeKey(call.command, call.args) == key {
			return true
		}
	}
	return false
}

// makeKey creates a unique key for a command and its args
func (m *mockCommandExecutor) makeKey(command string, args []string) string {
	return command + " " + strings.Join(args, " ")
}

// Helper function to create test kmsg content
func createTestKmsgContent(entries ...string) string {
	return strings.Join(entries, "\n")
}

// Helper function to create test journal content
func createTestJournalContent(entries ...string) string {
	return strings.Join(entries, "\n")
}

// Common test log entries
const (
	testOOMKillLog      = "<4>[12345.678901] Out of memory: Kill process 1234 (test-process) score 1000"
	testDiskErrorLog    = "<3>[12346.789012] EXT4-fs (sda1): error count: 5"
	testNetworkErrorLog = "<4>[12347.890123] NETDEV WATCHDOG: eth0 (e1000e): transmit queue 0 timed out"
	testKernelPanicLog  = "<0>[12348.901234] Kernel panic - not syncing: VFS: Unable to mount root fs"
	testNormalLog       = "<6>[12349.012345] systemd[1]: Started User Manager for UID 1000."
	testKubeletErrorLog = "kubelet: Failed to initialize CSI driver"
	testContainerdLog   = "containerd: error creating container"
	testDockerErrorLog  = "dockerd: Failed to start docker engine"
)
