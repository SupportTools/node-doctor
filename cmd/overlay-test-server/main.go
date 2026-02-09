// Package main implements a minimal HTTP health server for overlay-test pods.
// It serves /healthz and returns JSON with pod metadata, enabling HTTP-based
// connectivity probing that works on Cilium clusters where ICMP is silently dropped.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := flag.Int("port", 8023, "port to listen on")
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	podIP := os.Getenv("POD_IP")

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]string{
			"status": "ok",
			"node":   nodeName,
			"podIP":  podIP,
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck // best-effort response
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("overlay-test-server listening on %s (node=%s, podIP=%s)", addr, nodeName, podIP)
	if err := http.ListenAndServe(addr, nil); err != nil { //nolint:gosec // intentionally binds all interfaces for intra-cluster probing
		log.Fatalf("failed to start server: %v", err)
	}
}
