package klorq

import (
	"context"
	"net"
	"testing"
	"time"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workspaceLabelsTestServer struct {
	compatv1.UnimplementedOpenShellServer
	request     chan *compatv1.CreateWorkspaceRequest
	listRequest chan *compatv1.ListWorkspacesRequest
}

func (s *workspaceLabelsTestServer) CreateWorkspace(_ context.Context, request *compatv1.CreateWorkspaceRequest) (*compatv1.CreateWorkspaceResponse, error) {
	s.request <- request
	return &compatv1.CreateWorkspaceResponse{Workspace: &datamodelv1.Workspace{
		Metadata: &datamodelv1.ObjectMeta{Name: request.GetName(), Labels: request.GetLabels()},
	}}, nil
}

func (s *workspaceLabelsTestServer) ListWorkspaces(_ context.Context, request *compatv1.ListWorkspacesRequest) (*compatv1.ListWorkspacesResponse, error) {
	if s.listRequest != nil {
		s.listRequest <- request
	}
	return &compatv1.ListWorkspacesResponse{
		Workspaces: []*datamodelv1.Workspace{{Metadata: &datamodelv1.ObjectMeta{
			Id: "ws-123", Name: "team-a", ResourceVersion: 42, Labels: map[string]string{"env": "test"},
			CreatedTime: timestamppb.New(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)),
		}, Status: &datamodelv1.WorkspaceStatus{Phase: datamodelv1.WorkspacePhase_WORKSPACE_PHASE_ACTIVE}}},
		NextPageToken: "cursor-2",
	}, nil
}

func TestCreateWorkspaceWithLabelsForwardsCompatibilityRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	service := &workspaceLabelsTestServer{request: make(chan *compatv1.CreateWorkspaceRequest, 1)}
	compatv1.RegisterOpenShellServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	client := New("http://" + listener.Addr().String())
	t.Cleanup(func() { _ = client.Close() })
	wantLabels := map[string]string{"env": "test", "owner": "agents"}
	created, err := client.CreateWorkspaceWithLabels(context.Background(), "team-a", wantLabels)
	if err != nil {
		t.Fatal(err)
	}
	request := <-service.request
	if request.GetName() != "team-a" || len(request.GetLabels()) != len(wantLabels) {
		t.Fatalf("CreateWorkspaceRequest = %+v", request)
	}
	for key, value := range wantLabels {
		if request.GetLabels()[key] != value || created.Labels[key] != value {
			t.Errorf("label %q request=%q result=%q, want %q", key, request.GetLabels()[key], created.Labels[key], value)
		}
	}
}

func TestListWorkspacesPageForwardsPaginationAndSelector(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	service := &workspaceLabelsTestServer{listRequest: make(chan *compatv1.ListWorkspacesRequest, 1)}
	compatv1.RegisterOpenShellServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	client := New("http://" + listener.Addr().String())
	t.Cleanup(func() { _ = client.Close() })
	page, err := client.ListWorkspacesPage(context.Background(), WorkspaceListOptions{
		PageSize: 7, PageToken: "cursor-1", LabelSelector: "env=test",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := <-service.listRequest
	if request.GetPageSize() != 7 || request.GetPageToken() != "cursor-1" || request.GetLabelSelector() != "env=test" {
		t.Fatalf("ListWorkspacesRequest = %+v", request)
	}
	if page.NextPageToken != "cursor-2" || len(page.Workspaces) != 1 || page.Workspaces[0].ID != "ws-123" || page.Workspaces[0].Name != "team-a" || page.Workspaces[0].ResourceVersion != 42 || page.Workspaces[0].Labels["env"] != "test" || page.Workspaces[0].Status != "Active" || page.Workspaces[0].CreatedAt != "2026-10-16T00:00:00Z" {
		t.Fatalf("workspace page = %+v", page)
	}
}
