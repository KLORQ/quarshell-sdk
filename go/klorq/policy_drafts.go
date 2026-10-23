package klorq

import (
	"context"
	"fmt"
	"strings"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// GetDraftPolicy returns OpenShell's native draft chunks for a sandbox.
func (c *Client) GetDraftPolicy(ctx context.Context, sandbox, statusFilter string) (*gatewayv1.GetDraftPolicyResponse, error) {
	if strings.TrimSpace(sandbox) == "" {
		return nil, fmt.Errorf("sandbox name must not be empty")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetDraftPolicy(ctx, &gatewayv1.GetDraftPolicyRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, StatusFilter: statusFilter,
	})
	if err != nil {
		return nil, fmt.Errorf("get draft policy for sandbox %q: %w", sandbox, err)
	}
	return response, nil
}

// ApproveDraftChunk approves an exact OpenShell chunk using its review token.
func (c *Client) ApproveDraftChunk(ctx context.Context, sandbox, chunkID, reviewToken string) (*gatewayv1.ApproveDraftChunkResponse, error) {
	if strings.TrimSpace(sandbox) == "" || strings.TrimSpace(chunkID) == "" || strings.TrimSpace(reviewToken) == "" {
		return nil, fmt.Errorf("sandbox, chunk ID, and review token are required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return nil, err
	}
	response, err := client.ApproveDraftChunk(ctx, &gatewayv1.ApproveDraftChunkRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, ChunkId: chunkID,
		ReviewToken: reviewToken, RequestId: requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("approve draft policy chunk %q: %w", chunkID, err)
	}
	return response, nil
}

// RejectDraftChunk rejects an OpenShell chunk with an optional reviewer note.
func (c *Client) RejectDraftChunk(ctx context.Context, sandbox, chunkID, reason string) (*gatewayv1.RejectDraftChunkResponse, error) {
	if strings.TrimSpace(sandbox) == "" || strings.TrimSpace(chunkID) == "" {
		return nil, fmt.Errorf("sandbox and chunk ID are required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return nil, err
	}
	response, err := client.RejectDraftChunk(ctx, &gatewayv1.RejectDraftChunkRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, ChunkId: chunkID,
		Reason: reason, RequestId: requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("reject draft policy chunk %q: %w", chunkID, err)
	}
	return response, nil
}
