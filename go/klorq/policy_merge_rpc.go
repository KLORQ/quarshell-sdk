package klorq

import (
	"context"
	"fmt"
	"strings"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// UpdateSandboxPolicyMerge applies an atomic batch of incremental policy
// operations to one sandbox in the client's selected workspace.
func (c *Client) UpdateSandboxPolicyMerge(ctx context.Context, sandbox string, operations []*gatewayv1.PolicyMergeOperation) (*gatewayv1.UpdateConfigResponse, error) {
	sandbox = strings.TrimSpace(sandbox)
	if sandbox == "" {
		return nil, fmt.Errorf("policy merge: sandbox is required")
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf("policy merge: at least one operation is required")
	}
	for i, operation := range operations {
		if operation == nil || operation.GetOperation() == nil {
			return nil, fmt.Errorf("policy merge: operation %d is empty", i)
		}
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	current, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{
		Name: sandbox, WorkspaceScope: workspaceSelector(c.workspace()),
	})
	if err != nil {
		return nil, fmt.Errorf("read sandbox %q before policy merge: %w", sandbox, err)
	}
	if current.GetSandbox() == nil || current.GetSandbox().GetMetadata() == nil {
		return nil, fmt.Errorf("read sandbox %q before policy merge: gateway returned no sandbox metadata", sandbox)
	}
	response, err := client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		WorkspaceScope:          workspaceSelector(c.workspace()),
		Sandbox:                 sandbox,
		MergeOperations:         operations,
		ExpectedResourceVersion: current.GetSandbox().GetMetadata().GetResourceVersion(),
	})
	if err != nil {
		return nil, fmt.Errorf("update sandbox policy %q with merge operations: %w", sandbox, err)
	}
	return response, nil
}
