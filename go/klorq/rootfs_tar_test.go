package klorq

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
)

type rootfsStagingServer struct {
	compatv1.UnimplementedOpenShellServer
	gotWorkspace string
	gotFileName  string
	gotSize      uint64
	uploadPath   string
	maxBytes     uint64
}

func (s *rootfsStagingServer) BeginRootfsTarStaging(_ context.Context, request *compatv1.BeginRootfsTarStagingRequest) (*compatv1.BeginRootfsTarStagingResponse, error) {
	s.gotWorkspace = request.GetWorkspaceScope().GetWorkspace()
	s.gotFileName = request.GetFileName()
	s.gotSize = request.GetSizeBytes()
	return &compatv1.BeginRootfsTarStagingResponse{StagingToken: "opaque-token", UploadPath: s.uploadPath, MaxBytes: s.maxBytes}, nil
}

func TestUploadRootfsTarWritesOpenShellStagingPath(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	grpcServer := grpc.NewServer()
	path := filepath.Join(t.TempDir(), "rootfs.tar")
	staging := &rootfsStagingServer{uploadPath: path}
	compatv1.RegisterOpenShellServer(grpcServer, staging)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
	})

	client := New("http://" + listener.Addr().String())
	client.Workspace = "team-a"
	t.Cleanup(func() { _ = client.Close() })
	token, err := client.UploadRootfsTar(context.Background(), "rootfs.tar", uint64(len("tar-bytes")), strings.NewReader("tar-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if token != "opaque-token" {
		t.Fatalf("token=%q", token)
	}
	if staging.gotWorkspace != "team-a" || staging.gotFileName != "rootfs.tar" || staging.gotSize != uint64(len("tar-bytes")) {
		t.Fatalf("staging request workspace=%q file=%q size=%d", staging.gotWorkspace, staging.gotFileName, staging.gotSize)
	}
	if uploaded, err := os.ReadFile(path); err != nil || string(uploaded) != "tar-bytes" {
		t.Fatalf("staged archive=%q err=%v", uploaded, err)
	}
}

func TestUploadRootfsTarRejectsGatewaySizeLimit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	grpcServer := grpc.NewServer()
	path := filepath.Join(t.TempDir(), "rootfs.tar")
	compatv1.RegisterOpenShellServer(grpcServer, &rootfsStagingServer{uploadPath: path, maxBytes: 3})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	client := New("http://" + listener.Addr().String())
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.UploadRootfsTar(context.Background(), "rootfs.tar", 4, strings.NewReader("data")); err == nil || !strings.Contains(err.Error(), "exceeding the gateway limit") {
		t.Fatalf("UploadRootfsTar error=%v, want Gateway size limit", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("oversized upload created staging file: stat error=%v", err)
	}
}
