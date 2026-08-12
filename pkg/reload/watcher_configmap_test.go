package reload

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfigMapVersion emulates the Kubernetes atomic-writer layout that backs
// a mounted ConfigMap, and the swap it performs on update:
//
//	<dir>/..<timestamp>/config.yaml   real data directory
//	<dir>/..data      -> ..<timestamp> symlink, replaced via rename(2)
//	<dir>/config.yaml -> ..data/config.yaml
//
// The agent watches <dir> (not the file) precisely because of this dance: the
// file the operator edits is a symlink whose target directory is swapped
// wholesale, so an inotify watch on the leaf path would never fire.
func writeConfigMapVersion(t *testing.T, dir, timestamp, content string) {
	t.Helper()

	dataDir := filepath.Join(dir, ".."+timestamp)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpLink := filepath.Join(dir, "..data_tmp")
	_ = os.Remove(tmpLink)
	if err := os.Symlink(".."+timestamp, tmpLink); err != nil {
		t.Fatal(err)
	}
	// kubelet swaps ..data atomically with rename(2); inotify reports this on the
	// parent directory as MOVED_TO, which fsnotify surfaces as a Create event.
	if err := os.Rename(tmpLink, filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "config.yaml")
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		if err := os.Symlink("..data/config.yaml", link); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWatcherFiresOnConfigMapAtomicSwap is the end-to-end guard that a real
// `kubectl patch configmap` reaches the running agent. Watching the config file
// directly (rather than its directory) silently breaks this — the file content
// changes but no event ever fires, which is indistinguishable from "hot reload
// is not wired at all" from an operator's seat.
func TestWatcherFiresOnConfigMapAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	writeConfigMapVersion(t, dir, "2026_08_12_00_00_00.111111", "version: 1\n")

	cfgPath := filepath.Join(dir, "config.yaml")
	w, err := NewConfigWatcher(cfgPath, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Let the watch settle before mutating.
	time.Sleep(100 * time.Millisecond)

	writeConfigMapVersion(t, dir, "2026_08_12_00_00_05.222222", "version: 2\n")
	// kubelet garbage-collects the previous data directory after the swap.
	_ = os.RemoveAll(filepath.Join(dir, "..2026_08_12_00_00_00.111111"))

	select {
	case <-ch:
		content, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("config unreadable after swap: %v", err)
		}
		if string(content) != "version: 2\n" {
			t.Errorf("watcher fired but the file still reads %q; the swap did not land", content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never fired after a ConfigMap atomic swap — a ConfigMap edit " +
			"would silently never reach the running agent")
	}
}

// TestWatcherFiresOnPlainFileRewrite is the control: a non-ConfigMap deployment
// (bare file on disk) must also be detected.
func TestWatcherFiresOnPlainFileRewrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewConfigWatcher(cfgPath, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(cfgPath, []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never fired after an in-place config rewrite")
	}
}
