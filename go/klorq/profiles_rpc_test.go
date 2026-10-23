package klorq

import (
	"context"
	"net"
	"strings"
	"testing"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type profileTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	getRequest    *gatewayv1.GetProviderProfileRequest
	importRequest *gatewayv1.ImportProviderProfilesRequest
	updateRequest *gatewayv1.UpdateProviderProfilesRequest
	deleteRequest *gatewayv1.DeleteProviderProfileRequest
}

func (s *profileTestServer) GetProviderProfile(_ context.Context, request *gatewayv1.GetProviderProfileRequest) (*gatewayv1.ProviderProfileResponse, error) {
	s.getRequest = request
	return &gatewayv1.ProviderProfileResponse{Profile: &gatewayv1.ProviderProfile{Id: request.GetId(), Category: gatewayv1.ProviderProfileCategory_PROVIDER_PROFILE_CATEGORY_INFERENCE, Source: "user", ResourceVersion: 7}}, nil
}

func (s *profileTestServer) ImportProviderProfiles(_ context.Context, request *gatewayv1.ImportProviderProfilesRequest) (*gatewayv1.ImportProviderProfilesResponse, error) {
	s.importRequest = request
	return &gatewayv1.ImportProviderProfilesResponse{Imported: true}, nil
}

func (s *profileTestServer) UpdateProviderProfiles(_ context.Context, request *gatewayv1.UpdateProviderProfilesRequest) (*gatewayv1.UpdateProviderProfilesResponse, error) {
	s.updateRequest = request
	return &gatewayv1.UpdateProviderProfilesResponse{Updated: true}, nil
}

func (s *profileTestServer) DeleteProviderProfile(_ context.Context, request *gatewayv1.DeleteProviderProfileRequest) (*gatewayv1.DeleteProviderProfileResponse, error) {
	s.deleteRequest = request
	return &gatewayv1.DeleteProviderProfileResponse{}, nil
}

func newProfileTestClient(t *testing.T, serverImpl *profileTestServer) *Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	gatewayv1.RegisterOpenShellServer(server, serverImpl)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return &Client{gatewayConn: connection, Workspace: "team"}
}

func TestProviderProfileCRUDUsesNativeOpenShell(t *testing.T) {
	backend := &profileTestServer{}
	client := newProfileTestClient(t, backend)

	data, source, version, err := client.GetProfileScoped(context.Background(), "custom", "workspace", "team")
	if err != nil {
		t.Fatal(err)
	}
	if backend.getRequest.GetWorkspaceScope().GetWorkspace() != "team" || source != "user" || version != "7" || !strings.Contains(string(data), "category: inference") {
		t.Fatalf("unexpected native profile response: data=%s source=%q version=%q request=%+v", data, source, version, backend.getRequest)
	}

	profile := []byte("id: custom\ncategory: inference\ndisplay_name: Custom\n")
	if err := client.CreateProfileScoped(context.Background(), "custom", profile, "workspace", "team"); err != nil {
		t.Fatal(err)
	}
	if backend.importRequest.GetWorkspaceScope().GetWorkspace() != "team" || len(backend.importRequest.GetProfiles()) != 1 || backend.importRequest.GetProfiles()[0].GetProfile().GetCategory() != gatewayv1.ProviderProfileCategory_PROVIDER_PROFILE_CATEGORY_INFERENCE {
		t.Fatalf("unexpected native import request: %+v", backend.importRequest)
	}

	if err := client.PutProfileScoped(context.Background(), "custom", profile, "7", "workspace", "team"); err != nil {
		t.Fatal(err)
	}
	if backend.updateRequest.GetExpectedResourceVersion() != 7 || backend.updateRequest.GetWorkspaceScope().GetWorkspace() != "team" || backend.updateRequest.GetProfile().GetProfile().GetId() != "custom" {
		t.Fatalf("unexpected native update request: %+v", backend.updateRequest)
	}

	if err := client.DeleteProfileScoped(context.Background(), "custom", "workspace", "team"); err != nil {
		t.Fatal(err)
	}
	if backend.deleteRequest.GetWorkspaceScope().GetWorkspace() != "team" || backend.deleteRequest.GetId() != "custom" {
		t.Fatalf("unexpected native delete request: %+v", backend.deleteRequest)
	}

	if profileWorkspaceSelector("global", "") != nil || profileWorkspaceSelector("workspace", "team").GetWorkspace() != "team" {
		t.Fatal("profile scope does not match the OpenShell workspace selector contract")
	}
}
