package remediators

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/supporttools/node-doctor/pkg/controller"
	"github.com/supporttools/node-doctor/pkg/types"
)

// LeaseClient communicates with the node-doctor controller to coordinate
// remediations across the cluster using a lease-based model.
type LeaseClient struct {
	config     *types.RemediationCoordinationConfig
	nodeName   string
	httpClient *http.Client

	mu          sync.RWMutex
	activeLease *controller.Lease
	stats       LeaseClientStats
}

// LeaseClientStats tracks statistics for the lease client.
type LeaseClientStats struct {
	mu                sync.RWMutex
	LeaseRequests     int64
	LeasesGranted     int64
	LeasesDenied      int64
	LeaseReleases     int64
	ControllerErrors  int64
	LastRequestTime   time.Time
	LastSuccessTime   time.Time
	LastError         string
	LastErrorTime     time.Time
	FallbacksUsed     int64
}

// NewLeaseClient creates a new lease client for the given node.
func NewLeaseClient(
	config *types.RemediationCoordinationConfig,
	nodeName string,
) (*LeaseClient, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if !config.Enabled {
		return nil, fmt.Errorf("coordination is disabled")
	}
	if config.ControllerURL == "" {
		return nil, fmt.Errorf("controller URL is required")
	}
	if nodeName == "" {
		return nil, fmt.Errorf("node name is required")
	}

	httpClient := &http.Client{
		Timeout: config.RequestTimeout,
	}

	return &LeaseClient{
		config:     config,
		nodeName:   nodeName,
		httpClient: httpClient,
	}, nil
}

// RequestLease requests a remediation lease from the controller.
// Returns the lease response and any error.
// If the controller is unreachable, it uses the fallback behavior.
func (lc *LeaseClient) RequestLease(ctx context.Context, remediationType, reason string) (*controller.LeaseResponse, error) {
	lc.recordRequestAttempt()

	request := controller.LeaseRequest{
		NodeName:          lc.nodeName,
		RemediationType:   remediationType,
		RequestedDuration: lc.config.LeaseTimeout.String(),
		Reason:            reason,
	}

	var lastErr error
	for attempt := 1; attempt <= lc.config.MaxRetries; attempt++ {
		resp, err := lc.sendLeaseRequest(ctx, request)
		if err == nil {
			// Successfully got a response from controller
			lc.recordSuccess()
			if resp.Approved {
				lc.recordLeaseGranted()
				lc.setActiveLease(&controller.Lease{
					ID:              resp.LeaseID,
					NodeName:        lc.nodeName,
					RemediationType: remediationType,
					GrantedAt:       time.Now(),
					ExpiresAt:       resp.ExpiresAt,
					Status:          "active",
					Reason:          reason,
				})
			} else {
				lc.recordLeaseDenied()
			}
			return resp, nil
		}

		lastErr = err
		lc.recordError(err)
		log.Printf("[WARN] Lease request attempt %d/%d failed: %v", attempt, lc.config.MaxRetries, err)

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			break
		}

		// Don't retry on last attempt
		if attempt >= lc.config.MaxRetries {
			break
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lc.config.RetryInterval):
			// Continue to next attempt
		}
	}

	// Controller is unreachable
	lc.recordControllerError()

	// Check fallback behavior
	if lc.config.FallbackOnUnreachable {
		log.Printf("[WARN] Controller unreachable, using fallback behavior (proceeding with remediation)")
		lc.recordFallbackUsed()
		return &controller.LeaseResponse{
			Approved: true,
			Message:  "Fallback: controller unreachable, proceeding with remediation",
		}, nil
	}

	return nil, fmt.Errorf("controller unreachable after %d attempts: %w", lc.config.MaxRetries, lastErr)
}

// ReleaseLease releases an active remediation lease.
func (lc *LeaseClient) ReleaseLease(ctx context.Context, leaseID string) error {
	if leaseID == "" {
		return nil // Nothing to release
	}

	// Build URL
	url := fmt.Sprintf("%s/api/v1/leases/%s", lc.config.ControllerURL, leaseID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create release request: %w", err)
	}

	resp, err := lc.httpClient.Do(req)
	if err != nil {
		// Log but don't fail - the lease will expire anyway
		log.Printf("[WARN] Failed to release lease %s: %v", leaseID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		lc.recordLeaseRelease()
		lc.clearActiveLease()
		log.Printf("[DEBUG] Successfully released lease %s", leaseID)
		return nil
	}

	// Non-success status - log but don't fail
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	log.Printf("[WARN] Failed to release lease %s: status %d, body: %s", leaseID, resp.StatusCode, string(body))
	return nil
}

// ReleaseActiveLease releases the currently active lease if any.
func (lc *LeaseClient) ReleaseActiveLease(ctx context.Context) error {
	lc.mu.RLock()
	lease := lc.activeLease
	lc.mu.RUnlock()

	if lease == nil {
		return nil
	}

	return lc.ReleaseLease(ctx, lease.ID)
}

// HasActiveLease returns whether there's an active lease.
func (lc *LeaseClient) HasActiveLease() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	if lc.activeLease == nil {
		return false
	}

	// Check if lease has expired
	if time.Now().After(lc.activeLease.ExpiresAt) {
		return false
	}

	return true
}

// GetActiveLease returns the current active lease, if any.
func (lc *LeaseClient) GetActiveLease() *controller.Lease {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	if lc.activeLease == nil {
		return nil
	}

	// Return a copy
	lease := *lc.activeLease
	return &lease
}

// GetStats returns the current statistics.
func (lc *LeaseClient) GetStats() LeaseClientStats {
	lc.stats.mu.RLock()
	defer lc.stats.mu.RUnlock()

	return LeaseClientStats{
		LeaseRequests:    lc.stats.LeaseRequests,
		LeasesGranted:    lc.stats.LeasesGranted,
		LeasesDenied:     lc.stats.LeasesDenied,
		LeaseReleases:    lc.stats.LeaseReleases,
		ControllerErrors: lc.stats.ControllerErrors,
		LastRequestTime:  lc.stats.LastRequestTime,
		LastSuccessTime:  lc.stats.LastSuccessTime,
		LastError:        lc.stats.LastError,
		LastErrorTime:    lc.stats.LastErrorTime,
		FallbacksUsed:    lc.stats.FallbacksUsed,
	}
}

// IsHealthy returns whether the lease client is healthy.
func (lc *LeaseClient) IsHealthy() bool {
	lc.stats.mu.RLock()
	defer lc.stats.mu.RUnlock()

	// Healthy if we've had a recent success
	if lc.stats.LeaseRequests == 0 {
		return true // No requests yet, assume healthy
	}

	// If we have had requests but high error rate, unhealthy
	if lc.stats.LeaseRequests > 5 {
		errorRate := float64(lc.stats.ControllerErrors) / float64(lc.stats.LeaseRequests)
		if errorRate > 0.5 {
			return false
		}
	}

	return true
}

// sendLeaseRequest sends a lease request to the controller.
func (lc *LeaseClient) sendLeaseRequest(ctx context.Context, request controller.LeaseRequest) (*controller.LeaseResponse, error) {
	// Build URL
	url := fmt.Sprintf("%s/api/v1/leases", lc.config.ControllerURL)

	// Marshal request
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "node-doctor-lease-client")

	// Send request
	resp, err := lc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle response based on status code
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// Lease approved
		var leaseResp controller.LeaseResponse
		if err := json.Unmarshal(respBody, &leaseResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		return &leaseResp, nil

	case http.StatusTooManyRequests:
		// Lease denied due to concurrency limit
		var leaseResp controller.LeaseResponse
		if err := json.Unmarshal(respBody, &leaseResp); err != nil {
			// Create a default denied response
			leaseResp = controller.LeaseResponse{
				Approved: false,
				Message:  "Too many concurrent remediations",
			}
		}
		return &leaseResp, nil

	case http.StatusConflict:
		// Lease denied due to existing lease
		var leaseResp controller.LeaseResponse
		if err := json.Unmarshal(respBody, &leaseResp); err != nil {
			leaseResp = controller.LeaseResponse{
				Approved: false,
				Message:  "Node already has an active lease",
			}
		}
		return &leaseResp, nil

	case http.StatusBadRequest:
		return nil, fmt.Errorf("bad request: %s", string(respBody))

	case http.StatusInternalServerError:
		return nil, fmt.Errorf("controller error: %s", string(respBody))

	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
}

// setActiveLease sets the active lease.
func (lc *LeaseClient) setActiveLease(lease *controller.Lease) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.activeLease = lease
}

// clearActiveLease clears the active lease.
func (lc *LeaseClient) clearActiveLease() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.activeLease = nil
}

// recordRequestAttempt records that a lease request was attempted.
func (lc *LeaseClient) recordRequestAttempt() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LeaseRequests++
	lc.stats.LastRequestTime = time.Now()
}

// recordSuccess records a successful controller response.
func (lc *LeaseClient) recordSuccess() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LastSuccessTime = time.Now()
}

// recordError records an error.
func (lc *LeaseClient) recordError(err error) {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LastError = err.Error()
	lc.stats.LastErrorTime = time.Now()
}

// recordControllerError records a controller connectivity error.
func (lc *LeaseClient) recordControllerError() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.ControllerErrors++
}

// recordLeaseGranted records that a lease was granted.
func (lc *LeaseClient) recordLeaseGranted() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LeasesGranted++
}

// recordLeaseDenied records that a lease was denied.
func (lc *LeaseClient) recordLeaseDenied() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LeasesDenied++
}

// recordLeaseRelease records that a lease was released.
func (lc *LeaseClient) recordLeaseRelease() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.LeaseReleases++
}

// recordFallbackUsed records that the fallback behavior was used.
func (lc *LeaseClient) recordFallbackUsed() {
	lc.stats.mu.Lock()
	defer lc.stats.mu.Unlock()
	lc.stats.FallbacksUsed++
}

// LeaseClientOption is a functional option for configuring the lease client.
type LeaseClientOption func(*LeaseClient)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) LeaseClientOption {
	return func(lc *LeaseClient) {
		lc.httpClient = client
	}
}
