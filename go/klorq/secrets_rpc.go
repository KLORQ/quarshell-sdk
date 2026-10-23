package klorq

import (
	"context"
	"fmt"

	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// ResolveSecrets resolves environment credentials for the matching sandbox
// principal. The gateway validates the sandbox token and returns only the
// environment the sandbox is allowed to receive.
func (c *Client) ResolveSecrets(ctx context.Context, sandbox string) (map[string]string, error) {
	if sandbox == "" {
		return nil, fmt.Errorf("sandbox name required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetSandboxProviderEnvironment(ctx, &compatv1.GetSandboxProviderEnvironmentRequest{SandboxId: sandbox})
	if err != nil {
		return nil, fmt.Errorf("resolve sandbox credentials: %w", err)
	}
	return response.GetEnvironment(), nil
}

// ResolveProxySecrets resolves static credentials for the authenticated sandbox
// proxy. The proxy supports endpoint binding enforcement, while stable
// placeholder handles remain disabled until their lifecycle contract is
// implemented.
func (c *Client) ResolveProxySecrets(ctx context.Context, sandbox string) (map[string]string, error) {
	if sandbox == "" {
		return nil, fmt.Errorf("sandbox name required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetSandboxProviderEnvironment(ctx, &compatv1.GetSandboxProviderEnvironmentRequest{
		SandboxId:                                sandbox,
		SupportsStaticCredentialBindings:         true,
		SupportsStablePlaceholderEnvironmentKeys: false,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve proxy credentials: %w", err)
	}
	return response.GetEnvironment(), nil
}
