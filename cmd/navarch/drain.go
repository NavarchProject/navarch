package main

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func drainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drain <node-id>",
		Short: "Drain a node (evict workloads and mark unschedulable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]

			client := protoconnect.NewControlPlaneServiceClient(
				http.DefaultClient,
				controlPlaneAddr,
			)

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			}

			resp, err := client.IssueCommand(context.Background(), connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to drain node: %w", err)
			}

			fmt.Printf("Node %s draining\n", nodeID)
			fmt.Printf("Command ID: %s\n", resp.Msg.CommandId)

			return nil
		},
	}

	return cmd
}

func uncordonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uncordon <node-id>",
		Short: "Mark a node as schedulable (not implemented yet)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("uncordon command not yet implemented in control plane")
		},
	}

	return cmd
}
