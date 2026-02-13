package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

//go:embed templates/*.html static/*.css
var content embed.FS

// Handler serves the web UI for fleet monitoring.
type Handler struct {
	db        db.DB
	templates *template.Template
	logger    *slog.Logger
}

// NewHandler creates a new UI handler.
func NewHandler(database db.DB, logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}

	funcMap := template.FuncMap{
		"statusClass":       statusClass,
		"healthClass":       healthClass,
		"instanceStateClass": instanceStateClass,
		"formatTime":        formatTime,
		"formatDuration":    formatDuration,
		"statusName":        statusName,
		"healthName":        healthName,
		"instanceStateName": instanceStateName,
		"divf":              func(a, b int64) float64 { return float64(a) / float64(b) },
		"dict": func(values ...interface{}) map[string]interface{} {
			d := make(map[string]interface{})
			for i := 0; i < len(values); i += 2 {
				key, _ := values[i].(string)
				d[key] = values[i+1]
			}
			return d
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Handler{
		db:        database,
		templates: tmpl,
		logger:    logger,
	}, nil
}

// RegisterRoutes registers all UI routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ui/", h.handleDashboard)
	mux.HandleFunc("/ui/nodes", h.handleNodes)
	mux.HandleFunc("/ui/nodes/", h.handleNodeDetail)
	mux.HandleFunc("/ui/instances", h.handleInstances)

	// Serve static files from embedded filesystem
	staticFS, _ := fs.Sub(content, "static")
	mux.Handle("/ui/static/", http.StripPrefix("/ui/static/", http.FileServer(http.FS(staticFS))))
}

// Dashboard data

type dashboardData struct {
	TotalNodes     int
	ActiveNodes    int
	CordonedNodes  int
	DrainingNodes  int
	UnhealthyNodes int
	TotalGPUs      int
	UnhealthyList  []*db.NodeRecord
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui/" && r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}

	nodes, err := h.db.ListNodes(r.Context())
	if err != nil {
		h.renderError(w, "Failed to load nodes", err)
		return
	}

	data := dashboardData{}
	for _, node := range nodes {
		data.TotalNodes++
		data.TotalGPUs += len(node.GPUs)

		switch node.Status {
		case pb.NodeStatus_NODE_STATUS_ACTIVE:
			data.ActiveNodes++
		case pb.NodeStatus_NODE_STATUS_CORDONED:
			data.CordonedNodes++
		case pb.NodeStatus_NODE_STATUS_DRAINING:
			data.DrainingNodes++
		case pb.NodeStatus_NODE_STATUS_UNHEALTHY:
			data.UnhealthyNodes++
			data.UnhealthyList = append(data.UnhealthyList, node)
		}
	}

	h.render(w, "dashboard.html", data)
}

// Nodes list

type nodesData struct {
	Nodes        []*db.NodeRecord
	StatusFilter string
}

func (h *Handler) handleNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.db.ListNodes(r.Context())
	if err != nil {
		h.renderError(w, "Failed to load nodes", err)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	var filtered []*db.NodeRecord

	if statusFilter != "" {
		for _, node := range nodes {
			if statusName(node.Status) == statusFilter {
				filtered = append(filtered, node)
			}
		}
	} else {
		filtered = nodes
	}

	data := nodesData{
		Nodes:        filtered,
		StatusFilter: statusFilter,
	}

	// For HTMX requests, only render the table body
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "nodes_table.html", data)
		return
	}

	h.render(w, "nodes.html", data)
}

// Node detail

type nodeDetailData struct {
	Node    *db.NodeRecord
	Message string
	Error   string
}

func (h *Handler) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimPrefix(r.URL.Path, "/ui/nodes/")
	if nodeID == "" {
		http.Redirect(w, r, "/ui/nodes", http.StatusFound)
		return
	}

	// Handle POST actions
	if r.Method == http.MethodPost {
		h.handleNodeAction(w, r, nodeID)
		return
	}

	node, err := h.db.GetNode(r.Context(), nodeID)
	if err != nil {
		h.renderError(w, "Node not found", err)
		return
	}

	data := nodeDetailData{
		Node:    node,
		Message: r.URL.Query().Get("message"),
		Error:   r.URL.Query().Get("error"),
	}

	h.render(w, "node.html", data)
}

func (h *Handler) handleNodeAction(w http.ResponseWriter, r *http.Request, nodeID string) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/ui/nodes/%s?error=Invalid+form", nodeID), http.StatusFound)
		return
	}

	action := r.FormValue("action")
	reason := r.FormValue("reason")

	var commandType pb.NodeCommandType
	switch action {
	case "cordon":
		commandType = pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON
	case "uncordon":
		commandType = pb.NodeCommandType_NODE_COMMAND_TYPE_UNCORDON
	case "drain":
		commandType = pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN
	default:
		http.Redirect(w, r, fmt.Sprintf("/ui/nodes/%s?error=Unknown+action", nodeID), http.StatusFound)
		return
	}

	// Issue command via DB (simplified - in production would use the RPC)
	cmd := &db.CommandRecord{
		CommandID:  fmt.Sprintf("ui-%d", time.Now().UnixNano()),
		NodeID:     nodeID,
		Type:       commandType,
		Parameters: map[string]string{"reason": reason},
		IssuedAt:   time.Now(),
		Status:     "pending",
	}

	// For cordon/uncordon/drain, update node status directly
	ctx := r.Context()
	var newStatus pb.NodeStatus
	switch action {
	case "cordon":
		newStatus = pb.NodeStatus_NODE_STATUS_CORDONED
	case "uncordon":
		newStatus = pb.NodeStatus_NODE_STATUS_ACTIVE
	case "drain":
		newStatus = pb.NodeStatus_NODE_STATUS_DRAINING
	}

	if err := h.db.UpdateNodeStatus(ctx, nodeID, newStatus); err != nil {
		h.logger.Error("failed to update node status", slog.String("node_id", nodeID), slog.String("error", err.Error()))
		http.Redirect(w, r, fmt.Sprintf("/ui/nodes/%s?error=Failed+to+%s+node", nodeID, action), http.StatusFound)
		return
	}

	// Record command for audit trail
	cmd.Status = "completed"
	if err := h.db.CreateCommand(ctx, cmd); err != nil {
		h.logger.Warn("failed to record command", slog.String("error", err.Error()))
	}

	h.logger.Info("node action executed",
		slog.String("node_id", nodeID),
		slog.String("action", action),
	)

	http.Redirect(w, r, fmt.Sprintf("/ui/nodes/%s?message=Node+%sed+successfully", nodeID, action), http.StatusFound)
}

// Instances list

type instancesData struct {
	Instances   []*db.InstanceRecord
	StateFilter string
}

func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.db.ListInstances(r.Context())
	if err != nil {
		h.renderError(w, "Failed to load instances", err)
		return
	}

	stateFilter := r.URL.Query().Get("state")
	var filtered []*db.InstanceRecord

	if stateFilter != "" {
		for _, inst := range instances {
			if instanceStateName(inst.State) == stateFilter {
				filtered = append(filtered, inst)
			}
		}
	} else {
		filtered = instances
	}

	data := instancesData{
		Instances:   filtered,
		StateFilter: stateFilter,
	}

	// For HTMX requests, only render the table body
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "instances_table.html", data)
		return
	}

	h.render(w, "instances.html", data)
}

// Rendering helpers

func (h *Handler) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error("template render failed", slog.String("template", name), slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) renderError(w http.ResponseWriter, message string, err error) {
	h.logger.Error(message, slog.String("error", err.Error()))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Error</title></head><body><h1>Error</h1><p>%s: %s</p><a href="/ui/">Back to Dashboard</a></body></html>`, message, err.Error())
}

// Template functions

func statusClass(status pb.NodeStatus) string {
	switch status {
	case pb.NodeStatus_NODE_STATUS_ACTIVE:
		return "status-active"
	case pb.NodeStatus_NODE_STATUS_CORDONED:
		return "status-cordoned"
	case pb.NodeStatus_NODE_STATUS_DRAINING:
		return "status-draining"
	case pb.NodeStatus_NODE_STATUS_UNHEALTHY:
		return "status-unhealthy"
	case pb.NodeStatus_NODE_STATUS_TERMINATED:
		return "status-terminated"
	default:
		return "status-unknown"
	}
}

func healthClass(status pb.HealthStatus) string {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "health-healthy"
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		return "health-degraded"
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "health-unhealthy"
	default:
		return "health-unknown"
	}
}

func instanceStateClass(state pb.InstanceState) string {
	switch state {
	case pb.InstanceState_INSTANCE_STATE_RUNNING:
		return "state-running"
	case pb.InstanceState_INSTANCE_STATE_PROVISIONING, pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION:
		return "state-pending"
	case pb.InstanceState_INSTANCE_STATE_TERMINATING, pb.InstanceState_INSTANCE_STATE_TERMINATED:
		return "state-terminated"
	case pb.InstanceState_INSTANCE_STATE_FAILED:
		return "state-failed"
	default:
		return "state-unknown"
	}
}

func statusName(status pb.NodeStatus) string {
	switch status {
	case pb.NodeStatus_NODE_STATUS_ACTIVE:
		return "Active"
	case pb.NodeStatus_NODE_STATUS_CORDONED:
		return "Cordoned"
	case pb.NodeStatus_NODE_STATUS_DRAINING:
		return "Draining"
	case pb.NodeStatus_NODE_STATUS_UNHEALTHY:
		return "Unhealthy"
	case pb.NodeStatus_NODE_STATUS_TERMINATED:
		return "Terminated"
	default:
		return "Unknown"
	}
}

func healthName(status pb.HealthStatus) string {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "Healthy"
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		return "Degraded"
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "Unhealthy"
	default:
		return "Unknown"
	}
}

func instanceStateName(state pb.InstanceState) string {
	switch state {
	case pb.InstanceState_INSTANCE_STATE_PROVISIONING:
		return "Provisioning"
	case pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION:
		return "Pending"
	case pb.InstanceState_INSTANCE_STATE_RUNNING:
		return "Running"
	case pb.InstanceState_INSTANCE_STATE_TERMINATING:
		return "Terminating"
	case pb.InstanceState_INSTANCE_STATE_TERMINATED:
		return "Terminated"
	case pb.InstanceState_INSTANCE_STATE_FAILED:
		return "Failed"
	default:
		return "Unknown"
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatDuration(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
