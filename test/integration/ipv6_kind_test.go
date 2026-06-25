// Copyright 2025 Support Tools Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build integration
// +build integration

// Package integration contains top-level integration tests that exercise
// Node Doctor against real infrastructure. This file brings up a dual-stack
// (IPv4 + IPv6) kind cluster and asserts that the cluster is genuinely
// dual-stack, validating the project's IPv6/dual-stack code paths end-to-end.
//
// The test is gated behind the `integration` build tag and skips cleanly when
// the environment cannot run it (no kind binary, no Docker, or -short). It is
// intended to run in CI where Docker is available; local/dev sandboxes without
// Docker will skip rather than fail or hang.
//
// Run with:
//
//	go test -tags=integration ./test/integration/... -run IPv6Kind -v
package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// dualStackConfigPath is the kind config that enables dual-stack networking.
	dualStackConfigPath = "testdata/kind-dualstack.yaml"

	// clusterCreateTimeout bounds the (potentially slow) cluster bring-up.
	clusterCreateTimeout = 6 * time.Minute

	// nodeReadyTimeout bounds waiting for nodes to report Ready.
	nodeReadyTimeout = 3 * time.Minute

	// clusterDeleteTimeout bounds teardown.
	clusterDeleteTimeout = 2 * time.Minute
)

// TestIPv6KindDualStackCluster brings up a dual-stack kind cluster and asserts
// the cluster is genuinely dual-stack (both an IPv4 and an IPv6 pod CIDR on the
// node, plus IPv6 service IPs in kube-system). The dual-stack assertions are
// designed to FAIL on a single-stack cluster.
func TestIPv6KindDualStackCluster(t *testing.T) {
	// ---- Skip guards: never fail or hang on an unusable environment. ----

	// Guard 1: -short skips heavy infra tests.
	if testing.Short() {
		t.Skip("skipping dual-stack kind integration test in -short mode")
	}

	// Guard 2: kind binary must be installed.
	if _, err := exec.LookPath("kind"); err != nil {
		t.Skip("skipping: kind binary not found in PATH")
	}

	// Guard 3: Docker must be available AND running. `docker info` is a cheap
	// check that fails fast (non-zero exit) when the daemon is unreachable,
	// so we skip instead of letting `kind create cluster` hang/fail later.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("skipping: docker binary not found in PATH")
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput(); err != nil {
			t.Skipf("skipping: docker daemon not available (docker info failed: %v): %s",
				err, strings.TrimSpace(string(out)))
		}
	}

	// Config file must exist relative to this package directory.
	if _, err := os.Stat(dualStackConfigPath); err != nil {
		t.Skipf("skipping: dual-stack kind config not found at %s: %v", dualStackConfigPath, err)
	}

	// ---- Create the cluster. ----

	// Unique cluster name so parallel/CI runs don't collide.
	clusterName := fmt.Sprintf("nd-dualstack-%d", time.Now().UnixNano())

	// Always register cleanup BEFORE create returns, so a panic or failure
	// mid-create still tears the cluster down. kind delete is a no-op if the
	// cluster doesn't exist.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), clusterDeleteTimeout)
		defer cancel()
		deleteKindCluster(ctx, t, clusterName)
	})

	createCtx, cancelCreate := context.WithTimeout(context.Background(), clusterCreateTimeout)
	defer cancelCreate()

	t.Logf("creating dual-stack kind cluster %q from %s", clusterName, dualStackConfigPath)
	createArgs := []string{
		"create", "cluster",
		"--name", clusterName,
		"--config", dualStackConfigPath,
		"--wait", "120s",
	}
	cmd := exec.CommandContext(createCtx, "kind", createArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// A real failure to create a cluster in an environment that claimed to
		// have Docker is a genuine test failure.
		t.Fatalf("kind create cluster failed: %v", err)
	}

	// ---- Build a kube client from the cluster's kubeconfig. ----

	kubeconfigPath := writeKindKubeconfig(t, clusterName)
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		t.Fatalf("failed to build rest config from kubeconfig %s: %v", kubeconfigPath, err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		t.Fatalf("failed to create kubernetes clientset: %v", err)
	}

	// ---- Wait for node(s) Ready. ----

	waitForNodesReady(t, clientset, nodeReadyTimeout)

	// ---- Dual-stack assertions. ----

	assertNodesDualStack(t, clientset)
	assertDualStackServiceGetsIPv6(t, clientset)
}

// deleteKindCluster tears down the named kind cluster. Failures are logged, not
// fatal, since this runs in cleanup.
func deleteKindCluster(ctx context.Context, t *testing.T, name string) {
	t.Helper()
	t.Logf("deleting kind cluster %q", name)
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("warning: failed to delete kind cluster %q: %v", name, err)
	}
}

// writeKindKubeconfig exports the cluster's kubeconfig to a temp file and
// returns its path. Using an isolated kubeconfig avoids mutating the user's
// ~/.kube/config.
func writeKindKubeconfig(t *testing.T, clusterName string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "kind", "get", "kubeconfig", "--name", clusterName).Output()
	if err != nil {
		t.Fatalf("kind get kubeconfig failed for %q: %v", clusterName, err)
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("failed to write kubeconfig to %s: %v", path, err)
	}
	return path
}

// waitForNodesReady polls until every node reports Ready or the timeout elapses.
func waitForNodesReady(t *testing.T, clientset kubernetes.Interface, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		cancel()
		if err == nil && len(nodes.Items) > 0 && allNodesReady(nodes.Items) {
			t.Logf("all %d node(s) Ready", len(nodes.Items))
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("nodes did not become Ready within %s", timeout)
}

func allNodesReady(nodes []corev1.Node) bool {
	for _, n := range nodes {
		ready := false
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			return false
		}
	}
	return true
}

// assertNodesDualStack is the PRIMARY dual-stack assertion. On a single-stack
// cluster a node has exactly one pod CIDR (and only IPv4 internal addresses);
// these checks would fail there.
func assertNodesDualStack(t *testing.T, clientset kubernetes.Interface) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list nodes: %v", err)
	}
	if len(nodes.Items) == 0 {
		t.Fatal("no nodes found in cluster")
	}

	for _, node := range nodes.Items {
		// Collect pod CIDRs (PodCIDRs is the dual-stack-aware field; PodCIDR is
		// the legacy single value).
		cidrs := node.Spec.PodCIDRs
		if len(cidrs) == 0 && node.Spec.PodCIDR != "" {
			cidrs = []string{node.Spec.PodCIDR}
		}
		t.Logf("node %q PodCIDRs=%v", node.Name, cidrs)

		hasV4CIDR, hasV6CIDR := false, false
		for _, c := range cidrs {
			if isIPv6CIDR(c) {
				hasV6CIDR = true
			} else {
				hasV4CIDR = true
			}
		}
		if !hasV4CIDR || !hasV6CIDR {
			t.Errorf("node %q is not dual-stack: PodCIDRs=%v (want both IPv4 and IPv6)", node.Name, cidrs)
		}

		// Node addresses should also include both families.
		hasV4Addr, hasV6Addr := false, false
		for _, addr := range node.Status.Addresses {
			if addr.Type != corev1.NodeInternalIP {
				continue
			}
			if isIPv6(addr.Address) {
				hasV6Addr = true
			} else {
				hasV4Addr = true
			}
		}
		if !hasV4Addr || !hasV6Addr {
			t.Errorf("node %q internal addresses are not dual-stack: %v (want both IPv4 and IPv6)",
				node.Name, node.Status.Addresses)
		}
	}
}

// assertDualStackServiceGetsIPv6 creates a Service with
// ipFamilyPolicy=RequireDualStack and asserts the apiserver allocates BOTH an
// IPv4 and an IPv6 ClusterIP. On a single-stack cluster the apiserver rejects
// RequireDualStack outright, so this assertion is impossible to satisfy without
// a real dual-stack service CIDR range — making it a definitive dual-stack
// proof that complements the node-level checks.
func assertDualStackServiceGetsIPv6(t *testing.T, clientset kubernetes.Interface) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	policy := corev1.IPFamilyPolicyRequireDualStack
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nd-dualstack-probe",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:           corev1.ServiceTypeClusterIP,
			IPFamilyPolicy: &policy,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80},
			},
			Selector: map[string]string{"app": "nd-dualstack-probe"},
		},
	}

	created, err := clientset.CoreV1().Services("default").Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		// The apiserver rejects RequireDualStack on a single-stack cluster.
		t.Fatalf("failed to create RequireDualStack service (cluster not dual-stack?): %v", err)
	}
	t.Cleanup(func() {
		delCtx, delCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer delCancel()
		_ = clientset.CoreV1().Services("default").Delete(delCtx, created.Name, metav1.DeleteOptions{})
	})

	ips := created.Spec.ClusterIPs
	if len(ips) == 0 && created.Spec.ClusterIP != "" {
		ips = []string{created.Spec.ClusterIP}
	}
	t.Logf("RequireDualStack service got ClusterIPs=%v IPFamilies=%v", ips, created.Spec.IPFamilies)

	hasV4, hasV6 := false, false
	for _, ip := range ips {
		if isIPv6(ip) {
			hasV6 = true
		} else {
			hasV4 = true
		}
	}
	if !hasV4 || !hasV6 {
		t.Errorf("RequireDualStack service did not get both IP families: ClusterIPs=%v (want IPv4 and IPv6)", ips)
	}
}

// isIPv6CIDR reports whether a CIDR string is an IPv6 CIDR (heuristic: contains
// a colon). Pod/Service CIDRs are well-formed, so this is sufficient.
func isIPv6CIDR(cidr string) bool {
	return strings.Contains(cidr, ":")
}

// isIPv6 reports whether an IP string is IPv6 (heuristic: contains a colon).
func isIPv6(ip string) bool {
	return strings.Contains(ip, ":")
}
