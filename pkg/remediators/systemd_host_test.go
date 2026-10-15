package remediators

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/supporttools/node-doctor/pkg/types"
)

// enabledAwareExecutor answers `systemctl is-enabled` with a fixed state and records
// every command.
type enabledAwareExecutor struct {
	mu       sync.Mutex
	enabled  string // output of is-enabled
	enErr    error  // error of is-enabled
	commands []string
}

func (e *enabledAwareExecutor) ExecuteSystemctl(_ context.Context, args ...string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.commands = append(e.commands, strings.Join(args, " "))
	if args[0] == "is-enabled" {
		return e.enabled, e.enErr
	}
	return "", nil
}

func (e *enabledAwareExecutor) IsActive(context.Context, string) (bool, error) { return true, nil }

func (e *enabledAwareExecutor) Commands() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.commands...)
}

func restartProblem(requireEnabled bool) types.Problem {
	md := map[string]string{metadataKeyService: "rke2-agent"}
	if requireEnabled {
		md[metadataKeyRequireEnabled] = "true"
	}
	return types.Problem{Type: StrategySystemdRestart, Resource: "KubeletUnhealthy", Metadata: md}
}

func newRestartRemediator(t *testing.T, exec SystemdExecutor, dryRun bool) *SystemdRemediator {
	t.Helper()
	r, err := NewSystemdRemediator(SystemdConfig{Operation: SystemdRestart, DryRun: dryRun})
	if err != nil {
		t.Fatal(err)
	}
	r.SetSystemdExecutor(exec)
	return r
}

func TestSystemdRestart_RequireEnabled_RestartsEnabledUnit(t *testing.T) {
	exec := &enabledAwareExecutor{enabled: "enabled"}
	r := newRestartRemediator(t, exec, false)

	if err := r.Remediate(context.Background(), restartProblem(true)); err != nil {
		t.Fatalf("Remediate: %v", err)
	}
	want := []string{"is-enabled rke2-agent", "restart rke2-agent"}
	if got := exec.Commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}

// On an RKE2 server node rke2-agent.service is installed but disabled. Restarting it
// would start a second kubelet, so the remediator must refuse.
func TestSystemdRestart_RequireEnabled_RefusesDisabledUnit(t *testing.T) {
	for _, state := range []string{"disabled", "masked", "static", "not-found", ""} {
		t.Run(state, func(t *testing.T) {
			exec := &enabledAwareExecutor{enabled: state, enErr: errors.New("exit status 1")}
			r := newRestartRemediator(t, exec, false)

			err := r.Remediate(context.Background(), restartProblem(true))
			if err == nil {
				t.Fatal("Remediate succeeded on a unit that is not enabled")
			}
			if !strings.Contains(err.Error(), "refusing") {
				t.Errorf("error %q does not say it refused", err)
			}
			for _, c := range exec.Commands() {
				if strings.HasPrefix(c, "restart") {
					t.Fatalf("ran %q on a unit that is not enabled", c)
				}
			}
		})
	}
}

func TestSystemdRestart_RequireEnabled_CheckedInDryRun(t *testing.T) {
	exec := &enabledAwareExecutor{enabled: "disabled", enErr: errors.New("exit status 1")}
	r := newRestartRemediator(t, exec, true)
	if err := r.Remediate(context.Background(), restartProblem(true)); err == nil {
		t.Fatal("dry run reported success for a restart that would be refused")
	}
}

func TestSystemdRestart_WithoutRequireEnabled_Unchanged(t *testing.T) {
	exec := &enabledAwareExecutor{enabled: "disabled"}
	r := newRestartRemediator(t, exec, false)
	if err := r.Remediate(context.Background(), restartProblem(false)); err != nil {
		t.Fatalf("Remediate: %v", err)
	}
	if got := exec.Commands(); !reflect.DeepEqual(got, []string{"restart rke2-agent"}) {
		t.Fatalf("commands = %q, want only the restart", got)
	}
}

func TestIsChrootNoop(t *testing.T) {
	if !isChrootNoop("Running in chroot, ignoring command 'restart'") {
		t.Error("chroot notice not recognised")
	}
	if !isChrootNoop("Running in chroot, ignoring request.") {
		t.Error("chroot request notice not recognised")
	}
	if isChrootNoop("active") || isChrootNoop("") {
		t.Error("normal output treated as chroot notice")
	}
}

func TestNsenterSystemctlArgs(t *testing.T) {
	got := nsenterSystemctlArgs([]string{"restart", "rke2-agent"})
	want := []string{"--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid", "--",
		"systemctl", "restart", "rke2-agent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nsenter args = %q, want %q", got, want)
	}
}

// fakeProc builds a /proc look-alike: 1/comm, and the mnt namespace links of PID 1
// and of "self".
func fakeProc(t *testing.T, pid1Comm, pid1MntNS, selfMntNS string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"1/ns", "self/ns"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "1", "comm"), []byte(pid1Comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pid1MntNS, filepath.Join(root, "1", "ns", "mnt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(selfMntNS, filepath.Join(root, "self", "ns", "mnt")); err != nil {
		t.Fatal(err)
	}
	return root
}

func withProbeSeams(t *testing.T, root string, nsenterFound bool) {
	t.Helper()
	oldRoot, oldLook := procRoot, lookPath
	procRoot = root
	lookPath = func(string) (string, error) {
		if nsenterFound {
			return "/usr/bin/nsenter", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { procRoot, lookPath = oldRoot, oldLook })
}

func TestHostNamespaceProbe(t *testing.T) {
	tests := []struct {
		name         string
		comm         string
		pid1NS       string
		selfNS       string
		nsenterFound bool
		wantEnter    bool
	}{
		// The DaemonSet: hostPID, so PID 1 is the host's systemd, but our own
		// mount namespace is the container's.
		{"container with hostPID", "systemd", "mnt:[4026531841]", "mnt:[4026533001]", true, true},
		// Running directly on the host: same namespace, plain systemctl.
		{"on the host", "systemd", "mnt:[4026531841]", "mnt:[4026531841]", true, false},
		// Container without hostPID: PID 1 is node-doctor itself.
		{"container without hostPID", "node-doctor", "mnt:[1]", "mnt:[1]", true, false},
		// No nsenter in the image: plain systemctl, whose chroot no-op is then an error.
		{"no nsenter", "systemd", "mnt:[4026531841]", "mnt:[4026533001]", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProbeSeams(t, fakeProc(t, tt.comm, tt.pid1NS, tt.selfNS), tt.nsenterFound)
			got := hostNamespaceProbe()
			if got.enter != tt.wantEnter {
				t.Fatalf("enter = %v, want %v", got.enter, tt.wantEnter)
			}
		})
	}
}

func TestHostNamespaceProbe_UnreadableProc(t *testing.T) {
	withProbeSeams(t, filepath.Join(t.TempDir(), "missing"), true)
	if hostNamespaceProbe().enter {
		t.Fatal("entered host namespaces without being able to read /proc")
	}
}
