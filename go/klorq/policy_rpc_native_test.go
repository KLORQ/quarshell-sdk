package klorq

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	sandboxv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type policyRPCNativeTestServer struct {
	gatewayv1.UnimplementedOpenShellServer
	statusRequests []*gatewayv1.GetSandboxPolicyStatusRequest
	updateRequest  *gatewayv1.UpdateConfigRequest
	configRequest  *sandboxv1.GetSandboxConfigRequest
	listRequests   []*gatewayv1.ListSandboxPoliciesRequest
	pushedLogs     []*gatewayv1.SandboxLogLine
}

func (s *policyRPCNativeTestServer) GetSandboxPolicyStatus(_ context.Context, request *gatewayv1.GetSandboxPolicyStatusRequest) (*gatewayv1.GetSandboxPolicyStatusResponse, error) {
	s.statusRequests = append(s.statusRequests, request)
	version := request.GetVersion()
	if version == 0 {
		version = 2
	}
	return &gatewayv1.GetSandboxPolicyStatusResponse{Revision: &gatewayv1.SandboxPolicyRevision{
		Version: version,
		Policy: &sandboxv1.SandboxPolicy{Version: 1, NetworkPolicies: map[string]*sandboxv1.NetworkPolicyRule{
			"api": {Name: "api", Endpoints: []*sandboxv1.NetworkEndpoint{{Host: "api.example.test", Port: 443, Tls: sandboxv1.NetworkTlsMode_NETWORK_TLS_MODE_SKIP}}},
		}},
	}}, nil
}

func (s *policyRPCNativeTestServer) GetSandbox(_ context.Context, request *gatewayv1.GetSandboxRequest) (*gatewayv1.SandboxResponse, error) {
	return &gatewayv1.SandboxResponse{Sandbox: &gatewayv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{
		Id: "sandbox-id", Name: request.GetName(), Workspace: request.GetWorkspaceScope().GetWorkspace(), ResourceVersion: 9,
	}}}, nil
}

func (s *policyRPCNativeTestServer) PushSandboxLogs(stream grpc.ClientStreamingServer[gatewayv1.PushSandboxLogsRequest, gatewayv1.PushSandboxLogsResponse]) error {
	for {
		request, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&gatewayv1.PushSandboxLogsResponse{})
		}
		if err != nil {
			return err
		}
		s.pushedLogs = append(s.pushedLogs, request.GetLogs()...)
	}
}

func (s *policyRPCNativeTestServer) GetGatewayInfo(context.Context, *gatewayv1.GetGatewayInfoRequest) (*gatewayv1.GetGatewayInfoResponse, error) {
	return &gatewayv1.GetGatewayInfoResponse{
		Status: gatewayv1.ServiceStatus_SERVICE_STATUS_HEALTHY, GatewayVersion: "0.1.4",
		ComputeDrivers: []*gatewayv1.ComputeDriverInfo{{Name: "docker", Capabilities: &gatewayv1.ComputeDriverCapabilities{DriverVersion: "25.0"}}},
	}, nil
}

func (s *policyRPCNativeTestServer) GetCurrentUser(context.Context, *gatewayv1.GetCurrentUserRequest) (*gatewayv1.GetCurrentUserResponse, error) {
	return &gatewayv1.GetCurrentUserResponse{Subject: "alice", Roles: []string{"user"}, Scopes: []string{"sandbox:read"}, IdentityProvider: "oidc"}, nil
}

func (s *policyRPCNativeTestServer) ListSandboxes(_ context.Context, request *gatewayv1.ListSandboxesRequest) (*gatewayv1.ListSandboxesResponse, error) {
	if request.GetPageToken() != "" {
		return &gatewayv1.ListSandboxesResponse{}, nil
	}
	return &gatewayv1.ListSandboxesResponse{Sandboxes: []*gatewayv1.Sandbox{{
		Metadata: &datamodelv1.ObjectMeta{Id: "sandbox-id", Name: "sandbox-a", Workspace: request.GetWorkspaceScope().GetWorkspace()},
		Status:   &gatewayv1.SandboxStatus{Phase: gatewayv1.SandboxPhase_SANDBOX_PHASE_READY},
	}}}, nil
}

func (s *policyRPCNativeTestServer) UpdateConfig(_ context.Context, request *gatewayv1.UpdateConfigRequest) (*gatewayv1.UpdateConfigResponse, error) {
	s.updateRequest = request
	return &gatewayv1.UpdateConfigResponse{Version: 3, Deleted: request.GetDeleteSetting()}, nil
}

func (s *policyRPCNativeTestServer) GetSandboxConfig(_ context.Context, request *sandboxv1.GetSandboxConfigRequest) (*sandboxv1.GetSandboxConfigResponse, error) {
	s.configRequest = request
	return &sandboxv1.GetSandboxConfigResponse{ConfigRevision: 4, Settings: map[string]*sandboxv1.EffectiveSetting{
		"proposal_approval_mode": {Value: &sandboxv1.SettingValue{Value: &sandboxv1.SettingValue_StringValue{StringValue: "auto"}}, Scope: sandboxv1.SettingScope_SETTING_SCOPE_SANDBOX},
	}, PolicySource: sandboxv1.PolicySource_POLICY_SOURCE_SANDBOX}, nil
}

func (*policyRPCNativeTestServer) GetGatewayConfig(context.Context, *sandboxv1.GetGatewayConfigRequest) (*sandboxv1.GetGatewayConfigResponse, error) {
	return &sandboxv1.GetGatewayConfigResponse{SettingsRevision: 7, Settings: map[string]*sandboxv1.SettingValue{
		"ocsf_json_enabled": {Value: &sandboxv1.SettingValue_BoolValue{BoolValue: true}},
	}}, nil
}

func (s *policyRPCNativeTestServer) ListSandboxPolicies(_ context.Context, request *gatewayv1.ListSandboxPoliciesRequest) (*gatewayv1.ListSandboxPoliciesResponse, error) {
	s.listRequests = append(s.listRequests, request)
	if request.GetPageToken() != "" {
		return &gatewayv1.ListSandboxPoliciesResponse{}, nil
	}
	return &gatewayv1.ListSandboxPoliciesResponse{Revisions: []*gatewayv1.SandboxPolicyRevision{{
		Version:     2,
		Status:      gatewayv1.PolicyStatus_POLICY_STATUS_LOADED,
		CreatedTime: timestamppb.Now(),
	}}}, nil
}

func TestPolicyMethodsUseNativeOpenShellRPCs(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &policyRPCNativeTestServer{}
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
	if err := client.PostLogs(context.Background(), "sandbox-a", []LogLine{{Text: "observed", Source: "cli", Level: "INFO"}}); err != nil {
		t.Fatalf("native sandbox log push: %v", err)
	}
	if len(backend.pushedLogs) != 1 || backend.pushedLogs[0].GetSandboxId() != "sandbox-id" || backend.pushedLogs[0].GetMessage() != "observed" {
		t.Fatalf("native sandbox logs: %+v", backend.pushedLogs)
	}
	if err := client.PostSandboxLogs(context.Background(), "sandbox-id", []LogLine{{Text: "supervisor event", Source: "sandbox", Level: "INFO"}}); err != nil {
		t.Fatalf("sandbox-token log push with known identity: %v", err)
	}
	if len(backend.pushedLogs) != 2 || backend.pushedLogs[1].GetSandboxId() != "sandbox-id" || backend.pushedLogs[1].GetMessage() != "supervisor event" {
		t.Fatalf("direct sandbox log push: %+v", backend.pushedLogs)
	}

	info, err := client.Info(context.Background())
	if err != nil || info["gateway_version"] != "0.1.4" {
		t.Fatalf("native gateway info=%v err=%v", info, err)
	}
	viewer, err := client.Whoami(context.Background())
	if err != nil || viewer["subject"] != "alice" {
		t.Fatalf("native viewer=%v err=%v", viewer, err)
	}
	overview, err := client.ControlOverview(context.Background())
	if err != nil || overview["sandbox_count"] != 1 || overview["registry_running_count"] != 1 {
		t.Fatalf("native overview=%v err=%v", overview, err)
	}

	global, err := client.GetGlobalPolicy(context.Background())
	if err != nil || !strings.Contains(string(global), "tls: skip") || !backend.statusRequests[0].GetGlobal() {
		t.Fatalf("native global policy read: policy=%s request=%+v err=%v", global, backend.statusRequests, err)
	}

	document := []byte("version: 1\nnetwork_policies:\n  api:\n    name: api\n    endpoints:\n    - host: api.example.test\n      port: 443\n      tls: skip\n")
	if err := client.PutGlobalPolicy(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if !backend.updateRequest.GetGlobal() || backend.updateRequest.GetPolicy().GetNetworkPolicies()["api"].GetEndpoints()[0].GetTls() != sandboxv1.NetworkTlsMode_NETWORK_TLS_MODE_SKIP || backend.updateRequest.GetRequestId() == "" {
		t.Fatalf("unexpected native global update: %+v", backend.updateRequest)
	}

	current, err := client.GetSandboxPolicy(context.Background(), "sandbox-a", "base")
	if err != nil || !strings.Contains(string(current), "network_policies:") {
		t.Fatalf("native sandbox policy read: policy=%s err=%v", current, err)
	}
	updated, version, err := client.PutSandboxPolicy(context.Background(), "sandbox-a", document)
	if err != nil || version != 3 || !strings.Contains(string(updated), "tls: skip") {
		t.Fatalf("native sandbox policy update: policy=%s version=%d err=%v", updated, version, err)
	}
	if backend.updateRequest.GetSandbox() != "sandbox-a" || backend.updateRequest.GetExpectedResourceVersion() != 9 || backend.updateRequest.GetWorkspaceScope().GetWorkspace() != "team-a" {
		t.Fatalf("unexpected native sandbox update scope/version: %+v", backend.updateRequest)
	}
	if err := client.PutSandboxSetting(context.Background(), "team-a", "sandbox-a", "proposal_approval_mode", "auto"); err != nil {
		t.Fatal(err)
	}
	if backend.updateRequest.GetSandbox() != "sandbox-a" || backend.updateRequest.GetSettingKey() != "proposal_approval_mode" || backend.updateRequest.GetSettingValue().GetStringValue() != "auto" || backend.updateRequest.GetExpectedResourceVersion() != 9 || backend.updateRequest.GetWorkspaceScope().GetWorkspace() != "team-a" {
		t.Fatalf("unexpected native sandbox setting update: %+v", backend.updateRequest)
	}
	settings, err := client.GetSandboxSettings(context.Background(), "team-a", "sandbox-a")
	if err != nil || settings.GetConfigRevision() != 4 || settings.GetSettings()["proposal_approval_mode"].GetValue().GetStringValue() != "auto" || backend.configRequest.GetWorkspaceScope().GetWorkspace() != "team-a" {
		t.Fatalf("native sandbox settings=%+v request=%+v err=%v", settings, backend.configRequest, err)
	}
	globalSettings, err := client.GetGatewaySettings(context.Background())
	if err != nil || globalSettings.GetSettingsRevision() != 7 || globalSettings.GetSettings()["ocsf_json_enabled"].GetBoolValue() != true {
		t.Fatalf("native global settings=%+v err=%v", globalSettings, err)
	}
	deleted, err := client.DeleteGatewaySetting(context.Background(), "ocsf_schema_version")
	if err != nil || !deleted.GetDeleted() || !backend.updateRequest.GetGlobal() || !backend.updateRequest.GetDeleteSetting() {
		t.Fatalf("native global setting delete=%+v request=%+v err=%v", deleted, backend.updateRequest, err)
	}

	revisions, err := client.ListPolicyRevisions(context.Background(), "sandbox-a")
	if err != nil || len(revisions) != 1 || revisions[0].Rev != 2 || revisions[0].Status != "loaded" {
		t.Fatalf("native policy history: revisions=%+v err=%v", revisions, err)
	}
	historical, err := client.GetPolicyRevision(context.Background(), "sandbox-a", 2)
	if err != nil || !strings.Contains(historical, "api.example.test") {
		t.Fatalf("native historical policy: policy=%s err=%v", historical, err)
	}
}
