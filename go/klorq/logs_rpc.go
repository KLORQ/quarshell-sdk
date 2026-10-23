package klorq

import (
	"context"
	"fmt"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PostLogs publishes client-collected observations through native OpenShell.
func (c *Client) PostLogs(ctx context.Context, sandbox string, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	current, err := c.GetControlSandbox(ctx, c.workspace(), sandbox)
	if err != nil {
		return fmt.Errorf("resolve OpenShell sandbox for log push: %w", err)
	}
	if current.ID == "" {
		return fmt.Errorf("resolve OpenShell sandbox for log push: sandbox ID is empty")
	}
	return c.postSandboxLogs(ctx, current.ID, lines)
}

// PostSandboxLogs publishes observations when the caller already holds the
// Gateway-issued sandbox ID, as a supervisor does with its Sandbox bearer.
func (c *Client) PostSandboxLogs(ctx context.Context, sandboxID string, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	if sandboxID == "" {
		return fmt.Errorf("sandbox ID is required")
	}
	return c.postSandboxLogs(ctx, sandboxID, lines)
}

func (c *Client) postSandboxLogs(ctx context.Context, sandboxID string, lines []LogLine) error {
	items := make([]*gatewayv1.SandboxLogLine, 0, len(lines))
	for _, line := range lines {
		ts := line.TS
		if ts.IsZero() {
			ts = time.Now()
		}
		items = append(items, &gatewayv1.SandboxLogLine{
			SandboxId: sandboxID, EventTime: timestamppb.New(ts),
			Source: line.Source, Level: line.Level, Message: line.Text,
		})
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	stream, err := client.PushSandboxLogs(ctx)
	if err != nil {
		return fmt.Errorf("open OpenShell sandbox log push: %w", err)
	}
	if err := stream.Send(&gatewayv1.PushSandboxLogsRequest{SandboxId: sandboxID, Logs: items}); err != nil {
		return fmt.Errorf("send OpenShell sandbox logs: %w", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return fmt.Errorf("publish OpenShell sandbox logs: %w", err)
	}
	return nil
}
