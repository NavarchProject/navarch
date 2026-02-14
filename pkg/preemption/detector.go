// Package preemption provides detection of spot/preemptible instance termination notices.
package preemption

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Status represents the preemption state of an instance.
type Status struct {
	Preempted    bool      // True if preemption notice received
	TerminateAt  time.Time // When the instance will be terminated (zero if not preempted)
	Provider     string    // Which provider detected the preemption
	InstanceID   string    // Instance ID (for logging)
}

// Detector checks for preemption notices from cloud providers.
type Detector interface {
	// Check returns the preemption status for this instance.
	// Returns (status, nil) on success, (nil, error) on failure to check.
	Check(ctx context.Context) (*Status, error)
}

// AWSDetector checks AWS spot instance termination notices via IMDS.
type AWSDetector struct {
	client     *http.Client
	instanceID string
}

// NewAWSDetector creates a detector for AWS spot termination notices.
func NewAWSDetector(instanceID string) *AWSDetector {
	return &AWSDetector{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		instanceID: instanceID,
	}
}

// Check queries AWS Instance Metadata Service for spot termination notice.
// AWS provides a 2-minute warning before spot termination.
func (d *AWSDetector) Check(ctx context.Context) (*Status, error) {
	// First, get IMDSv2 token
	tokenReq, err := http.NewRequestWithContext(ctx, "PUT", "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "60")

	tokenResp, err := d.client.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get IMDS token: %w", err)
	}
	defer tokenResp.Body.Close()

	tokenBytes, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token: %w", err)
	}
	token := string(tokenBytes)

	// Check for spot termination notice
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://169.254.169.254/latest/meta-data/spot/termination-time", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check termination notice: %w", err)
	}
	defer resp.Body.Close()

	// 404 means no termination notice (good!)
	if resp.StatusCode == http.StatusNotFound {
		return &Status{
			Preempted:  false,
			Provider:   "aws",
			InstanceID: d.instanceID,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Parse termination time (format: 2015-01-05T18:02:00Z)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read termination time: %w", err)
	}

	terminateAt, err := time.Parse(time.RFC3339, strings.TrimSpace(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse termination time: %w", err)
	}

	return &Status{
		Preempted:   true,
		TerminateAt: terminateAt,
		Provider:    "aws",
		InstanceID:  d.instanceID,
	}, nil
}

// GCPDetector checks GCP preemptible/spot instance termination notices via metadata.
type GCPDetector struct {
	client     *http.Client
	instanceID string
}

// NewGCPDetector creates a detector for GCP preemption notices.
func NewGCPDetector(instanceID string) *GCPDetector {
	return &GCPDetector{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		instanceID: instanceID,
	}
}

// Check queries GCP metadata server for preemption notice.
// GCP provides a 30-second warning before preemption.
func (d *GCPDetector) Check(ctx context.Context) (*Status, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://metadata.google.internal/computeMetadata/v1/instance/preempted", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check preemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	preempted := strings.TrimSpace(string(body)) == "TRUE"

	status := &Status{
		Preempted:  preempted,
		Provider:   "gcp",
		InstanceID: d.instanceID,
	}

	if preempted {
		// GCP gives ~30 seconds warning
		status.TerminateAt = time.Now().Add(30 * time.Second)
	}

	return status, nil
}

// MultiDetector tries multiple detectors and returns the first successful result.
// Useful when you don't know which cloud you're running on.
type MultiDetector struct {
	detectors []Detector
}

// NewMultiDetector creates a detector that tries multiple providers.
func NewMultiDetector(instanceID string) *MultiDetector {
	return &MultiDetector{
		detectors: []Detector{
			NewAWSDetector(instanceID),
			NewGCPDetector(instanceID),
		},
	}
}

// Check tries each detector until one succeeds.
func (d *MultiDetector) Check(ctx context.Context) (*Status, error) {
	var lastErr error
	for _, detector := range d.detectors {
		status, err := detector.Check(ctx)
		if err == nil {
			return status, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all detectors failed, last error: %w", lastErr)
}

// Watcher continuously monitors for preemption and notifies via callback.
type Watcher struct {
	detector Detector
	interval time.Duration
	onPreempt func(Status)
}

// NewWatcher creates a preemption watcher.
func NewWatcher(detector Detector, interval time.Duration, onPreempt func(Status)) *Watcher {
	if interval == 0 {
		interval = 5 * time.Second
	}
	return &Watcher{
		detector:  detector,
		interval:  interval,
		onPreempt: onPreempt,
	}
}

// Watch starts watching for preemption notices.
// Blocks until context is cancelled or preemption is detected.
func (w *Watcher) Watch(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := w.detector.Check(ctx)
			if err != nil {
				// Log but continue - metadata service might be temporarily unavailable
				continue
			}
			if status.Preempted {
				w.onPreempt(*status)
				return nil
			}
		}
	}
}
