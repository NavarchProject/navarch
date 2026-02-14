package preemption

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAWSDetector_NoPreemption(t *testing.T) {
	// Mock IMDS server
	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT for token, got %s", r.Method)
		}
		w.Write([]byte("mock-token"))
	})

	termHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-aws-ec2-metadata-token") != "mock-token" {
			t.Error("missing or wrong token header")
		}
		// 404 means no termination notice
		w.WriteHeader(http.StatusNotFound)
	})

	mux := http.NewServeMux()
	mux.Handle("/latest/api/token", tokenHandler)
	mux.Handle("/latest/meta-data/spot/termination-time", termHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	detector := &AWSDetector{
		client:     server.Client(),
		instanceID: "i-12345",
	}
	// Override the IMDS URL by using a custom transport
	detector.client.Transport = &urlRewriteTransport{
		base:    http.DefaultTransport,
		fromURL: "http://169.254.169.254",
		toURL:   server.URL,
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Preempted {
		t.Error("expected Preempted=false")
	}
	if status.Provider != "aws" {
		t.Errorf("expected provider=aws, got %s", status.Provider)
	}
	if status.InstanceID != "i-12345" {
		t.Errorf("expected instanceID=i-12345, got %s", status.InstanceID)
	}
}

func TestAWSDetector_Preempted(t *testing.T) {
	terminateTime := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)

	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("mock-token"))
	})

	termHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(terminateTime))
	})

	mux := http.NewServeMux()
	mux.Handle("/latest/api/token", tokenHandler)
	mux.Handle("/latest/meta-data/spot/termination-time", termHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	detector := &AWSDetector{
		client:     server.Client(),
		instanceID: "i-12345",
	}
	detector.client.Transport = &urlRewriteTransport{
		base:    http.DefaultTransport,
		fromURL: "http://169.254.169.254",
		toURL:   server.URL,
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Preempted {
		t.Error("expected Preempted=true")
	}
	if status.TerminateAt.IsZero() {
		t.Error("expected non-zero TerminateAt")
	}
}

func TestGCPDetector_NoPreemption(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			t.Error("missing Metadata-Flavor header")
		}
		w.Write([]byte("FALSE"))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	detector := &GCPDetector{
		client:     server.Client(),
		instanceID: "gcp-12345",
	}
	detector.client.Transport = &urlRewriteTransport{
		base:    http.DefaultTransport,
		fromURL: "http://metadata.google.internal",
		toURL:   server.URL,
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Preempted {
		t.Error("expected Preempted=false")
	}
	if status.Provider != "gcp" {
		t.Errorf("expected provider=gcp, got %s", status.Provider)
	}
}

func TestGCPDetector_Preempted(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("TRUE"))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	detector := &GCPDetector{
		client:     server.Client(),
		instanceID: "gcp-12345",
	}
	detector.client.Transport = &urlRewriteTransport{
		base:    http.DefaultTransport,
		fromURL: "http://metadata.google.internal",
		toURL:   server.URL,
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Preempted {
		t.Error("expected Preempted=true")
	}
	// GCP gives ~30 second warning, so TerminateAt should be set
	if status.TerminateAt.IsZero() {
		t.Error("expected non-zero TerminateAt")
	}
}

func TestMultiDetector_FirstSuccess(t *testing.T) {
	detector := &MultiDetector{
		detectors: []Detector{
			&mockDetector{err: nil, status: &Status{Provider: "mock1", Preempted: false}},
			&mockDetector{err: nil, status: &Status{Provider: "mock2", Preempted: false}},
		},
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Provider != "mock1" {
		t.Errorf("expected first detector result, got provider=%s", status.Provider)
	}
}

func TestMultiDetector_Fallback(t *testing.T) {
	detector := &MultiDetector{
		detectors: []Detector{
			&mockDetector{err: context.DeadlineExceeded, status: nil},
			&mockDetector{err: nil, status: &Status{Provider: "mock2", Preempted: false}},
		},
	}

	status, err := detector.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Provider != "mock2" {
		t.Errorf("expected second detector result, got provider=%s", status.Provider)
	}
}

func TestMultiDetector_AllFail(t *testing.T) {
	detector := &MultiDetector{
		detectors: []Detector{
			&mockDetector{err: context.DeadlineExceeded, status: nil},
			&mockDetector{err: context.Canceled, status: nil},
		},
	}

	_, err := detector.Check(context.Background())
	if err == nil {
		t.Error("expected error when all detectors fail")
	}
}

func TestWatcher_DetectsPreemption(t *testing.T) {
	callCount := 0
	detector := &mockDetector{
		checkFunc: func() (*Status, error) {
			callCount++
			if callCount >= 3 {
				return &Status{Preempted: true, Provider: "mock"}, nil
			}
			return &Status{Preempted: false, Provider: "mock"}, nil
		},
	}

	var receivedStatus Status
	watcher := NewWatcher(detector, 10*time.Millisecond, func(s Status) {
		receivedStatus = s
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := watcher.Watch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !receivedStatus.Preempted {
		t.Error("expected preemption callback to be called")
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 checks, got %d", callCount)
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	detector := &mockDetector{
		status: &Status{Preempted: false, Provider: "mock"},
	}

	watcher := NewWatcher(detector, 10*time.Millisecond, func(s Status) {
		t.Error("callback should not be called")
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately
	cancel()

	err := watcher.Watch(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// mockDetector for testing
type mockDetector struct {
	status    *Status
	err       error
	checkFunc func() (*Status, error)
}

func (m *mockDetector) Check(ctx context.Context) (*Status, error) {
	if m.checkFunc != nil {
		return m.checkFunc()
	}
	return m.status, m.err
}

// urlRewriteTransport rewrites URLs for testing
type urlRewriteTransport struct {
	base    http.RoundTripper
	fromURL string
	toURL   string
}

func (t *urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL
	newURL := t.toURL + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return t.base.RoundTrip(newReq)
}
