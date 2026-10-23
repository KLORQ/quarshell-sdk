package klorq

import (
	"context"
	"fmt"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// ControlOverview composes a caller-visible summary from native OpenShell RPCs.
func (c *Client) ControlOverview(ctx context.Context) (map[string]any, error) {
	workspace := c.workspace()
	viewer, err := c.Whoami(ctx)
	if err != nil {
		return nil, err
	}
	info, err := c.gatewayInfo(ctx)
	if err != nil {
		return nil, err
	}
	sandboxes, err := c.ListControlSandboxes(ctx, workspace, false)
	if err != nil {
		return nil, fmt.Errorf("list OpenShell sandboxes for overview: %w", err)
	}
	running := 0
	for _, sandbox := range sandboxes {
		if sandbox.Status == "ready" || sandbox.Status == "running" {
			running++
		}
	}
	drivers := make([]string, 0)
	if entries, ok := info["compute_drivers"].([]any); ok {
		for _, entry := range entries {
			if driver, ok := entry.(map[string]any); ok {
				if name, ok := driver["name"].(string); ok {
					drivers = append(drivers, name)
				}
			}
		}
	}
	return map[string]any{
		"gateway_version":             info["gateway_version"],
		"workspace":                   workspace,
		"sandbox_count":               len(sandboxes),
		"registry_running_count":      running,
		"auth_mode":                   viewer["identity_provider"],
		"roles":                       viewer["roles"],
		"scopes":                      viewer["scopes"],
		"compute_drivers":             drivers,
		"sandbox_lifecycle_available": true,
	}, nil
}

// Whoami returns the authenticated viewer through native OpenShell RPCs.
func (c *Client) Whoami(ctx context.Context) (map[string]any, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetCurrentUser(ctx, &gatewayv1.GetCurrentUserRequest{})
	if err != nil {
		return nil, fmt.Errorf("get OpenShell current user: %w", err)
	}
	return map[string]any{
		"auth":              "authenticated",
		"subject":           response.GetSubject(),
		"roles":             append([]string(nil), response.GetRoles()...),
		"scopes":            append([]string(nil), response.GetScopes()...),
		"identity_provider": response.GetIdentityProvider(),
	}, nil
}
