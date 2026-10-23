package klorq

import (
	"context"
	"fmt"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
)

const (
	// ServiceAuthorizationModeUnspecified is the wire name for the omitted mode.
	ServiceAuthorizationModeUnspecified = "SERVICE_AUTHORIZATION_MODE_UNSPECIFIED"
	// ServiceAuthorizationModeStrip removes Authorization before proxying.
	ServiceAuthorizationModeStrip = "SERVICE_AUTHORIZATION_MODE_STRIP"
	// ServiceAuthorizationModeBearerPassthrough forwards one valid application
	// bearer Authorization header to the exposed sandbox service.
	ServiceAuthorizationModeBearerPassthrough = "SERVICE_AUTHORIZATION_MODE_BEARER_PASSTHROUGH"
)

// ExposeService registers a sandbox service through the pinned OpenShell RPC.
func (c *Client) ExposeService(ctx context.Context, sandbox, name string, port uint32) (gc.ServiceRecord, error) {
	return c.ExposeServiceWithDomain(ctx, sandbox, name, port, true)
}

// ExposeServiceWithDomain registers a sandbox service and controls whether the
// Gateway should provide a browser-facing service URL.
func (c *Client) ExposeServiceWithDomain(ctx context.Context, sandbox, name string, port uint32, domain bool) (gc.ServiceRecord, error) {
	return c.ExposeServiceWithOptions(ctx, sandbox, name, port, domain, ServiceAuthorizationModeStrip)
}

// ExposeServiceWithOptions registers a service with explicit browser URL and
// application Authorization forwarding behavior.
func (c *Client) ExposeServiceWithOptions(ctx context.Context, sandbox, name string, port uint32, domain bool, authorizationMode string) (gc.ServiceRecord, error) {
	mode, err := serviceAuthorizationMode(authorizationMode)
	if err != nil {
		return gc.ServiceRecord{}, err
	}
	client, err := c.gatewayClient()
	if err != nil {
		return gc.ServiceRecord{}, err
	}
	response, err := client.ExposeService(ctx, &gatewayv1.ExposeServiceRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Name: name, TargetPort: port, Domain: domain, AuthorizationMode: mode,
	})
	if err != nil {
		return gc.ServiceRecord{}, fmt.Errorf("expose service %q: %w", name, err)
	}
	return serviceRecord(response.GetEndpoint(), response.GetUrl()), nil
}

// GetService reads a service endpoint through the pinned OpenShell RPC.
func (c *Client) GetService(ctx context.Context, name string) (gc.ServiceRecord, error) {
	return c.GetSandboxService(ctx, "", name)
}

// GetSandboxService reads a named or unnamed endpoint for one sandbox.
func (c *Client) GetSandboxService(ctx context.Context, sandbox, name string) (gc.ServiceRecord, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return gc.ServiceRecord{}, err
	}
	response, err := client.GetService(ctx, &gatewayv1.GetServiceRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Name: name,
	})
	if err != nil {
		return gc.ServiceRecord{}, fmt.Errorf("get service %q: %w", name, err)
	}
	return serviceRecord(response.GetEndpoint(), response.GetUrl()), nil
}

// PutService exposes a service over OpenShell RPC. Custom host routing is not
// accepted because OpenShell routes the endpoint through the sandbox relay.
// Browser-facing URL registration remains enabled for compatibility.
func (c *Client) PutService(ctx context.Context, record gc.ServiceRecord) (gc.ServiceRecord, error) {
	return c.PutServiceWithDomain(ctx, record, true)
}

// PutServiceWithDomain exposes a service with an explicit browser-facing URL
// setting while keeping unsupported backend and authorization modes rejected.
func (c *Client) PutServiceWithDomain(ctx context.Context, record gc.ServiceRecord, domain bool) (gc.ServiceRecord, error) {
	return c.PutServiceWithOptions(ctx, record, domain, record.AuthorizationMode)
}

// PutServiceWithOptions exposes a service with explicit URL and authorization settings.
func (c *Client) PutServiceWithOptions(ctx context.Context, record gc.ServiceRecord, domain bool, authorizationMode string) (gc.ServiceRecord, error) {
	if record.BackendHost != "" || (record.BackendPort != 0 && record.BackendPort != record.Port) {
		return gc.ServiceRecord{}, fmt.Errorf("custom service backends are not supported; use a sandbox target port")
	}
	if record.Port <= 0 || record.Port > 65535 {
		return gc.ServiceRecord{}, fmt.Errorf("service target port must be between 1 and 65535")
	}
	mode, err := serviceAuthorizationMode(authorizationMode)
	if err != nil {
		return gc.ServiceRecord{}, err
	}
	return c.ExposeServiceWithOptions(ctx, record.Sandbox, record.Name, uint32(record.Port), domain, mode.String())
}

func serviceAuthorizationMode(value string) (gatewayv1.ServiceAuthorizationMode, error) {
	switch value {
	case "", ServiceAuthorizationModeUnspecified, ServiceAuthorizationModeStrip:
		return gatewayv1.ServiceAuthorizationMode_SERVICE_AUTHORIZATION_MODE_STRIP, nil
	case ServiceAuthorizationModeBearerPassthrough:
		return gatewayv1.ServiceAuthorizationMode_SERVICE_AUTHORIZATION_MODE_BEARER_PASSTHROUGH, nil
	default:
		return 0, fmt.Errorf("unsupported service authorization mode %q", value)
	}
}

// DeleteService removes a service endpoint through the pinned OpenShell RPC.
func (c *Client) DeleteService(ctx context.Context, name string) error {
	return c.DeleteSandboxService(ctx, "", name)
}

// DeleteSandboxService removes a named or unnamed endpoint from one sandbox.
func (c *Client) DeleteSandboxService(ctx context.Context, sandbox, name string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.DeleteService(ctx, &gatewayv1.DeleteServiceRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Name: name,
	}); err != nil {
		return fmt.Errorf("delete service %q: %w", name, err)
	}
	return nil
}

func serviceRecord(endpoint *gatewayv1.ServiceEndpoint, url string) gc.ServiceRecord {
	if endpoint == nil {
		return gc.ServiceRecord{URL: url}
	}
	return gc.ServiceRecord{
		ID: endpoint.GetMetadata().GetId(), SandboxID: endpoint.GetSandboxId(),
		Workspace: endpoint.GetMetadata().GetWorkspace(), Name: endpoint.GetName(),
		Sandbox: endpoint.GetSandbox(), Port: int(endpoint.GetTargetPort()), URL: url,
		Domain: endpoint.GetDomain(), AuthorizationMode: endpoint.GetAuthorizationMode().String(),
		BackendPort: int(endpoint.GetTargetPort()),
	}
}

// ListServicesPage lists one Gateway page of service endpoints. A negative page
// size is rejected locally; zero selects the server default and larger values
// are capped by the pinned Gateway contract.
func (c *Client) ListServicesPage(ctx context.Context, sandbox string, pageSize int32, pageToken string, allWorkspaces bool) ([]gc.ServiceRecord, string, error) {
	if pageSize < 0 {
		return nil, "", fmt.Errorf("service page size must not be negative")
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
	response, err := client.ListServices(ctx, &gatewayv1.ListServicesRequest{
		WorkspaceScope: scope, Sandbox: sandbox, PageSize: pageSize, PageToken: pageToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list services: %w", err)
	}
	services := make([]gc.ServiceRecord, 0, len(response.GetServices()))
	for _, item := range response.GetServices() {
		if item != nil {
			services = append(services, serviceRecord(item.GetEndpoint(), item.GetUrl()))
		}
	}
	return services, response.GetNextPageToken(), nil
}

// ListServicesAllPages collects service endpoint pages in the selected scope.
// Set allWorkspaces only for callers authorized as platform administrators.
func (c *Client) ListServicesAllPages(ctx context.Context, sandbox string, allWorkspaces bool) ([]ServiceRecord, error) {
	result := make([]ServiceRecord, 0)
	seenTokens := map[string]struct{}{}
	seenServices := map[string]struct{}{}
	pageToken := ""
	for {
		items, nextToken, err := c.ListServicesPage(ctx, sandbox, 1000, pageToken, allWorkspaces)
		if err != nil {
			return nil, err
		}
		result, err = appendUniquePageItems(result, seenServices, items, func(item ServiceRecord) string {
			if item.ID != "" {
				return item.ID
			}
			return item.Sandbox + "\x00" + item.Name
		}, "service endpoint")
		if err != nil {
			return nil, err
		}
		if nextToken == "" {
			return result, nil
		}
		if _, seen := seenTokens[nextToken]; seen {
			return nil, fmt.Errorf("list service endpoints: gateway repeated a pagination token")
		}
		seenTokens[nextToken] = struct{}{}
		pageToken = nextToken
	}
}
