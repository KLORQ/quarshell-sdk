package klorq

import (
	"context"
	"net"
	"testing"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type workloadTemplateTestServer struct {
	compatv1.UnimplementedOpenShellServer
	created *compatv1.SandboxWorkloadTemplate
	got     *compatv1.SandboxWorkloadTemplate
	listed  []*compatv1.SandboxWorkloadTemplate
	page    int
	deleted bool
}

func (s *workloadTemplateTestServer) CreateSandboxTemplate(_ context.Context, request *compatv1.CreateSandboxTemplateRequest) (*compatv1.SandboxTemplateResponse, error) {
	s.created = request.GetTemplate()
	return &compatv1.SandboxTemplateResponse{Template: request.GetTemplate()}, nil
}

func (s *workloadTemplateTestServer) GetSandboxTemplate(_ context.Context, request *compatv1.GetSandboxTemplateRequest) (*compatv1.SandboxTemplateResponse, error) {
	if request.GetWorkspaceScope().GetWorkspace() != "team" || request.GetName() != "agent" {
		return nil, status.Error(codes.InvalidArgument, "unexpected template target")
	}
	return &compatv1.SandboxTemplateResponse{Template: s.got}, nil
}

func (s *workloadTemplateTestServer) ListSandboxTemplates(_ context.Context, request *compatv1.ListSandboxTemplatesRequest) (*compatv1.ListSandboxTemplatesResponse, error) {
	if request.GetWorkspaceScope().GetWorkspace() != "team" {
		return nil, status.Error(codes.InvalidArgument, "unexpected workspace")
	}
	if s.page == 0 {
		if request.GetPageToken() != "" {
			return nil, status.Error(codes.InvalidArgument, "first page has unexpected token")
		}
		s.page++
		return &compatv1.ListSandboxTemplatesResponse{Templates: []*compatv1.SandboxWorkloadTemplate{s.got}, NextPageToken: "page-2"}, nil
	}
	if request.GetPageToken() != "page-2" {
		return nil, status.Error(codes.InvalidArgument, "second page token missing")
	}
	s.page++
	return &compatv1.ListSandboxTemplatesResponse{Templates: s.listed}, nil
}

func (s *workloadTemplateTestServer) DeleteSandboxTemplate(_ context.Context, request *compatv1.DeleteSandboxTemplateRequest) (*compatv1.DeleteSandboxTemplateResponse, error) {
	if request.GetWorkspaceScope().GetWorkspace() != "team" || request.GetName() != "agent" {
		return nil, status.Error(codes.InvalidArgument, "unexpected template target")
	}
	s.deleted = true
	return &compatv1.DeleteSandboxTemplateResponse{Outcome: compatv1.DeletionOutcome_DELETION_OUTCOME_COMPLETED}, nil
}

func TestWorkloadTemplateGatewayCRUDMapsWorkspaceAndWorkload(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	template := &compatv1.SandboxWorkloadTemplate{
		Metadata: &datamodelv1.ObjectMeta{Name: "agent", Workspace: "team", Labels: map[string]string{"owner": "agents"}},
		Spec: &compatv1.SandboxWorkloadTemplateSpec{Workload: &compatv1.SandboxWorkloadConfig{
			Image: "registry.example/agent:v1", Environment: map[string]string{"MODE": "fast"},
			Resources: &compatv1.SandboxResources{Cpu: "500m", Memory: "2Gi", Gpu: &compatv1.GpuResourceRequirements{}},
		}},
	}
	second := &compatv1.SandboxWorkloadTemplate{Metadata: &datamodelv1.ObjectMeta{Name: "worker", Workspace: "team"}, Spec: template.GetSpec()}
	service := &workloadTemplateTestServer{got: template, listed: []*compatv1.SandboxWorkloadTemplate{second}}
	compatv1.RegisterOpenShellServer(server, service)
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

	created, err := client.CreateSandboxTemplate(context.Background(), WorkloadTemplate{
		Name: "agent", Image: "registry.example/agent:v1", CPU: "500m", Memory: "2Gi", GPU: true,
		Environment: map[string]string{"MODE": "fast"}, Labels: map[string]string{"owner": "agents"},
		DriverConfigJSON: `{"docker":{"network":"isolated"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "agent" || created.Workspace != "team" || created.Image != "registry.example/agent:v1" || created.CPU != "500m" || created.Memory != "2Gi" || !created.GPU || created.GPUCount != 0 {
		t.Fatalf("created template mapping = %+v", created)
	}
	if service.created.GetMetadata().GetWorkspace() != "team" || service.created.GetSpec().GetWorkload().GetEnvironment()["MODE"] != "fast" || service.created.GetSpec().GetDriverConfig().AsMap()["docker"] == nil {
		t.Fatalf("gateway create request did not preserve workspace/workload: %+v", service.created)
	}
	if _, err := client.GetSandboxTemplate(context.Background(), "team", "agent"); err != nil {
		t.Fatal(err)
	}
	list, err := client.ListSandboxTemplates(context.Background(), "team")
	if err != nil || len(list) != 2 || list[0].Name != "agent" || list[1].Name != "worker" {
		t.Fatalf("list templates = %+v, %v", list, err)
	}
	deleted, err := client.DeleteSandboxTemplate(context.Background(), "team", "agent")
	if err != nil || !deleted || !service.deleted {
		t.Fatalf("delete template = %v, %v, server deleted=%v", deleted, err, service.deleted)
	}
}

func TestCreateSandboxTemplateRejectsInvalidDriverConfigBeforeRPC(t *testing.T) {
	client := &Client{}
	_, err := client.CreateSandboxTemplate(context.Background(), WorkloadTemplate{Name: "agent", Image: "image", DriverConfigJSON: `[]`})
	if err == nil || err.Error() != "create sandbox template: driver config must be a non-empty JSON object" {
		t.Fatalf("invalid config error = %v", err)
	}
}
