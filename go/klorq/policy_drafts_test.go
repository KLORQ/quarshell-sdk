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

type draftPolicyTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	getRequest     *gatewayv1.GetDraftPolicyRequest
	approveRequest *gatewayv1.ApproveDraftChunkRequest
	rejectRequest  *gatewayv1.RejectDraftChunkRequest
}

func (s *draftPolicyTestServer) GetDraftPolicy(_ context.Context, request *gatewayv1.GetDraftPolicyRequest) (*gatewayv1.GetDraftPolicyResponse, error) {
	s.getRequest = request
	return &gatewayv1.GetDraftPolicyResponse{Chunks: []*gatewayv1.PolicyChunk{{Id: "chunk-1", Status: "pending", ReviewToken: "review-token"}}}, nil
}

func (s *draftPolicyTestServer) ApproveDraftChunk(_ context.Context, request *gatewayv1.ApproveDraftChunkRequest) (*gatewayv1.ApproveDraftChunkResponse, error) {
	s.approveRequest = request
	return &gatewayv1.ApproveDraftChunkResponse{PolicyVersion: 2}, nil
}

func (s *draftPolicyTestServer) RejectDraftChunk(_ context.Context, request *gatewayv1.RejectDraftChunkRequest) (*gatewayv1.RejectDraftChunkResponse, error) {
	s.rejectRequest = request
	return &gatewayv1.RejectDraftChunkResponse{}, nil
}

func TestPolicyDraftMethodsUseNativeOpenShellRequests(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &draftPolicyTestServer{}
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
	client := &Client{gatewayConn: connection, Workspace: "team-a"}

	draft, err := client.GetDraftPolicy(context.Background(), "sandbox-a", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.GetChunks()) != 1 || draft.GetChunks()[0].GetReviewToken() != "review-token" || backend.getRequest.GetWorkspaceScope().GetWorkspace() != "team-a" || backend.getRequest.GetSandbox() != "sandbox-a" {
		t.Fatalf("unexpected native draft: response=%+v request=%+v", draft, backend.getRequest)
	}
	if _, err := client.ApproveDraftChunk(context.Background(), "sandbox-a", "chunk-1", "review-token"); err != nil {
		t.Fatal(err)
	}
	if backend.approveRequest.GetReviewToken() != "review-token" || backend.approveRequest.GetChunkId() != "chunk-1" || backend.approveRequest.GetRequestId() == "" {
		t.Fatalf("unexpected native approval: %+v", backend.approveRequest)
	}
	if _, err := client.RejectDraftChunk(context.Background(), "sandbox-a", "chunk-1", "too broad"); err != nil {
		t.Fatal(err)
	}
	if backend.rejectRequest.GetReason() != "too broad" || backend.rejectRequest.GetRequestId() == "" {
		t.Fatalf("unexpected native rejection: %+v", backend.rejectRequest)
	}
}
