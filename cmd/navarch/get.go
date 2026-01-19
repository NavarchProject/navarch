package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func getCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <node-id>",
		Short: "Get details about a specific node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]

			client := protoconnect.NewControlPlaneServiceClient(
				http.DefaultClient,
				controlPlaneAddr,
			)

			req := &pb.GetNodeRequest{
				NodeId: nodeID,
			}

			resp, err := client.GetNode(context.Background(), connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to get node: %w", err)
			}

			switch outputFormat {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Msg.Node)
			case "table":
				return outputNodeDetails(resp.Msg.Node)
			default:
				return fmt.Errorf("unsupported output format: %s", outputFormat)
			}
		},
	}

	return cmd
}

func outputNodeDetails(node *pb.NodeInfo) error {
	fmt.Printf("Node ID:       %s\n", node.NodeId)
	fmt.Printf("Provider:      %s\n", node.Provider)
	fmt.Printf("Region:        %s\n", node.Region)
	fmt.Printf("Zone:          %s\n", node.Zone)
	fmt.Printf("Instance Type: %s\n", node.InstanceType)
	fmt.Printf("Status:        %s\n", formatStatus(node.Status))
	fmt.Printf("Health:        %s\n", formatHealthStatus(node.HealthStatus))

	if node.LastHeartbeat != nil {
		fmt.Printf("Last Heartbeat: %s\n", formatTimestamp(node.LastHeartbeat.AsTime()))
	} else {
		fmt.Printf("Last Heartbeat: Never\n")
	}

	if len(node.Gpus) > 0 {
		fmt.Printf("\nGPUs:\n")
		for _, gpu := range node.Gpus {
			fmt.Printf("  GPU %d:\n", gpu.Index)
			fmt.Printf("    UUID:       %s\n", gpu.Uuid)
			fmt.Printf("    Name:       %s\n", gpu.Name)
			fmt.Printf("    PCI Bus ID: %s\n", gpu.PciBusId)
		}
	}

	if node.Metadata != nil {
		fmt.Printf("\nMetadata:\n")
		if node.Metadata.Hostname != "" {
			fmt.Printf("  Hostname:    %s\n", node.Metadata.Hostname)
		}
		if node.Metadata.InternalIp != "" {
			fmt.Printf("  Internal IP: %s\n", node.Metadata.InternalIp)
		}
		if node.Metadata.ExternalIp != "" {
			fmt.Printf("  External IP: %s\n", node.Metadata.ExternalIp)
		}
	}

	return nil
}
