package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	prometheusexporter "github.com/supporttools/node-doctor/pkg/exporters/prometheus"
	"github.com/supporttools/node-doctor/pkg/types"
)

// TestHealthEndpointServesBeforeNetworkedExporters is the behavioural
// regression guard for the ordering fix in PR #24 (#node-doctor-246).
//
// The incident: on a degraded node a networked exporter's Start() BLOCKS
// (cluster-DNS or API-server reachability, informer cache sync). Before the
// fix, the health server was created AFTER those exporters, so the probe
// listener never opened inside the kubelet's startup-probe budget → the probe
// failed → the kubelet killed the container → crashloop, on exactly the nodes
// node-doctor exists to observe (a1pinode01 crashlooped 125x).
//
// This test pins the invariant directly: while phase 2 is blocked, the health
// endpoint must ALREADY be answering probes over the per-pod unix socket. If
// anyone reorders createExporters so networked init runs first, this test hangs
// on an unservable socket and fails.
func TestHealthEndpointServesBeforeNetworkedExporters(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "health.sock")

	// Phase 2 blocks until we release it — standing in for a wedged exporter
	// Start() on a degraded node.
	release := make(chan struct{})
	entered := make(chan struct{})

	original := startNetworkedExportersFn
	startNetworkedExportersFn = func(_ context.Context, _ *types.NodeDoctorConfig) ([]ExporterLifecycle, []types.Exporter, *prometheusexporter.PrometheusExporter) {
		close(entered)
		<-release
		return nil, nil, nil
	}
	t.Cleanup(func() { startNetworkedExportersFn = original })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := &types.NodeDoctorConfig{
		Exporters: types.ExporterConfigs{
			Kubernetes: &types.KubernetesExporterConfig{Enabled: true},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _, _, _ = createExporters(ctx, config, nil, socket)
	}()

	// Wait until phase 2 is definitely underway and stuck.
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("networked exporter phase never started")
	}

	// THE ASSERTION: the probe must already succeed even though the networked
	// phase is wedged. runHealthCheck is the exact code path the kubelet exec
	// probe runs, so this exercises the real production probe mechanism.
	if code := runHealthCheck(socket, "/healthz"); code != 0 {
		t.Errorf("liveness probe exit code = %d, want 0. The health server must bind BEFORE "+
			"networked exporter init; otherwise a blocked exporter on a degraded node prevents "+
			"the probe listener from ever opening and the kubelet crashloops the pod.", code)
	}

	// Readiness must also be reachable (it returns 503 until a monitor reports,
	// but the endpoint must be SERVING, not absent).
	if code := runHealthCheck(socket, "/ready"); code != 1 {
		t.Errorf("readiness probe exit code = %d, want 1 (endpoint serving, reporting NotReady "+
			"because no monitor has run yet)", code)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("createExporters did not return after phase 2 was released")
	}
}

// TestCreateExportersSourceOrdering is a static lint guard over
// cmd/node-doctor/main.go.
//
// The behavioural test above proves the property for the current structure; this
// one catches a subtler regression: someone inlining a networked exporter
// constructor back into createExporters ahead of the health server, which would
// reintroduce the crashloop while potentially still passing a test that stubs
// the phase-2 seam.
func TestCreateExportersSourceOrdering(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	fn := findFunc(file, "createExporters")
	if fn == nil {
		t.Fatal("createExporters not found in main.go")
	}

	// Constructors/starters that touch the network during startup and can block.
	networkedMarkers := []string{
		"NewKubernetesExporter",
		"NewHTTPExporter",
		"NewPrometheusExporter",
		"startNetworkedExporters",
	}

	var healthPos, firstNetworkedPos token.Pos
	var firstNetworkedName string

	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if name == "" {
			return true
		}

		if name == "startHealthServer" && healthPos == token.NoPos {
			healthPos = call.Pos()
		}
		for _, marker := range networkedMarkers {
			if strings.Contains(name, marker) && firstNetworkedPos == token.NoPos {
				firstNetworkedPos = call.Pos()
				firstNetworkedName = name
			}
		}
		return true
	})

	if healthPos == token.NoPos {
		t.Fatal("createExporters must call startHealthServer — the health listener has to be bound " +
			"before any networked initialization")
	}
	if firstNetworkedPos == token.NoPos {
		t.Fatal("expected createExporters to perform networked exporter initialization")
	}

	if firstNetworkedPos < healthPos {
		t.Errorf("networked init %q at %s runs BEFORE the health server is started at %s. "+
			"On a degraded node that exporter's Start() can block, the probe listener never opens "+
			"within the startup-probe budget, and the kubelet crashloops the pod (#node-doctor-246). "+
			"Move the health server creation back to the top of createExporters.",
			firstNetworkedName, fset.Position(firstNetworkedPos), fset.Position(healthPos))
	}
}

// TestStartHealthServerDoesNotTouchNetworkedExporters guards the other half of
// the invariant: phase 1 must stay free of any networked exporter construction,
// or "health first" becomes meaningless.
func TestStartHealthServerDoesNotTouchNetworkedExporters(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	fn := findFunc(file, "startHealthServer")
	if fn == nil {
		t.Fatal("startHealthServer not found in main.go")
	}

	forbidden := []string{"NewKubernetesExporter", "NewHTTPExporter", "NewPrometheusExporter"}

	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		for _, f := range forbidden {
			if strings.Contains(name, f) {
				t.Errorf("startHealthServer must not construct networked exporters, found %q at %s. "+
					"Phase 1 exists precisely to bind the probe listener before anything that can block.",
					name, fset.Position(call.Pos()))
			}
		}
		return true
	})
}

// findFunc locates a top-level function declaration by name.
func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// callName renders the called function's name, including a package or receiver
// qualifier when present (e.g. "health.NewServer", "healthServer.Start").
func callName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	}
	return ""
}
