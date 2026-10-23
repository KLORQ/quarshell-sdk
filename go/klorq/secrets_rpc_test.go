package klorq

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type providerEnvironmentCapabilityServer struct {
	compatv1.UnimplementedOpenShellServer
	mu      sync.Mutex
	request *compatv1.GetSandboxProviderEnvironmentRequest
	bearer  string
}

func (s *providerEnvironmentCapabilityServer) GetSandboxProviderEnvironment(ctx context.Context, req *compatv1.GetSandboxProviderEnvironmentRequest) (*compatv1.GetSandboxProviderEnvironmentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.request = req
	values, _ := metadata.FromIncomingContext(ctx)
	if authorization := values.Get("authorization"); len(authorization) == 1 {
		s.bearer = strings.TrimPrefix(authorization[0], "Bearer ")
	}
	return &compatv1.GetSandboxProviderEnvironmentResponse{Environment: map[string]string{"API_TOKEN": "bound-value"}}, nil
}

func TestResolveProxySecretsAdvertisesOnlyStaticEndpointBindingCapability(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	service := &providerEnvironmentCapabilityServer{}
	compatv1.RegisterOpenShellServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	httpGateway := httptest.NewServer(http.NotFoundHandler())
	defer httpGateway.Close()
	client := NewWithToken(httpGateway.URL, "sandbox-token")
	client.SetGRPCEndpoint("http://" + listener.Addr().String())
	t.Cleanup(func() { _ = client.Close() })
	got, err := client.ResolveProxySecrets(context.Background(), "sandbox-a")
	if err != nil || got["API_TOKEN"] != "bound-value" {
		t.Fatalf("resolved credentials=%v err=%v", got, err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.request == nil || service.request.GetSandboxId() != "sandbox-a" || !service.request.GetSupportsStaticCredentialBindings() || service.request.GetSupportsStablePlaceholderEnvironmentKeys() || service.bearer != "sandbox-token" {
		t.Fatalf("capability request=%v bearer=%q", service.request, service.bearer)
	}
}
