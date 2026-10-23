package klorq

import (
	"context"
	"encoding/json"
	"fmt"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func (c *Client) gatewayInfo(ctx context.Context) (map[string]any, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetGatewayInfo(ctx, &gatewayv1.GetGatewayInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("get OpenShell gateway info: %w", err)
	}
	drivers := make([]any, 0, len(response.GetComputeDrivers()))
	for _, driver := range response.GetComputeDrivers() {
		capabilities, err := protoMap(driver.GetCapabilities())
		if err != nil {
			return nil, err
		}
		drivers = append(drivers, map[string]any{
			"name":         driver.GetName(),
			"capabilities": capabilities,
		})
	}
	extensions := make([]any, 0, len(response.GetExtensions()))
	for _, extension := range response.GetExtensions() {
		value, err := protoMap(extension)
		if err != nil {
			return nil, err
		}
		extensions = append(extensions, value)
	}
	return map[string]any{
		"status":          response.GetStatus().String(),
		"gateway_version": response.GetGatewayVersion(),
		"compute_drivers": drivers,
		"extensions":      extensions,
	}, nil
}

func protoMap(message proto.Message) (map[string]any, error) {
	encoded, err := protojson.Marshal(message)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}
