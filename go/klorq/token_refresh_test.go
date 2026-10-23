package klorq

import (
	"context"
	"testing"
)

func TestGatewayCredentialsReadRotatedToken(t *testing.T) {
	client := NewWithToken("http://127.0.0.1", "initial")
	credentials := gatewayCredentials{token: client.authToken}
	metadata, err := credentials.GetRequestMetadata(context.Background())
	if err != nil || metadata["authorization"] != "Bearer initial" {
		t.Fatalf("initial authorization = %v, %v", metadata, err)
	}
	client.SetToken("rotated")
	metadata, err = credentials.GetRequestMetadata(context.Background())
	if err != nil || metadata["authorization"] != "Bearer rotated" {
		t.Fatalf("rotated authorization = %v, %v", metadata, err)
	}
}
