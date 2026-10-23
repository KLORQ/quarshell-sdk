package klorq

import (
	"context"
	"fmt"
	"strings"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gatewayclient "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
)

// ListSandboxesPage lists one page from the pinned OpenShell Gateway RPC.
// Zero pageSize selects the server default; negative values are rejected.
func (c *Client) ListSandboxesPage(ctx context.Context, pageSize int32, pageToken, labelSelector string, allWorkspaces bool) ([]Sandbox, string, error) {
	if pageSize < 0 {
		return nil, "", fmt.Errorf("sandbox page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return nil, "", err
	}
	scope := workspaceSelector(c.workspace())
	if allWorkspaces {
		scope = &datamodelv1.WorkspaceSelector{Selection: &datamodelv1.WorkspaceSelector_AllWorkspaces{AllWorkspaces: &datamodelv1.AllWorkspaces{}}}
	}
	response, err := client.ListSandboxes(ctx, &gatewayv1.ListSandboxesRequest{
		WorkspaceScope: scope, PageSize: pageSize, PageToken: pageToken, LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list sandboxes: %w", err)
	}
	items := make([]Sandbox, 0, len(response.GetSandboxes()))
	for _, item := range response.GetSandboxes() {
		if item != nil {
			items = append(items, sandboxFromOpenShell(item))
		}
	}
	return items, response.GetNextPageToken(), nil
}

func sandboxFromOpenShell(item *gatewayv1.Sandbox) Sandbox {
	if item == nil {
		return Sandbox{}
	}
	metadata, spec, status := item.GetMetadata(), item.GetSpec(), item.GetStatus()
	result := Sandbox{}
	if metadata != nil {
		result.Name = metadata.GetName()
		result.ID = metadata.GetId()
		result.Workspace = metadata.GetWorkspace()
		result.ResourceVersion = metadata.GetResourceVersion()
		result.Labels = gatewayclient.Labels(metadata.GetLabels())
	}
	if spec != nil {
		result.TTY = spec.GetTty()
		result.Environment = spec.GetEnvironment()
		result.AttachedProviders = append([]string(nil), spec.GetProviders()...)
		if template := spec.GetTemplate(); template != nil {
			result.Image = template.GetImage()
		}
	}
	if status != nil && status.GetPhase() != gatewayv1.SandboxPhase_SANDBOX_PHASE_UNSPECIFIED {
		result.Status = strings.TrimPrefix(strings.ToLower(status.GetPhase().String()), "sandbox_phase_")
	}
	return result
}
