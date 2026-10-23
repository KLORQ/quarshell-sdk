package klorq

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	runtimev1 "github.com/KLORQ/quarshell-proto/gen/go/quarshell/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type interactiveExecServer struct {
	pb.UnimplementedOpenShellServer
	runtimev1.UnimplementedMainProcessServiceServer
	mu        sync.Mutex
	request   *pb.ExecSandboxRequest
	mainStart *runtimev1.MainProcessTTYStart
	stdin     []byte
	cols      uint32
	rows      uint32
	signal    runtimev1.MainProcessSignal
}

func (s *interactiveExecServer) Signal(_ context.Context, request *runtimev1.MainProcessServiceSignalRequest) (*runtimev1.MainProcessServiceSignalResponse, error) {
	s.mu.Lock()
	s.signal = request.GetSignal()
	s.mu.Unlock()
	return &runtimev1.MainProcessServiceSignalResponse{}, nil
}

func (s *interactiveExecServer) AttachTTY(stream grpc.BidiStreamingServer[runtimev1.MainProcessServiceAttachTTYRequest, runtimev1.MainProcessServiceAttachTTYResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.mainStart = first.GetStart()
	s.mu.Unlock()
	for {
		if _, err := stream.Recv(); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	if err := stream.Send(&runtimev1.MainProcessServiceAttachTTYResponse{Payload: &runtimev1.MainProcessServiceAttachTTYResponse_Stdout{Stdout: []byte("main-output")}}); err != nil {
		return err
	}
	return stream.Send(&runtimev1.MainProcessServiceAttachTTYResponse{Payload: &runtimev1.MainProcessServiceAttachTTYResponse_ExitCode{ExitCode: 7}})
}

type stalledInteractiveExecServer struct {
	pb.UnimplementedOpenShellServer
	started chan struct{}
}

func (s *stalledInteractiveExecServer) ExecSandboxInteractive(stream grpc.BidiStreamingServer[pb.ExecSandboxInput, pb.ExecSandboxEvent]) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	close(s.started)
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (s *interactiveExecServer) ExecSandboxInteractive(stream grpc.BidiStreamingServer[pb.ExecSandboxInput, pb.ExecSandboxEvent]) error {
	start, err := stream.Recv()
	if err != nil {
		return err
	}
	request := start.GetStart()
	s.mu.Lock()
	s.request = request
	s.mu.Unlock()
	for {
		frame, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return recvErr
		}
		if input := frame.GetStdin(); len(input) > 0 {
			s.mu.Lock()
			s.stdin = append(s.stdin, input...)
			s.mu.Unlock()
		}
		if resize := frame.GetResize(); resize != nil {
			s.mu.Lock()
			s.cols, s.rows = resize.GetCols(), resize.GetRows()
			s.mu.Unlock()
		}
	}
	if err := stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Stdout{Stdout: &pb.ExecSandboxStdout{Data: []byte("stdout")}}}); err != nil {
		return err
	}
	if err := stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Stderr{Stderr: &pb.ExecSandboxStderr{Data: []byte("stderr")}}}); err != nil {
		return err
	}
	return stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Exit{Exit: &pb.ExecSandboxExit{ExitCode: 11}}})
}

func (s *interactiveExecServer) ExecSandbox(request *pb.ExecSandboxRequest, stream grpc.ServerStreamingServer[pb.ExecSandboxEvent]) error {
	s.mu.Lock()
	s.request = request
	s.mu.Unlock()
	if err := stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Stdout{Stdout: &pb.ExecSandboxStdout{Data: []byte("out")}}}); err != nil {
		return err
	}
	if err := stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Stderr{Stderr: &pb.ExecSandboxStderr{Data: []byte("err")}}}); err != nil {
		return err
	}
	return stream.Send(&pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Exit{Exit: &pb.ExecSandboxExit{ExitCode: 5}}})
}

func TestExecInteractiveWithTTYStreamsNonTTYInputAndWorkspace(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &interactiveExecServer{}
	pb.RegisterOpenShellServer(server, backend)
	runtimev1.RegisterMainProcessServiceServer(server, backend)
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
	if err := client.SignalMainProcess(context.Background(), "team", "shared", runtimev1.MainProcessSignal_MAIN_PROCESS_SIGNAL_INTERRUPT); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	signal := backend.signal
	backend.mu.Unlock()
	if signal != runtimev1.MainProcessSignal_MAIN_PROCESS_SIGNAL_INTERRUPT {
		t.Fatalf("main-process signal=%v, want interrupt", signal)
	}
	session, err := client.ExecInteractiveWithTTYTimeout(context.Background(), "team", "shared", []string{"cat"}, false, 0, 0, 13*time.Second, SandboxExecOptions{
		WorkDir: "/workspace", Env: map[string]string{"MODE": "test"}, NoLoginShell: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("piped input")); err != nil {
		t.Fatal(err)
	}
	if err := CloseSandboxExecInput(session); err != nil {
		t.Fatal(err)
	}
	stdout, err := session.NextOutput()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := session.NextOutput()
	if err != nil {
		t.Fatal(err)
	}
	code, err := session.ExitCode()
	if err != nil || code != 11 || string(stdout.Data) != "stdout" || stdout.Stream != "stdout" || string(stderr.Data) != "stderr" || stderr.Stream != "stderr" {
		t.Fatalf("session stdout=%+v stderr=%+v exit=%d err=%v", stdout, stderr, code, err)
	}
	_ = session.Close()

	backend.mu.Lock()
	if backend.request == nil || backend.request.GetSandbox() != "shared" || backend.request.GetWorkspaceScope().GetWorkspace() != "team" || backend.request.GetTty() || backend.request.GetCols() != 0 || backend.request.GetRows() != 0 {
		t.Fatalf("interactive start request: %+v", backend.request)
	}
	if backend.request.GetWorkdir() != "/workspace" || backend.request.GetEnvironment()["MODE"] != "test" || !backend.request.GetNoLoginShell() || backend.request.GetExecutionTimeout().AsDuration() != 13*time.Second || string(backend.stdin) != "piped input" {
		t.Fatalf("interactive request/input lost: request=%+v stdin=%q", backend.request, backend.stdin)
	}
	backend.mu.Unlock()

	ttySession, err := client.ExecInteractiveWithTTY(context.Background(), "team", "shared", []string{"bash"}, true, 113, 41, SandboxExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ttySession.Resize(120, 43); err != nil {
		t.Fatal(err)
	}
	if err := CloseSandboxExecInput(ttySession); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, ttySession); err != nil {
		t.Fatal(err)
	}
	if code, err := ttySession.ExitCode(); err != nil || code != 11 {
		t.Fatalf("tty exit=(%d,%v)", code, err)
	}
	_ = ttySession.Close()
	backend.mu.Lock()
	if backend.request == nil || !backend.request.GetTty() || backend.request.GetCols() != 113 || backend.request.GetRows() != 41 || backend.cols != 120 || backend.rows != 43 {
		t.Fatalf("TTY dimensions not preserved: %+v", backend.request)
	}
	backend.mu.Unlock()
	mainSession, err := client.ExecInteractiveWithTTY(context.Background(), "team", "shared", nil, true, 80, 24, SandboxExecOptions{AttachMainProcess: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseSandboxExecInput(mainSession); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, mainSession); err != nil {
		t.Fatal(err)
	}
	if _, err := mainSession.ExitCode(); err != nil {
		t.Fatal(err)
	}
	_ = mainSession.Close()
	backend.mu.Lock()
	mainStart := backend.mainStart
	backend.mu.Unlock()
	if mainStart == nil || mainStart.GetWorkspace() != "team" || mainStart.GetSandbox() != "shared" || mainStart.GetCols() != 80 || mainStart.GetRows() != 24 || mainStart.GetNonTty() {
		t.Fatalf("main-process attachment not preserved: %+v", mainStart)
	}
	pipeSession, err := client.ExecInteractiveWithTTY(context.Background(), "team", "shared", nil, false, 0, 0, SandboxExecOptions{AttachMainProcess: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseSandboxExecInput(pipeSession); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, pipeSession); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeSession.ExitCode(); err != nil {
		t.Fatal(err)
	}
	_ = pipeSession.Close()
	backend.mu.Lock()
	mainStart = backend.mainStart
	backend.mu.Unlock()
	if mainStart == nil || !mainStart.GetNonTty() {
		t.Fatalf("non-TTY main-process attachment was not selected: %+v", mainStart)
	}
}

func TestGatewayInteractiveSessionCloseCancelsBlockedInputWrite(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &stalledInteractiveExecServer{started: make(chan struct{})}
	pb.RegisterOpenShellServer(server, backend)
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
	session, err := client.ExecInteractiveWithTTY(context.Background(), "team", "shared", []string{"cat"}, false, 0, 0, SandboxExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("server did not start interactive exec")
	}

	chunk := make([]byte, 32<<10)
	writeStarted := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		close(writeStarted)
		for range 128 {
			if _, writeErr := session.Write(chunk); writeErr != nil {
				writeDone <- writeErr
				return
			}
		}
		writeDone <- nil
	}()
	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("input writer did not start")
	}
	select {
	case err := <-writeDone:
		t.Fatalf("all upload frames passed while the server was not reading input: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	select {
	case err := <-closeDone:
		_ = err // Cancelling the RPC may be reported by CloseSend.
	case <-time.After(time.Second):
		t.Fatal("session close did not cancel the blocked input writer")
	}
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("blocked input writer completed successfully after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelling the RPC did not release the blocked input writer")
	}
}

func TestExecStreamingWithTimeoutSendsServerDeadlineAndSeparateOutput(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	backend := &interactiveExecServer{}
	pb.RegisterOpenShellServer(server, backend)
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
	stream, err := client.ExecStreamingWithTimeout(context.Background(), "team", "shared", []string{"echo", "ok"}, 9*time.Second, SandboxExecOptions{WorkDir: "/work"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []*SandboxExecChunk
	for {
		chunk, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		chunks = append(chunks, chunk)
	}
	code, err := stream.ExitCode()
	if err != nil || code != 5 || len(chunks) != 2 || string(chunks[0].Data) != "out" || chunks[0].Stream != "stdout" || string(chunks[1].Data) != "err" || chunks[1].Stream != "stderr" {
		t.Fatalf("stream chunks=%+v exit=%d err=%v", chunks, code, err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.request == nil || backend.request.GetWorkspaceScope().GetWorkspace() != "team" || backend.request.GetExecutionTimeout().AsDuration() != 9*time.Second {
		t.Fatalf("stream request missing scope/timeout: %+v", backend.request)
	}
}
