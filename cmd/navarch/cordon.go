package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
)

func cordonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cordon <node-id>",
		Short: "Mark a node as unschedulable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			client := newClient()

			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			warnIfNodeOffline(ctx, client, nodeID)

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			}

			resp, err := client.IssueCommand(ctx, connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to cordon node: %w", err)
			}

			fmt.Printf("Node %s cordoned\n", nodeID)
			fmt.Printf("Command ID: %s\n", resp.Msg.CommandId)

			return nil
		},
	}

	return cmd
}
