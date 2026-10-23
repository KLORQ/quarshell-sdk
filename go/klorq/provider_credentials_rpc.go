package klorq

import (
	"context"
	"fmt"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// UpdateProviderCredentials patches credential values without reading or
// replacing existing secrets.
func (c *Client) UpdateProviderCredentials(ctx context.Context, workspace, name string, credentials map[string]string) error {
	if workspace == "" {
		workspace = c.workspace()
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	_, err = client.UpdateProvider(ctx, &gatewayv1.UpdateProviderRequest{
		WorkspaceScope: workspaceSelector(workspace),
		Provider: &datamodelv1.Provider{
			Metadata:    &datamodelv1.ObjectMeta{Name: name},
			Credentials: credentials,
		},
	})
	if err != nil {
		return fmt.Errorf("update provider credentials for %q through OpenShell: %w", name, err)
	}
	return nil
}
