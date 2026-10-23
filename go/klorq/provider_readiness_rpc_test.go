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

type providerReadinessTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	statusRequest *gatewayv1.GetSandboxProviderStatusRequest
	attachRequest *gatewayv1.AttachSandboxProviderRequest
}

func (s *providerReadinessTestServer) GetSandboxProviderStatus(_ context.Context, request *gatewayv1.GetSandboxProviderStatusRequest) (*gatewayv1.GetSandboxProviderStatusResponse, error) {
	s.statusRequest = request
	return &gatewayv1.GetSandboxProviderStatusResponse{Status: &gatewayv1.ProviderReadinessStatus{
		Receipt:  &gatewayv1.ProviderMutationReceipt{ReceiptId: "receipt-1", MutationId: "mutation-1", Provider: "github", Workspace: "team", Kind: gatewayv1.ProviderMutationKind_PROVIDER_MUTATION_KIND_ATTACH},
		State:    gatewayv1.ProviderReadinessState_PROVIDER_READINESS_STATE_READY,
		Reason:   gatewayv1.ProviderReadinessReason_PROVIDER_READINESS_REASON_UNSPECIFIED,
		Observed: &gatewayv1.ProviderReadinessObservation{SessionId: "session-1", Sequence: 2, ProviderEnvRevision: 9},
	}}, nil
}
func (s *providerReadinessTestServer) GetSandbox(context.Context, *gatewayv1.GetSandboxRequest) (*gatewayv1.SandboxResponse, error) {
	return &gatewayv1.SandboxResponse{Sandbox: &gatewayv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{Name: "demo", ResourceVersion: 3}}}, nil
}
func (s *providerReadinessTestServer) AttachSandboxProvider(_ context.Context, request *gatewayv1.AttachSandboxProviderRequest) (*gatewayv1.AttachSandboxProviderResponse, error) {
	s.attachRequest = request
	return &gatewayv1.AttachSandboxProviderResponse{Receipt: &gatewayv1.ProviderMutationReceipt{ReceiptId: "receipt-attach"}}, nil
}

func TestProviderReadinessAndAttachReceiptUseNativeRPCs(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &providerReadinessTestServer{}
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

	status, err := client.GetSandboxProviderStatus(context.Background(), "demo", "github", "receipt-1")
	if err != nil {
		t.Fatal(err)
	}
	if backend.statusRequest.GetWorkspaceScope().GetWorkspace() != "team" || backend.statusRequest.GetReceiptId() != "receipt-1" {
		t.Fatalf("status request=%+v", backend.statusRequest)
	}
	if status.ReceiptID != "receipt-1" || status.State != "PROVIDER_READINESS_STATE_READY" || status.Observed == nil || status.Observed.Sequence != 2 {
		t.Fatalf("status=%+v", status)
	}
	receipt, err := client.AttachProviderWithReceipt(context.Background(), "demo", "github")
	if err != nil {
		t.Fatal(err)
	}
	if receipt != "receipt-attach" || backend.attachRequest.GetExpectedResourceVersion() != 3 {
		t.Fatalf("receipt=%q attach request=%+v", receipt, backend.attachRequest)
	}
}
