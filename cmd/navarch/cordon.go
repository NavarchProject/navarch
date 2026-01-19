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

func cordonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cordon <node-id>",
		Short: "Mark a node as unschedulable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]

			client := protoconnect.NewControlPlaneServiceClient(
				http.DefaultClient,
				controlPlaneAddr,
			)

			req := &pb.IssueCommandRequest{
				NodeId:      nodeID,
				CommandType: pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON,
			}

			resp, err := client.IssueCommand(context.Background(), connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("failed to cordon node: %w", err)
			}

			fmt.Printf("Node %s cordoned successfully\n", nodeID)
			fmt.Printf("Command ID: %s\n", resp.Msg.CommandId)

			return nil
		},
	}

	return cmd
}
