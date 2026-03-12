package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestNewHandler(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	if handler == nil {
		t.Fatal("NewHandler() returned nil handler")
	}
}

func TestDashboard(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	// Add some test nodes
	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:   "node-1",
		Provider: "gcp",
		Region:   "us-central1",
		Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
		GPUs:     []*pb.GPUInfo{{Index: 0}, {Index: 1}},
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:   "node-2",
		Provider: "gcp",
		Region:   "us-central1",
		Status:   pb.NodeStatus_NODE_STATUS_UNHEALTHY,
		GPUs:     []*pb.GPUInfo{{Index: 0}},
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Fleet Dashboard") {
		t.Error("response should contain 'Fleet Dashboard'")
	}
	if !strings.Contains(body, "2") { // Total nodes
		t.Error("response should contain node count")
	}
	if !strings.Contains(body, "Unhealthy Nodes") {
		t.Error("response should show unhealthy nodes section")
	}
}

func TestNodesList(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:       "node-1",
		Provider:     "gcp",
		Region:       "us-central1",
		InstanceType: "a3-highgpu-8g",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/nodes", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "node-1") {
		t.Error("response should contain node ID")
	}
	if !strings.Contains(body, "a3-highgpu-8g") {
		t.Error("response should contain instance type")
	}
}

func TestNodesListWithFilter(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "active-node",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	})
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "cordoned-node",
		Status: pb.NodeStatus_NODE_STATUS_CORDONED,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Filter by Active status
	req := httptest.NewRequest("GET", "/ui/nodes?status=Active", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "active-node") {
		t.Error("response should contain active node")
	}
	if strings.Contains(body, "cordoned-node") {
		t.Error("response should not contain cordoned node when filtering by Active")
	}
}

func TestNodesListHTMXRequest(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// HTMX request should only return table body
	req := httptest.NewRequest("GET", "/ui/nodes", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	// Should contain table content but not full page
	if !strings.Contains(body, "node-1") {
		t.Error("HTMX response should contain node data")
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("HTMX response should not contain full HTML document")
	}
}

func TestNodeDetail(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:       "node-1",
		Provider:     "gcp",
		Region:       "us-central1",
		Zone:         "us-central1-a",
		InstanceType: "a3-highgpu-8g",
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		HealthStatus: pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		GPUs: []*pb.GPUInfo{
			{Index: 0, Name: "H100", Uuid: "GPU-123"},
		},
		Metadata: &pb.NodeMetadata{
			Hostname:   "gpu-node-1",
			InternalIp: "10.0.0.1",
		},
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/nodes/node-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	checks := []string{"node-1", "gcp", "us-central1", "H100", "gpu-node-1", "Cordon"}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("response should contain '%s'", check)
		}
	}
}

func TestNodeDetail_NotFound(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/nodes/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for not found, got %d", w.Code)
	}
}

func TestNodeAction_Cordon(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	form := url.Values{}
	form.Set("action", "cordon")
	form.Set("reason", "maintenance")

	req := httptest.NewRequest("POST", "/ui/nodes/node-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}

	// Verify node status changed
	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_CORDONED {
		t.Errorf("expected node status CORDONED, got %v", node.Status)
	}
}

func TestNodeAction_Uncordon(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_CORDONED,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	form := url.Values{}
	form.Set("action", "uncordon")

	req := httptest.NewRequest("POST", "/ui/nodes/node-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}

	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Errorf("expected node status ACTIVE, got %v", node.Status)
	}
}

func TestNodeAction_Drain(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	form := url.Values{}
	form.Set("action", "drain")
	form.Set("reason", "decommission")

	req := httptest.NewRequest("POST", "/ui/nodes/node-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}

	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_DRAINING {
		t.Errorf("expected node status DRAINING, got %v", node.Status)
	}
}

func TestInstancesList(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.CreateInstance(ctx, &db.InstanceRecord{
		InstanceID:   "instance-1",
		Provider:     "gcp",
		Region:       "us-central1",
		InstanceType: "a3-highgpu-8g",
		State:        pb.InstanceState_INSTANCE_STATE_RUNNING,
		PoolName:     "default",
		NodeID:       "node-1",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/instances", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "instance-1") {
		t.Error("response should contain instance ID")
	}
	if !strings.Contains(body, "Running") {
		t.Error("response should contain instance state")
	}
}

func TestInstancesListWithFilter(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	ctx := context.Background()
	database.CreateInstance(ctx, &db.InstanceRecord{
		InstanceID: "running-1",
		State:      pb.InstanceState_INSTANCE_STATE_RUNNING,
	})
	database.CreateInstance(ctx, &db.InstanceRecord{
		InstanceID: "failed-1",
		State:      pb.InstanceState_INSTANCE_STATE_FAILED,
	})

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/instances?state=Running", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "running-1") {
		t.Error("response should contain running instance")
	}
	if strings.Contains(body, "failed-1") {
		t.Error("response should not contain failed instance when filtering by Running")
	}
}

func TestStaticFiles(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	handler, err := NewHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ui/static/style.css", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for static file, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/css") {
		t.Errorf("expected CSS content type, got %s", contentType)
	}
}

func TestTemplateFunctions(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() string
		expected string
	}{
		{"statusName Active", func() string { return statusName(pb.NodeStatus_NODE_STATUS_ACTIVE) }, "Active"},
		{"statusName Cordoned", func() string { return statusName(pb.NodeStatus_NODE_STATUS_CORDONED) }, "Cordoned"},
		{"statusName Unknown", func() string { return statusName(pb.NodeStatus_NODE_STATUS_UNKNOWN) }, "Unknown"},
		{"healthName Healthy", func() string { return healthName(pb.HealthStatus_HEALTH_STATUS_HEALTHY) }, "Healthy"},
		{"healthName Degraded", func() string { return healthName(pb.HealthStatus_HEALTH_STATUS_DEGRADED) }, "Degraded"},
		{"instanceStateName Running", func() string { return instanceStateName(pb.InstanceState_INSTANCE_STATE_RUNNING) }, "Running"},
		{"instanceStateName Failed", func() string { return instanceStateName(pb.InstanceState_INSTANCE_STATE_FAILED) }, "Failed"},
		{"statusClass Active", func() string { return statusClass(pb.NodeStatus_NODE_STATUS_ACTIVE) }, "status-active"},
		{"healthClass Healthy", func() string { return healthClass(pb.HealthStatus_HEALTH_STATUS_HEALTHY) }, "health-healthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		contains string
	}{
		{"zero time", time.Time{}, "-"},
		{"seconds ago", time.Now().Add(-30 * time.Second), "s ago"},
		{"minutes ago", time.Now().Add(-5 * time.Minute), "m ago"},
		{"hours ago", time.Now().Add(-2 * time.Hour), "h ago"},
		{"days ago", time.Now().Add(-3 * 24 * time.Hour), "d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	t.Run("zero time", func(t *testing.T) {
		result := formatTime(time.Time{})
		if result != "-" {
			t.Errorf("expected '-', got %q", result)
		}
	})

	t.Run("valid time", func(t *testing.T) {
		ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		result := formatTime(ts)
		if result != "2024-01-15 10:30:00" {
			t.Errorf("expected '2024-01-15 10:30:00', got %q", result)
		}
	})
}
