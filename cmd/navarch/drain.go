package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
)

func drainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drain <node-id>",
		Short: "Drain a node (evict workloads and mark unschedulable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			client := newClient()

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			}

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			resp, err := client.IssueCommand(ctx, connect.NewRequest(req))
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
		Short: "Mark a node as schedulable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			client := newClient()

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_UNCORDON,
			}

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			resp, err := client.IssueCommand(ctx, connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to uncordon node: %w", err)
			}

			fmt.Printf("Node %s uncordoned successfully\n", nodeID)
			fmt.Printf("Command ID: %s\n", resp.Msg.CommandId)

			return nil
		},
	}

	return cmd
}
