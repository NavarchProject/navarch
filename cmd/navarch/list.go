package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
)

func listCmd() *cobra.Command {
	var provider, region string
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()

			req := &pb.ListNodesRequest{
				Provider: provider,
				Region:   region,
			}

			if status != "" {
				statusEnum, err := parseNodeStatus(status)
				if err != nil {
					return err
				}
				req.Status = statusEnum
			}

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			resp, err := client.ListNodes(ctx, connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to list nodes: %w", err)
			}

			if len(resp.Msg.Nodes) == 0 {
				fmt.Println("No nodes found")
				return nil
			}

			switch outputFormat {
			case "json":
				return outputJSON(resp.Msg.Nodes)
			case "table":
				return outputTable(resp.Msg.Nodes)
			default:
				return fmt.Errorf("unsupported output format: %s", outputFormat)
			}
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "Filter by provider")
	cmd.Flags().StringVar(&region, "region", "", "Filter by region")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (active, cordoned, draining, terminated)")

	return cmd
}

func outputJSON(nodes []*pb.NodeInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(nodes)
}

func outputTable(nodes []*pb.NodeInfo) error {
	table := tablewriter.NewWriter(os.Stdout)
	table.Append([]string{"Node ID", "Provider", "Region", "Zone", "Instance Type", "Status", "Health", "Last Heartbeat", "GPUs"})

	for _, node := range nodes {
		lastHeartbeat := "Never"
		if node.LastHeartbeat != nil {
			lastHeartbeat = formatTimestamp(node.LastHeartbeat.AsTime())
		}

		gpuCount := fmt.Sprintf("%d", len(node.Gpus))

		table.Append([]string{
			node.NodeId,
			node.Provider,
			node.Region,
			node.Zone,
			node.InstanceType,
			formatStatus(node.Status),
			formatHealthStatus(node.HealthStatus),
			lastHeartbeat,
			gpuCount,
		})
	}

	table.Render()
	return nil
}

func formatStatus(status pb.NodeStatus) string {
	switch status {
	case pb.NodeStatus_NODE_STATUS_ACTIVE:
		return "Active"
	case pb.NodeStatus_NODE_STATUS_CORDONED:
		return "Cordoned"
	case pb.NodeStatus_NODE_STATUS_DRAINING:
		return "Draining"
	case pb.NodeStatus_NODE_STATUS_TERMINATED:
		return "Terminated"
	default:
		return "Unknown"
	}
}

func formatHealthStatus(status pb.HealthStatus) string {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "Healthy"
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		return "Degraded"
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "Unhealthy"
	case pb.HealthStatus_HEALTH_STATUS_UNKNOWN:
		return "Unknown"
	default:
		return "Unknown"
	}
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}
	duration := time.Since(t)
	if duration < time.Minute {
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(duration.Hours()/24))
}

func parseNodeStatus(s string) (pb.NodeStatus, error) {
	// Accept both short form (active) and full form (NODE_STATUS_ACTIVE)
	normalized := strings.ToUpper(s)
	if !strings.HasPrefix(normalized, "NODE_STATUS_") {
		normalized = "NODE_STATUS_" + normalized
	}

	statusValue, ok := pb.NodeStatus_value[normalized]
	if !ok {
		return 0, fmt.Errorf("invalid status: %s (valid: active, cordoned, draining, terminated)", s)
	}
	return pb.NodeStatus(statusValue), nil
}
