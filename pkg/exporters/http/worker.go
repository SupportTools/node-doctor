package http

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// WorkerPool manages a pool of workers for asynchronous webhook processing
type WorkerPool struct {
	workers    int
	queueSize  int
	requestCh  chan *WorkerRequest
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	clients    map[string]*HTTPClient
	stats      *Stats
	nodeName   string
	mu         sync.RWMutex
	started    bool
}

// WorkerRequest represents a request to be processed by workers
type WorkerRequest struct {
	Type        string                // "status" or "problem"
	Status      *types.Status         // Status data (for status requests)
	Problem     *types.Problem        // Problem data (for problem requests)
	Endpoints   []types.WebhookEndpoint // Target endpoints
	RequestID   string                // Unique request ID for tracing
	SubmitTime  time.Time            // When the request was submitted
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers, queueSize int, nodeName string, stats *Stats) (*WorkerPool, error) {
	if workers <= 0 {
		return nil, fmt.Errorf("workers must be positive, got %d", workers)
	}
	if queueSize <= 0 {
		return nil, fmt.Errorf("queueSize must be positive, got %d", queueSize)
	}
	if nodeName == "" {
		return nil, fmt.Errorf("nodeName cannot be empty")
	}
	if stats == nil {
		return nil, fmt.Errorf("stats cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workers:   workers,
		queueSize: queueSize,
		requestCh: make(chan *WorkerRequest, queueSize),
		ctx:       ctx,
		cancel:    cancel,
		clients:   make(map[string]*HTTPClient),
		stats:     stats,
		nodeName:  nodeName,
	}, nil
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		return fmt.Errorf("worker pool already started")
	}

	log.Printf("[INFO] Starting HTTP worker pool with %d workers and queue size %d", wp.workers, wp.queueSize)

	// Start workers
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	wp.started = true
	return nil
}

// Stop stops the worker pool and waits for all workers to finish
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.started {
		return nil // Already stopped or never started
	}

	log.Printf("[INFO] Stopping HTTP worker pool...")

	// Step 1: Close request channel to prevent new requests - this allows graceful shutdown
	close(wp.requestCh)

	// Step 2: Wait for all workers to finish their current work with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[INFO] HTTP worker pool stopped successfully")
	case <-time.After(30 * time.Second):
		log.Printf("[WARN] HTTP worker pool stop timeout, forcing shutdown...")
		// Step 3: Only cancel context as last resort after timeout
		wp.cancel()
		// Give a bit more time for forced shutdown
		select {
		case <-done:
			log.Printf("[INFO] HTTP worker pool stopped after forced shutdown")
		case <-time.After(5 * time.Second):
			log.Printf("[ERROR] HTTP worker pool failed to stop even after forced shutdown")
		}
	}

	// Close all HTTP clients
	for name, client := range wp.clients {
		if err := client.Close(); err != nil {
			log.Printf("[WARN] Failed to close HTTP client for webhook %s: %v", name, err)
		}
	}

	wp.started = false
	return nil
}

// SubmitStatusRequest submits a status export request to the worker pool
func (wp *WorkerPool) SubmitStatusRequest(status *types.Status, endpoints []types.WebhookEndpoint, requestID string) error {
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	request := &WorkerRequest{
		Type:       "status",
		Status:     status,
		Endpoints:  endpoints,
		RequestID:  requestID,
		SubmitTime: time.Now(),
	}

	return wp.submitRequest(request)
}

// SubmitProblemRequest submits a problem export request to the worker pool
func (wp *WorkerPool) SubmitProblemRequest(problem *types.Problem, endpoints []types.WebhookEndpoint, requestID string) error {
	if problem == nil {
		return fmt.Errorf("problem cannot be nil")
	}

	request := &WorkerRequest{
		Type:       "problem",
		Problem:    problem,
		Endpoints:  endpoints,
		RequestID:  requestID,
		SubmitTime: time.Now(),
	}

	return wp.submitRequest(request)
}

// submitRequest submits a request to the worker pool
func (wp *WorkerPool) submitRequest(request *WorkerRequest) error {
	wp.mu.RLock()
	started := wp.started
	wp.mu.RUnlock()

	if !started {
		return fmt.Errorf("worker pool not started")
	}

	// Try to submit request to queue
	select {
	case wp.requestCh <- request:
		wp.stats.RecordQueuedRequest()
		return nil
	default:
		// Queue is full, drop request
		wp.stats.RecordDroppedRequest()
		return fmt.Errorf("worker pool queue is full, request dropped")
	}
}

// worker runs in a goroutine and processes requests
func (wp *WorkerPool) worker(workerID int) {
	defer wp.wg.Done()

	log.Printf("[DEBUG] HTTP worker %d started", workerID)

	for {
		select {
		case request, ok := <-wp.requestCh:
			if !ok {
				// Channel closed - graceful shutdown
				log.Printf("[DEBUG] HTTP worker %d stopping due to channel closure", workerID)
				return
			}

			wp.processRequest(workerID, request)

		case <-wp.ctx.Done():
			// Context canceled - forced shutdown (only after timeout)
			log.Printf("[DEBUG] HTTP worker %d stopping due to context cancellation", workerID)
			return
		}
	}
}

// processRequest processes a single request
func (wp *WorkerPool) processRequest(workerID int, request *WorkerRequest) {
	startTime := time.Now()
	queueTime := startTime.Sub(request.SubmitTime)

	log.Printf("[DEBUG] Worker %d processing %s request %s (queued for %v)",
		workerID, request.Type, request.RequestID, queueTime)

	// Filter endpoints based on what they want to receive
	var targetEndpoints []types.WebhookEndpoint
	for _, endpoint := range request.Endpoints {
		shouldSend := false
		switch request.Type {
		case "status":
			shouldSend = endpoint.SendStatus
		case "problem":
			shouldSend = endpoint.SendProblems
		}

		if shouldSend {
			targetEndpoints = append(targetEndpoints, endpoint)
		}
	}

	if len(targetEndpoints) == 0 {
		log.Printf("[DEBUG] Worker %d: no endpoints configured to receive %s requests", workerID, request.Type)
		return
	}

	// Process each endpoint
	for _, endpoint := range targetEndpoints {
		wp.processEndpoint(workerID, request, endpoint)
	}

	processingTime := time.Since(startTime)
	log.Printf("[DEBUG] Worker %d completed %s request %s in %v",
		workerID, request.Type, request.RequestID, processingTime)
}

// processEndpoint processes a request for a specific endpoint
func (wp *WorkerPool) processEndpoint(workerID int, request *WorkerRequest, endpoint types.WebhookEndpoint) {
	startTime := time.Now()

	// Get or create HTTP client for this endpoint
	client, err := wp.getHTTPClient(endpoint)
	if err != nil {
		log.Printf("[ERROR] Worker %d: failed to create HTTP client for webhook %s: %v",
			workerID, endpoint.Name, err)
		wp.recordFailure(request.Type, endpoint.Name, time.Since(startTime), err)
		return
	}

	// Create webhook request
	metadata := RequestMetadata{
		ExporterVersion: "1.0.0",
		RequestID:       request.RequestID,
		RetryAttempt:    0,
		WebhookName:     endpoint.Name,
	}

	var webhookRequest *WebhookRequest
	switch request.Type {
	case "status":
		webhookRequest = NewStatusRequest(request.Status, wp.nodeName, metadata)
	case "problem":
		webhookRequest = NewProblemRequest(request.Problem, wp.nodeName, metadata)
	default:
		log.Printf("[ERROR] Worker %d: unknown request type: %s", workerID, request.Type)
		return
	}

	// Send request with timeout - use original context, not the cancellation context
	// This allows in-progress requests to complete during graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), endpoint.Timeout)
	defer cancel()

	err = client.SendRequest(ctx, webhookRequest)
	responseTime := time.Since(startTime)

	if err != nil {
		log.Printf("[WARN] Worker %d: failed to send %s to webhook %s: %v",
			workerID, request.Type, endpoint.Name, err)
		wp.recordFailure(request.Type, endpoint.Name, responseTime, err)
	} else {
		log.Printf("[DEBUG] Worker %d: successfully sent %s to webhook %s in %v",
			workerID, request.Type, endpoint.Name, responseTime)
		wp.recordSuccess(request.Type, endpoint.Name, responseTime)
	}
}

// getHTTPClient gets or creates an HTTP client for an endpoint
func (wp *WorkerPool) getHTTPClient(endpoint types.WebhookEndpoint) (*HTTPClient, error) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Use endpoint name + URL as key to handle endpoint updates
	key := endpoint.Name + "|" + endpoint.URL

	client, exists := wp.clients[key]
	if !exists {
		var err error
		client, err = NewHTTPClient(endpoint)
		if err != nil {
			return nil, err
		}
		wp.clients[key] = client
	}

	return client, nil
}

// recordSuccess records a successful export
func (wp *WorkerPool) recordSuccess(requestType, webhookName string, responseTime time.Duration) {
	switch requestType {
	case "status":
		wp.stats.RecordStatusExport(true, webhookName, responseTime, nil)
	case "problem":
		wp.stats.RecordProblemExport(true, webhookName, responseTime, nil)
	}
}

// recordFailure records a failed export
func (wp *WorkerPool) recordFailure(requestType, webhookName string, responseTime time.Duration, err error) {
	switch requestType {
	case "status":
		wp.stats.RecordStatusExport(false, webhookName, responseTime, err)
	case "problem":
		wp.stats.RecordProblemExport(false, webhookName, responseTime, err)
	}
}

// GetQueueLength returns the current queue length
func (wp *WorkerPool) GetQueueLength() int {
	return len(wp.requestCh)
}

// GetWorkerCount returns the number of workers
func (wp *WorkerPool) GetWorkerCount() int {
	return wp.workers
}

// GetQueueCapacity returns the queue capacity
func (wp *WorkerPool) GetQueueCapacity() int {
	return wp.queueSize
}

// IsStarted returns true if the worker pool is started
func (wp *WorkerPool) IsStarted() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.started
}