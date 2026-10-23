package klorq

import (
	"context"
	"fmt"
	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

func (c *Client) ListControlSandboxes(ctx context.Context, workspace string, allWorkspaces bool) ([]Sandbox, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	scope := workspaceSelector(workspace)
	if allWorkspaces {
		scope = allWorkspacesSelector()
	}
	var result []Sandbox
	seen := map[string]struct{}{}
	pageToken := ""
	for {
		response, err := client.ListSandboxes(ctx, &gatewayv1.ListSandboxesRequest{WorkspaceScope: scope, PageSize: 1000, PageToken: pageToken})
		if err != nil {
			return nil, fmt.Errorf("list sandboxes: %w", err)
		}
		items := make([]Sandbox, 0, len(response.GetSandboxes()))
		for _, item := range response.GetSandboxes() {
			items = append(items, sandboxFromOpenShell(item))
		}
		result, err = appendUniquePageItems(result, seen, items, func(item Sandbox) string {
			identity := item.ID
			if identity == "" {
				identity = item.Name
			}
			return item.Workspace + "\x00" + identity
		}, "sandbox")
		if err != nil {
			return nil, err
		}
		pageToken = response.GetNextPageToken()
		if pageToken == "" {
			return result, nil
		}
	}
}

func allWorkspacesSelector() *datamodelv1.WorkspaceSelector {
	return &datamodelv1.WorkspaceSelector{Selection: &datamodelv1.WorkspaceSelector_AllWorkspaces{AllWorkspaces: &datamodelv1.AllWorkspaces{}}}
}

// GetControlSandbox fetches one sandbox through the pinned OpenShell Gateway API.
func (c *Client) GetControlSandbox(ctx context.Context, workspace, name string) (Sandbox, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return Sandbox{}, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{
		WorkspaceScope: workspaceSelector(workspace), Name: name,
	})
	if err != nil {
		return Sandbox{}, fmt.Errorf("get sandbox %q: %w", name, err)
	}
	return sandboxFromOpenShell(response.GetSandbox()), nil
}

// GetControlSandboxLogs returns a bounded tail of gateway-buffered logs.
