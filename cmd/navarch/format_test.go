package main

import (
	"testing"

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
