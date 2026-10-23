package klorq

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sshForwardTestGateway struct {
	gatewayv1.UnimplementedOpenShellServer
	gotInit chan *gatewayv1.TcpForwardInit
}

func (s *sshForwardTestGateway) ForwardTcp(stream grpc.BidiStreamingServer[gatewayv1.TcpForwardFrame, gatewayv1.TcpForwardFrame]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	init := first.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "missing init")
	}
	if auth, ok := metadata.FromIncomingContext(stream.Context()); ok {
		if values := auth.Get("authorization"); len(values) == 1 && values[0] == "Bearer operator-token" {
			select {
			case s.gotInit <- init:
			default:
			}
		}
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if data := frame.GetData(); len(data) > 0 {
			if err := stream.Send(&gatewayv1.TcpForwardFrame{Payload: &gatewayv1.TcpForwardFrame_Data{Data: data}}); err != nil {
				return err
			}
		}
	}
}

func TestOpenSSHForwardUsesOpenShellForwardTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	gateway := &sshForwardTestGateway{gotInit: make(chan *gatewayv1.TcpForwardInit, 1)}
	gatewayv1.RegisterOpenShellServer(server, gateway)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	client := NewWithToken("http://"+listener.Addr().String(), "operator-token")
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.OpenSSHForward(ctx, "sandbox-id", "ssh-session-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload := []byte("openshell-forward-tcp")
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("forwarded bytes = %q, want %q", got, payload)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case init := <-gateway.gotInit:
		if init.GetSandbox() != "sandbox-id" || init.GetAuthorizationToken() != "ssh-session-token" || init.GetSsh() == nil {
			t.Fatalf("ForwardTcp init = %+v", init)
		}
	case <-ctx.Done():
		t.Fatal("ForwardTcp init not received")
	}
}

func TestOpenTCPForwardUsesNativeLoopbackTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	gateway := &sshForwardTestGateway{gotInit: make(chan *gatewayv1.TcpForwardInit, 1)}
	gatewayv1.RegisterOpenShellServer(server, gateway)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	client := NewWithToken("http://"+listener.Addr().String(), "operator-token")
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.OpenTCPForward(ctx, "sandbox-id", "service-forward:sandbox-id:127.0.0.1:9000", "127.0.0.1", 9000)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case init := <-gateway.gotInit:
		if init.GetSandbox() != "sandbox-id" || init.GetWorkspace() != "default" || init.GetServiceId() != "service-forward:sandbox-id:127.0.0.1:9000" || init.GetTcp().GetHost() != "127.0.0.1" || init.GetTcp().GetPort() != 9000 || init.GetAuthorizationToken() != "" {
			t.Fatalf("ForwardTcp init = %+v", init)
		}
	case <-ctx.Done():
		t.Fatal("TCP ForwardTcp init not received")
	}
}
