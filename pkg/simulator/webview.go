package simulator

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
)

//go:embed templates/live.html
var liveTemplate embed.FS

// WebEvent represents a real-time event sent to the browser via SSE.
type WebEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Elapsed   float64     `json:"elapsed"` // seconds since simulation start
	Data      interface{} `json:"data"`
}

// WebNodeEvent is sent when a node changes state.
type WebNodeEvent struct {
	NodeID       string `json:"node_id"`
	Provider     string `json:"provider,omitempty"`
	Region       string `json:"region,omitempty"`
	Zone         string `json:"zone,omitempty"`
	InstanceType string `json:"instance_type,omitempty"`
	GPUCount     int    `json:"gpu_count,omitempty"`
	GPUType      string `json:"gpu_type,omitempty"`
	Status       string `json:"status,omitempty"`
	Message      string `json:"message,omitempty"`
	FailureType  string `json:"failure_type,omitempty"`
	XIDCode      int    `json:"xid_code,omitempty"`
	IsCascade    bool   `json:"is_cascade,omitempty"`
	PolicyRule   string `json:"policy_rule,omitempty"`
}

// WebSampleEvent is sent periodically with aggregate metrics.
type WebSampleEvent struct {
	TotalNodes    int   `json:"total_nodes"`
	Healthy       int   `json:"healthy"`
	Unhealthy     int   `json:"unhealthy"`
	Degraded      int   `json:"degraded"`
	Failures      int64 `json:"failures"`
	Cascading     int64 `json:"cascading"`
	Recoveries    int64 `json:"recoveries"`
	Outages       int64 `json:"outages"`
}

// WebInitEvent is sent on connection with the initial simulation state.
type WebInitEvent struct {
	ScenarioName string  `json:"scenario_name"`
	TotalNodes   int     `json:"total_nodes"`
	Duration     float64 `json:"duration_seconds"`
	FailureRate  float64 `json:"failure_rate"`
	Seed         int64   `json:"seed"`
}

// WebView serves the live simulation dashboard and streams events via SSE.
type WebView struct {
	logger    *slog.Logger
	clock     clock.Clock
	startTime time.Time
	addr      string

	server   *http.Server
	listener net.Listener

	mu       sync.RWMutex
	clients  map[uint64]chan []byte
	nextID   uint64
	initData *WebInitEvent
}

// NewWebView creates a new live web view server.
func NewWebView(clk clock.Clock, logger *slog.Logger) *WebView {
	if clk == nil {
		clk = clock.Real()
	}
	return &WebView{
		logger:  logger.With(slog.String("component", "webview")),
		clock:   clk,
		clients: make(map[uint64]chan []byte),
	}
}

// Start begins serving the web view on a random port.
func (wv *WebView) Start(ctx context.Context) error {
	wv.startTime = wv.clock.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/", wv.handleIndex)
	mux.HandleFunc("/events", wv.handleSSE)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	wv.listener = listener

	port := listener.Addr().(*net.TCPAddr).Port
	wv.addr = fmt.Sprintf("http://localhost:%d", port)

	wv.server = &http.Server{Handler: mux}

	go func() {
		if err := wv.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			wv.logger.Error("webview server error", slog.String("error", err.Error()))
		}
	}()

	go func() {
		<-ctx.Done()
		wv.Stop()
	}()

	wv.logger.Info("live web view started", slog.String("url", wv.addr))
	return nil
}

// Stop shuts down the web view server.
func (wv *WebView) Stop() {
	if wv.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		wv.server.Shutdown(ctx)
	}
	wv.mu.Lock()
	for id, ch := range wv.clients {
		close(ch)
		delete(wv.clients, id)
	}
	wv.mu.Unlock()
}

// Addr returns the address the web view is serving on.
func (wv *WebView) Addr() string {
	return wv.addr
}

// SetInit provides the initial simulation configuration for new connections.
func (wv *WebView) SetInit(data *WebInitEvent) {
	wv.mu.Lock()
	wv.initData = data
	wv.mu.Unlock()
}

// Broadcast sends an event to all connected clients.
func (wv *WebView) Broadcast(event WebEvent) {
	event.Timestamp = wv.clock.Now()
	event.Elapsed = wv.clock.Since(wv.startTime).Seconds()

	data, err := json.Marshal(event)
	if err != nil {
		wv.logger.Error("failed to marshal event", slog.String("error", err.Error()))
		return
	}

	wv.mu.RLock()
	defer wv.mu.RUnlock()

	for _, ch := range wv.clients {
		select {
		case ch <- data:
		default:
			// Client too slow, drop event
		}
	}
}

// EmitNodeStart sends a node_start event.
func (wv *WebView) EmitNodeStart(spec NodeSpec) {
	wv.Broadcast(WebEvent{
		Type: "node_start",
		Data: WebNodeEvent{
			NodeID:       spec.ID,
			Provider:     spec.Provider,
			Region:       spec.Region,
			Zone:         spec.Zone,
			InstanceType: spec.InstanceType,
			GPUCount:     spec.GPUCount,
			GPUType:      spec.GPUType,
			Status:       "healthy",
		},
	})
}

// EmitFailure sends a failure event.
func (wv *WebView) EmitFailure(event FailureEvent) {
	wv.Broadcast(WebEvent{
		Type: "failure",
		Data: WebNodeEvent{
			NodeID:      event.NodeID,
			FailureType: event.Type,
			XIDCode:     event.XIDCode,
			Message:     event.Message,
			IsCascade:   event.IsCascade,
			Status:      "unhealthy",
		},
	})
}

// EmitHealthChange sends a health status change event.
func (wv *WebView) EmitHealthChange(nodeID, status string) {
	wv.Broadcast(WebEvent{
		Type: "health_change",
		Data: WebNodeEvent{
			NodeID: nodeID,
			Status: status,
		},
	})
}

// EmitRecovery sends a recovery event.
func (wv *WebView) EmitRecovery(nodeID, failureType string) {
	wv.Broadcast(WebEvent{
		Type: "recovery",
		Data: WebNodeEvent{
			NodeID:      nodeID,
			FailureType: failureType,
			Status:      "healthy",
		},
	})
}

// EmitSample sends periodic aggregate metrics.
func (wv *WebView) EmitSample(sample WebSampleEvent) {
	wv.Broadcast(WebEvent{
		Type: "sample",
		Data: sample,
	})
}

// EmitComplete signals the simulation has finished.
func (wv *WebView) EmitComplete() {
	wv.Broadcast(WebEvent{
		Type: "complete",
		Data: nil,
	})
}

// --- MetricsListener implementation ---

func (wv *WebView) OnNodeStart(spec NodeSpec)              { wv.EmitNodeStart(spec) }
func (wv *WebView) OnFailure(event FailureEvent)           { wv.EmitFailure(event) }
func (wv *WebView) OnHealthChange(nodeID, status string)   { wv.EmitHealthChange(nodeID, status) }
func (wv *WebView) OnRecovery(nodeID, failureType string)  { wv.EmitRecovery(nodeID, failureType) }
func (wv *WebView) OnSample(sample WebSampleEvent)         { wv.EmitSample(sample) }

func (wv *WebView) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := liveTemplate.ReadFile("templates/live.html")
	if err != nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (wv *WebView) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Register this client
	ch := make(chan []byte, 256)
	wv.mu.Lock()
	id := wv.nextID
	wv.nextID++
	wv.clients[id] = ch

	// Send init event if available
	initData := wv.initData
	wv.mu.Unlock()

	if initData != nil {
		initEvent := WebEvent{
			Type:      "init",
			Timestamp: wv.clock.Now(),
			Elapsed:   wv.clock.Since(wv.startTime).Seconds(),
			Data:      initData,
		}
		data, _ := json.Marshal(initEvent)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	defer func() {
		wv.mu.Lock()
		delete(wv.clients, id)
		wv.mu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
