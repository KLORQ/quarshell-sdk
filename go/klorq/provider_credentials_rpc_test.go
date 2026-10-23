package klorq

import (
	"context"
	"net"
	"testing"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type providerUpdateTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	request *gatewayv1.UpdateProviderRequest
}

func (s *providerUpdateTestServer) UpdateProvider(_ context.Context, request *gatewayv1.UpdateProviderRequest) (*gatewayv1.ProviderResponse, error) {
	s.request = request
	return &gatewayv1.ProviderResponse{Provider: request.GetProvider()}, nil
}

func TestUpdateProviderCredentialsUsesNativeOpenShellPatch(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &providerUpdateTestServer{}
	gatewayv1.RegisterOpenShellServer(server, backend)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := &Client{gatewayConn: connection, Workspace: "team"}

	if err := client.UpdateProviderCredentials(context.Background(), "", "github", map[string]string{"GITHUB_TOKEN": "secret"}); err != nil {
		t.Fatal(err)
	}
	request := backend.request
	if request == nil || request.GetWorkspaceScope().GetWorkspace() != "team" || request.GetProvider().GetMetadata().GetName() != "github" || request.GetProvider().GetCredentials()["GITHUB_TOKEN"] != "secret" {
		t.Fatalf("unexpected native provider update request: %+v", request)
	}
	if len(request.GetProvider().GetConfig()) != 0 || request.GetProvider().GetType() != "" {
		t.Fatalf("credential patch replaced unrelated provider fields: %+v", request.GetProvider())
	}
}
