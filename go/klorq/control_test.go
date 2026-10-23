package klorq

import (
	"context"
	"net"
	"testing"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type sandboxCreateTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	request *gatewayv1.CreateSandboxRequest
}

func (s *sandboxCreateTestServer) CreateSandbox(_ context.Context, request *gatewayv1.CreateSandboxRequest) (*gatewayv1.SandboxResponse, error) {
	s.request = request
	return &gatewayv1.SandboxResponse{Sandbox: &gatewayv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{Name: request.GetName(), Workspace: request.GetWorkspaceScope().GetWorkspace()}, Spec: request.GetSpec()}, ServiceUrls: map[string]string{"api": "https://service.example"}}, nil
}

type nativeLifecycleTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	starts, stops, deletes int
}

func (s *nativeLifecycleTestServer) StartSandbox(_ context.Context, request *gatewayv1.StartSandboxRequest) (*gatewayv1.SandboxResponse, error) {
	s.starts++
	return &gatewayv1.SandboxResponse{Sandbox: &gatewayv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{Name: request.GetName(), Workspace: request.GetWorkspaceScope().GetWorkspace()}}}, nil
}

func (s *nativeLifecycleTestServer) StopSandbox(_ context.Context, request *gatewayv1.StopSandboxRequest) (*gatewayv1.SandboxResponse, error) {
	s.stops++
	return &gatewayv1.SandboxResponse{Sandbox: &gatewayv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{Name: request.GetName(), Workspace: request.GetWorkspaceScope().GetWorkspace()}}}, nil
}

func (s *nativeLifecycleTestServer) DeleteSandbox(_ context.Context, _ *gatewayv1.DeleteSandboxRequest) (*gatewayv1.DeleteSandboxResponse, error) {
	s.deletes++
	return &gatewayv1.DeleteSandboxResponse{Outcome: gatewayv1.DeletionOutcome_DELETION_OUTCOME_COMPLETED}, nil
}

func TestNewRequestIDIsOpaqueAndUnique(t *testing.T) {
	first, err := newRequestID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || len(second) != 32 || first == second {
		t.Fatalf("invalid request IDs: %q %q", first, second)
	}
}

func TestCreateSandboxMapsNativeOpenShellWorkloadFields(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	create := &sandboxCreateTestServer{}
	gatewayv1.RegisterOpenShellServer(server, create)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := &Client{gatewayConn: connection}
	result, err := client.CreateControlSandboxOperation(context.Background(), Sandbox{
		Name: "agent", Workspace: "team", Image: "alpine:3.22",
		Labels:    Labels{"purpose": "test"},
		CPUMillis: 500, MemoryBytes: 2 * 1024 * 1024 * 1024, GPUCount: 2,
		Environment: map[string]string{"FEATURE_FLAG": "on"}, DriverConfigJSON: `{"docker":{"shm_size":67108864}}`, TTY: true,
		PolicyYAML:        "version: 1\nfilesystem_policy:\n  include_workdir: true\n",
		AttachedProviders: []string{"github"}, RestartPolicy: "on-failure", Ephemeral: true,
	}, []string{"sleep", "infinity"})
	if err != nil {
		t.Fatal(err)
	}
	request := create.request
	if request == nil || request.GetWorkspaceScope().GetWorkspace() != "team" || request.GetSpec().GetTemplate().GetImage() != "alpine:3.22" || len(request.GetSpec().GetCommand()) != 2 || request.GetSpec().GetTemplate().GetResources().AsMap()["limits"].(map[string]any)["cpu"] != "500m" || request.GetSpec().GetResourceRequirements().GetGpu().GetCount() != 2 || request.GetSpec().GetEnvironment()["FEATURE_FLAG"] != "on" || request.GetSpec().GetTemplate().GetDriverConfig().AsMap()["docker"] == nil || !request.GetSpec().GetTty() || request.GetSpec().GetPolicy().GetFilesystem().GetIncludeWorkdir() != true || len(request.GetSpec().GetProviders()) != 1 || request.GetSpec().GetProviders()[0] != "github" || request.GetSpec().GetRestartPolicy() != gatewayv1.SandboxRestartPolicy_SANDBOX_RESTART_POLICY_ON_FAILURE || request.GetAnnotations()["openshell.nvidia.com/retention"] != "ephemeral" {
		t.Fatalf("unexpected native create request: %+v", request)
	}
	if result.Sandbox.Name != "agent" || result.Sandbox.Workspace != "team" || result.ServiceURLs["api"] != "https://service.example" || result.RequestID == "" {
		t.Fatalf("unexpected create result: %+v", result)
	}
}

func TestControlLifecycleRequiresAndPreservesCompletion(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &nativeLifecycleTestServer{}
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
	client := &Client{gatewayConn: connection}

	started, err := client.ChangeControlSandboxStateOperation(context.Background(), "team", "demo", "start")
	if err != nil || started.Sandbox.Name != "demo" || started.Sandbox.Workspace != "team" {
		t.Fatalf("start result = %+v, %v", started, err)
	}
	stopped, err := client.StopControlSandbox(context.Background(), "team", "demo")
	if err != nil || stopped.Name != "demo" || backend.starts != 1 || backend.stops != 1 {
		t.Fatalf("stop result = %+v, %v; calls start=%d stop=%d", stopped, err, backend.starts, backend.stops)
	}
	if err := client.DeleteControlSandbox(context.Background(), "team", "demo"); err != nil || backend.deletes != 1 {
		t.Fatalf("delete err=%v calls=%d", err, backend.deletes)
	}
}
