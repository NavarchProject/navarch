package main

import (
	"testing"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

func TestParseNodeStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    pb.NodeStatus
		wantErr bool
	}{
		{
			name:  "short form active",
			input: "active",
			want:  pb.NodeStatus_NODE_STATUS_ACTIVE,
		},
		{
			name:  "short form cordoned",
			input: "cordoned",
			want:  pb.NodeStatus_NODE_STATUS_CORDONED,
		},
		{
			name:  "short form draining",
			input: "draining",
			want:  pb.NodeStatus_NODE_STATUS_DRAINING,
		},
		{
			name:  "short form terminated",
			input: "terminated",
			want:  pb.NodeStatus_NODE_STATUS_TERMINATED,
		},
		{
			name:  "uppercase short form",
			input: "ACTIVE",
			want:  pb.NodeStatus_NODE_STATUS_ACTIVE,
		},
		{
			name:  "mixed case short form",
			input: "Active",
			want:  pb.NodeStatus_NODE_STATUS_ACTIVE,
		},
		{
			name:  "full form",
			input: "NODE_STATUS_ACTIVE",
			want:  pb.NodeStatus_NODE_STATUS_ACTIVE,
		},
		{
			name:    "invalid status",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNodeStatus(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseNodeStatus(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseNodeStatus(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseNodeStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		input pb.NodeStatus
		want  string
	}{
		{pb.NodeStatus_NODE_STATUS_ACTIVE, "Active"},
		{pb.NodeStatus_NODE_STATUS_CORDONED, "Cordoned"},
		{pb.NodeStatus_NODE_STATUS_DRAINING, "Draining"},
		{pb.NodeStatus_NODE_STATUS_TERMINATED, "Terminated"},
		{pb.NodeStatus_NODE_STATUS_UNKNOWN, "Unknown"},
		{pb.NodeStatus(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatStatus(tt.input)
			if got != tt.want {
				t.Errorf("formatStatus(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatHealthStatus(t *testing.T) {
	tests := []struct {
		input pb.HealthStatus
		want  string
	}{
		{pb.HealthStatus_HEALTH_STATUS_HEALTHY, "Healthy"},
		{pb.HealthStatus_HEALTH_STATUS_DEGRADED, "Degraded"},
		{pb.HealthStatus_HEALTH_STATUS_UNHEALTHY, "Unhealthy"},
		{pb.HealthStatus_HEALTH_STATUS_UNKNOWN, "Unknown"},
		{pb.HealthStatus(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatHealthStatus(tt.input)
			if got != tt.want {
				t.Errorf("formatHealthStatus(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "zero time",
			time: time.Time{},
			want: "Never",
		},
		{
			name: "30 seconds ago",
			time: time.Now().Add(-30 * time.Second),
			want: "30s ago",
		},
		{
			name: "5 minutes ago",
			time: time.Now().Add(-5 * time.Minute),
			want: "5m ago",
		},
		{
			name: "2 hours ago",
			time: time.Now().Add(-2 * time.Hour),
			want: "2h ago",
		},
		{
			name: "3 days ago",
			time: time.Now().Add(-3 * 24 * time.Hour),
			want: "3d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimestamp(tt.time)
			if got != tt.want {
				t.Errorf("formatTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}

