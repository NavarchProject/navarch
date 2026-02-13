package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

const staleHeartbeatThreshold = 2 * time.Minute

func drainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drain <node-id>",
		Short: "Drain a node (evict workloads and mark unschedulable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			client := newClient()

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			warnIfNodeOffline(ctx, client, nodeID)

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN,
			}

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

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			warnIfNodeOffline(ctx, client, nodeID)

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_UNCORDON,
			}

			resp, err := client.IssueCommand(ctx, connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to uncordon node: %w", err)
			}

			fmt.Printf("Node %s uncordoned\n", nodeID)
			fmt.Printf("Command ID: %s\n", resp.Msg.CommandId)

			return nil
		},
	}

	return cmd
}

func warnIfNodeOffline(ctx context.Context, client protoconnect.ControlPlaneServiceClient, nodeID string) {
	resp, err := client.GetNode(ctx, connect.NewRequest(&pb.GetNodeRequest{NodeId: nodeID}))
	if err != nil {
		return // Don't block command on lookup failure
	}

	node := resp.Msg.Node
	if node.LastHeartbeat == nil {
		fmt.Fprintf(os.Stderr, "Warning: node %s has never sent a heartbeat\n", nodeID)
		return
	}

	age := time.Since(node.LastHeartbeat.AsTime())
	if age > staleHeartbeatThreshold {
		fmt.Fprintf(os.Stderr, "Warning: node %s last heartbeat was %s ago (may be offline)\n", nodeID, age.Round(time.Second))
	}
}
