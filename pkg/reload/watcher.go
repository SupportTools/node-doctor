// Package reload provides configuration hot reload functionality.
package reload

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher watches a configuration file for changes and emits reload events.
type ConfigWatcher struct {
	configPath       string
	debounceInterval time.Duration
	watcher          *fsnotify.Watcher
	changeCh         chan struct{}
	mu               sync.Mutex
	running          bool
	stopCh           chan struct{}
}

// NewConfigWatcher creates a new configuration file watcher.
func NewConfigWatcher(configPath string, debounceInterval time.Duration) (*ConfigWatcher, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}

	if debounceInterval <= 0 {
		debounceInterval = 500 * time.Millisecond // Default debounce
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &ConfigWatcher{
		configPath:       configPath,
		debounceInterval: debounceInterval,
		watcher:          watcher,
		changeCh:         make(chan struct{}, 1), // Buffered to prevent blocking
		stopCh:           make(chan struct{}),
	}, nil
}

// Start begins watching the configuration file for changes.
// Returns a channel that receives events when the config file changes.
func (cw *ConfigWatcher) Start(ctx context.Context) (<-chan struct{}, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.running {
		return nil, fmt.Errorf("watcher already running")
	}

	// Watch the directory, not the file directly
	// This handles atomic writes (rename operations) used by Kubernetes ConfigMaps
	dir := filepath.Dir(cw.configPath)
	if err := cw.watcher.Add(dir); err != nil {
		return nil, fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	cw.running = true

	// Start event processing goroutine
	go cw.processEvents(ctx)

	return cw.changeCh, nil
}

// Stop stops watching the configuration file.
func (cw *ConfigWatcher) Stop() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.running {
		return
	}

	close(cw.stopCh)
	cw.watcher.Close()
	close(cw.changeCh)
	cw.running = false
}

// processEvents handles file system events with debouncing.
func (cw *ConfigWatcher) processEvents(ctx context.Context) {
	var debounceTimer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case <-cw.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			// Only process events for our config file
			if !cw.isConfigFileEvent(event) {
				continue
			}

			// Process WRITE and CREATE events (ConfigMap updates use atomic rename)
			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create {

				// Start or reset debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.NewTimer(cw.debounceInterval)
				timerCh = debounceTimer.C
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue watching
			// In production, this should use proper logging
			fmt.Printf("file watcher error: %v\n", err)

		case <-timerCh:
			// Debounce period elapsed, emit change event
			select {
			case cw.changeCh <- struct{}{}:
				// Event sent
			default:
				// Channel full, event already pending
			}
			timerCh = nil
		}
	}
}

// isConfigFileEvent checks if the event is for our configuration file.
func (cw *ConfigWatcher) isConfigFileEvent(event fsnotify.Event) bool {
	// Handle both direct file writes and symlink updates (ConfigMap pattern)
	eventPath := filepath.Clean(event.Name)
	configPath := filepath.Clean(cw.configPath)

	// Direct match
	if eventPath == configPath {
		return true
	}

	// Symlink resolution for ConfigMap atomic updates
	// ConfigMaps use ..data symlink pattern
	if filepath.Base(eventPath) == "..data" && filepath.Dir(eventPath) == filepath.Dir(configPath) {
		return true
	}

	return false
}
