package klorq

import (
	"context"
	"fmt"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
)

// WorkspaceListOptions selects one page of workspaces.
type WorkspaceListOptions struct {
	PageSize      int
	PageToken     string
	LabelSelector string
}

// WorkspaceListPage contains one page and its opaque continuation token.
type WorkspaceListPage struct {
	Workspaces    []gc.WorkspaceRecord `json:"workspaces"`
	NextPageToken string               `json:"next_page_token,omitempty"`
}

// ListWorkspacesPage lists one compatibility-contract workspace page.
func (c *Client) ListWorkspacesPage(ctx context.Context, options WorkspaceListOptions) (WorkspaceListPage, error) {
	if options.PageSize < 0 {
		return WorkspaceListPage{}, fmt.Errorf("list workspace page: page size must not be negative")
	}
	pageSize := options.PageSize
	if pageSize > 1000 {
		pageSize = 1000
	}
	client, err := c.gatewayClient()
	if err != nil {
		return WorkspaceListPage{}, err
	}
	page, err := client.ListWorkspaces(ctx, &gatewayv1.ListWorkspacesRequest{
		PageSize: int32(pageSize), PageToken: options.PageToken, LabelSelector: options.LabelSelector,
	})
	if err != nil {
		return WorkspaceListPage{}, fmt.Errorf("list workspace page: %w", err)
	}
	if page == nil {
		return WorkspaceListPage{}, fmt.Errorf("list workspace page: gateway returned an empty response")
	}
	result := WorkspaceListPage{Workspaces: make([]gc.WorkspaceRecord, 0)}
	result.NextPageToken = page.NextPageToken
	for _, workspace := range page.Workspaces {
		if workspace == nil {
			continue
		}
		metadata := workspace.GetMetadata()
		if metadata == nil {
			metadata = &datamodelv1.ObjectMeta{}
		}
		record := gc.WorkspaceRecord{
			ID: metadata.GetId(), Name: metadata.GetName(), ResourceVersion: metadata.GetResourceVersion(),
			Labels: cloneStringMap(metadata.GetLabels()), Status: workspacePhaseString(workspace),
		}
		if created := metadata.GetCreatedTime(); created != nil {
			record.CreatedAt = created.AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		result.Workspaces = append(result.Workspaces, record)
	}
	return result, nil
}

func workspacePhaseString(workspace *datamodelv1.Workspace) string {
	if workspace == nil || workspace.GetStatus() == nil {
		return "Unknown"
	}
	switch workspace.GetStatus().GetPhase() {
	case datamodelv1.WorkspacePhase_WORKSPACE_PHASE_ACTIVE:
		return "Active"
	case datamodelv1.WorkspacePhase_WORKSPACE_PHASE_TERMINATING:
		return "Terminating"
	default:
		return "Unknown"
	}
}
