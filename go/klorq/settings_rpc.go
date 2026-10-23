package klorq

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	sandboxv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/sandbox/v1"
)

// GetSetting reads a registered gateway setting through OpenShell RPC.
func (c *Client) GetSetting(ctx context.Context, key string) (string, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return "", err
	}
	config, err := client.GetGatewayConfig(ctx, &sandboxv1.GetGatewayConfigRequest{})
	if err != nil {
		return "", fmt.Errorf("get gateway settings: %w", err)
	}
	value, ok := config.Settings[key]
	if !ok || value == nil || value.GetValue() == nil {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return settingString(value), nil
}

// ListSettings lists configured gateway settings through OpenShell RPC.
func (c *Client) ListSettings(ctx context.Context) (map[string]string, error) {
	config, err := c.GetGatewaySettings(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(config.GetSettings()))
	for key, value := range config.GetSettings() {
		if value != nil && value.GetValue() != nil {
			result[key] = settingString(value)
		}
	}
	return result, nil
}

// GetGatewaySettings returns the native global settings and revision.
func (c *Client) GetGatewaySettings(ctx context.Context) (*sandboxv1.GetGatewayConfigResponse, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	config, err := client.GetGatewayConfig(ctx, &sandboxv1.GetGatewayConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("list gateway settings: %w", err)
	}
	return config, nil
}

// GetSandboxSettings returns the native effective configuration for a sandbox.
func (c *Client) GetSandboxSettings(ctx context.Context, workspace, sandbox string) (*sandboxv1.GetSandboxConfigResponse, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	config, err := client.GetSandboxConfig(ctx, &sandboxv1.GetSandboxConfigRequest{
		Name: sandbox, WorkspaceScope: workspaceSelector(workspace),
	})
	if err != nil {
		return nil, fmt.Errorf("get sandbox settings %q: %w", sandbox, err)
	}
	return config, nil
}

// DeleteGatewaySetting removes one setting from the OpenShell global config.
func (c *Client) DeleteGatewaySetting(ctx context.Context, key string) (*gatewayv1.UpdateConfigResponse, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{Global: true, SettingKey: key, DeleteSetting: true})
	if err != nil {
		return nil, fmt.Errorf("delete gateway setting %q: %w", key, err)
	}
	return response, nil
}

// DeleteSandboxSetting requests removal at sandbox scope using OpenShell's config RPC.
func (c *Client) DeleteSandboxSetting(ctx context.Context, workspace, sandbox, key string) (*gatewayv1.UpdateConfigResponse, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		WorkspaceScope: workspaceSelector(workspace), Sandbox: sandbox, SettingKey: key, DeleteSetting: true,
	})
	if err != nil {
		return nil, fmt.Errorf("delete sandbox %q setting %q: %w", sandbox, key, err)
	}
	return response, nil
}

// PutSetting updates a gateway-global setting through OpenShell RPC.
func (c *Client) PutSetting(ctx context.Context, key, value string) error {
	return c.PutGatewaySettingValue(ctx, key, &sandboxv1.SettingValue{Value: &sandboxv1.SettingValue_StringValue{StringValue: value}})
}

// PutGatewaySettingValue updates a typed OpenShell gateway-global setting.
func (c *Client) PutGatewaySettingValue(ctx context.Context, key string, value *sandboxv1.SettingValue) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	_, err = client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		Global: true, SettingKey: key,
		SettingValue: value,
	})
	if err != nil {
		return fmt.Errorf("update gateway setting %q: %w", key, err)
	}
	return nil
}

// PutSandboxSetting updates one OpenShell setting at sandbox scope using the
// current resource version as an optimistic concurrency guard.
func (c *Client) PutSandboxSetting(ctx context.Context, workspace, sandbox, key, value string) error {
	return c.PutSandboxSettingValue(ctx, workspace, sandbox, key, &sandboxv1.SettingValue{Value: &sandboxv1.SettingValue_StringValue{StringValue: value}})
}

// PutSandboxSettingValue updates one typed OpenShell setting at sandbox scope.
func (c *Client) PutSandboxSettingValue(ctx context.Context, workspace, sandbox, key string, value *sandboxv1.SettingValue) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	selector := workspaceSelector(workspace)
	current, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{WorkspaceScope: selector, Name: sandbox})
	if err != nil {
		return fmt.Errorf("read sandbox %q before setting %q: %w", sandbox, key, err)
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return err
	}
	_, err = client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		WorkspaceScope: selector, Sandbox: sandbox, SettingKey: key,
		SettingValue:            value,
		ExpectedResourceVersion: current.GetSandbox().GetMetadata().GetResourceVersion(), RequestId: requestID,
	})
	if err != nil {
		return fmt.Errorf("update sandbox %q setting %q: %w", sandbox, key, err)
	}
	return nil
}

func settingString(value *sandboxv1.SettingValue) string {
	if value == nil {
		return ""
	}
	switch item := value.GetValue().(type) {
	case *sandboxv1.SettingValue_StringValue:
		return item.StringValue
	case *sandboxv1.SettingValue_BoolValue:
		return strconv.FormatBool(item.BoolValue)
	case *sandboxv1.SettingValue_IntValue:
		return strconv.FormatInt(item.IntValue, 10)
	case *sandboxv1.SettingValue_BytesValue:
		return base64.StdEncoding.EncodeToString(item.BytesValue)
	default:
		return ""
	}
}
