package klorq

import (
	"context"
	"fmt"
	"sort"

	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
)

// ListServices lists services visible in the selected workspace through native OpenShell RPCs.
// Backend addresses are intentionally omitted from the public response.
func (c *Client) ListServices(ctx context.Context) ([]gc.ServiceRecord, error) {
	result, err := c.ListServicesAllPages(ctx, "", false)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// ListTemplates lists native OpenShell workload templates in the selected workspace.
func (c *Client) ListTemplates(ctx context.Context) ([]WorkloadTemplate, error) {
	result, err := c.ListSandboxTemplates(ctx, c.workspace())
	if err != nil {
		return nil, fmt.Errorf("list OpenShell workload templates: %w", err)
	}
	return result, nil
}
