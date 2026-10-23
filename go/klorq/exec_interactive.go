package klorq

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	runtimev1 "github.com/KLORQ/quarshell-proto/gen/go/cautem/runtime/v1"
	dm "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	pb "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

// ExecInteractiveWithTTY opens the OpenShell bidirectional exec RPC with an
// explicit PTY setting. The upstream Go facade currently hardcodes TTY=true.
func (c *Client) ExecInteractiveWithTTY(ctx context.Context, workspace, name string, argv []string, tty bool, cols, rows uint32, options SandboxExecOptions) (SandboxInteractiveOutput, error) {
	return c.ExecInteractiveWithTTYTimeout(ctx, workspace, name, argv, tty, cols, rows, 0, options)
}

// ExecInteractiveWithTTYTimeout opens interactive exec with an explicit PTY
// setting and optional server-side process deadline.
func (c *Client) ExecInteractiveWithTTYTimeout(ctx context.Context, workspace, name string, argv []string, tty bool, cols, rows uint32, timeout time.Duration, options SandboxExecOptions) (SandboxInteractiveOutput, error) {
	if options.AttachMainProcess {
		if !tty || len(argv) != 0 {
			return nil, fmt.Errorf("main-process attachment requires tty=true and an empty command")
		}
		return c.AttachMainProcessTTY(ctx, workspace, name, cols, rows)
	}
	if workspace == "" || name == "" || (len(argv) == 0 && !options.AttachMainProcess) {
		return nil, fmt.Errorf("usage: ExecInteractiveWithTTY(workspace, name, argv, tty, cols, rows, options)")
	}
	rpc, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := rpc.ExecSandboxInteractive(streamCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open interactive exec in sandbox %q: %w", name, err)
	}
	request := &pb.ExecSandboxRequest{
		Sandbox:        name,
		Command:        append([]string(nil), argv...),
		Workdir:        options.WorkDir,
		Environment:    cloneStringMap(options.Env),
		NoLoginShell:   options.NoLoginShell,
		Tty:            tty,
		WorkspaceScope: &dm.WorkspaceSelector{Selection: &dm.WorkspaceSelector_Workspace{Workspace: workspace}},
	}
	if timeout > 0 {
		request.ExecutionTimeout = durationpb.New(timeout)
	}
	if tty {
		request.Cols, request.Rows = cols, rows
	}
	if err := stream.Send(&pb.ExecSandboxInput{Payload: &pb.ExecSandboxInput_Start{Start: request}}); err != nil {
		cancel()
		return nil, fmt.Errorf("start interactive exec in sandbox %q: %w", name, err)
	}
	return newGatewayInteractiveSession(streamCtx, cancel, stream), nil
}

func (c *Client) execStreamingWithTimeout(ctx context.Context, workspace, name string, argv []string, timeout time.Duration, options SandboxExecOptions) (SandboxExecStream, error) {
	if workspace == "" || name == "" || len(argv) == 0 {
		return nil, fmt.Errorf("usage: ExecStreamingWithTimeout(workspace, name, argv, timeout, options)")
	}
	rpc, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	request := &pb.ExecSandboxRequest{
		Sandbox:        name,
		Command:        append([]string(nil), argv...),
		Workdir:        options.WorkDir,
		Environment:    cloneStringMap(options.Env),
		NoLoginShell:   options.NoLoginShell,
		WorkspaceScope: &dm.WorkspaceSelector{Selection: &dm.WorkspaceSelector_Workspace{Workspace: workspace}},
	}
	if timeout > 0 {
		request.ExecutionTimeout = durationpb.New(timeout)
	}
	stream, err := rpc.ExecSandbox(streamCtx, request)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stream exec in sandbox %q: %w", name, err)
	}
	return &gatewayExecOutputStream{stream: stream, cancel: cancel}, nil
}

type gatewayExecOutputStream struct {
	stream  grpc.ServerStreamingClient[pb.ExecSandboxEvent]
	cancel  context.CancelFunc
	exit    int
	done    bool
	hasExit bool
}

func (s *gatewayExecOutputStream) Next() (*SandboxExecChunk, error) {
	if s.done {
		return nil, io.EOF
	}
	event, err := s.stream.Recv()
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, err
	}
	if output := event.GetStdout(); output != nil {
		return &SandboxExecChunk{Stream: "stdout", Data: append([]byte(nil), output.GetData()...)}, nil
	}
	if output := event.GetStderr(); output != nil {
		return &SandboxExecChunk{Stream: "stderr", Data: append([]byte(nil), output.GetData()...)}, nil
	}
	if exit := event.GetExit(); exit != nil {
		s.exit, s.done, s.hasExit = int(exit.GetExitCode()), true, true
		return nil, io.EOF
	}
	return nil, fmt.Errorf("exec stream returned an empty event")
}

func (s *gatewayExecOutputStream) ExitCode() (int, error) {
	if !s.done {
		for {
			_, err := s.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return -1, err
			}
		}
	}
	if !s.hasExit {
		return -1, fmt.Errorf("exec stream ended without exit event")
	}
	return s.exit, nil
}

func (s *gatewayExecOutputStream) Close() error {
	s.cancel()
	return nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

type gatewayInteractiveStream interface {
	Send(*pb.ExecSandboxInput) error
	Recv() (*pb.ExecSandboxEvent, error)
	CloseSend() error
}

type mainProcessTTYClientStream struct {
	stream grpc.BidiStreamingClient[runtimev1.MainProcessServiceAttachTTYRequest, runtimev1.MainProcessServiceAttachTTYResponse]
}

func (s *mainProcessTTYClientStream) Send(input *pb.ExecSandboxInput) error {
	switch payload := input.GetPayload().(type) {
	case *pb.ExecSandboxInput_Stdin:
		return s.stream.Send(&runtimev1.MainProcessServiceAttachTTYRequest{Payload: &runtimev1.MainProcessServiceAttachTTYRequest_Stdin{Stdin: append([]byte(nil), payload.Stdin...)}})
	case *pb.ExecSandboxInput_Resize:
		return s.stream.Send(&runtimev1.MainProcessServiceAttachTTYRequest{Payload: &runtimev1.MainProcessServiceAttachTTYRequest_Resize{Resize: &runtimev1.MainProcessTTYResize{Cols: payload.Resize.GetCols(), Rows: payload.Resize.GetRows()}}})
	default:
		return fmt.Errorf("main-process TTY stream accepts only stdin and resize")
	}
}

func (s *mainProcessTTYClientStream) Recv() (*pb.ExecSandboxEvent, error) {
	event, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	switch payload := event.GetPayload().(type) {
	case *runtimev1.MainProcessServiceAttachTTYResponse_Stdout:
		return &pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Stdout{Stdout: &pb.ExecSandboxStdout{Data: append([]byte(nil), payload.Stdout...)}}}, nil
	case *runtimev1.MainProcessServiceAttachTTYResponse_ExitCode:
		return &pb.ExecSandboxEvent{Payload: &pb.ExecSandboxEvent_Exit{Exit: &pb.ExecSandboxExit{ExitCode: payload.ExitCode}}}, nil
	default:
		return &pb.ExecSandboxEvent{}, nil
	}
}

func (s *mainProcessTTYClientStream) CloseSend() error { return s.stream.CloseSend() }

// AttachMainProcessTTY attaches to the retained canonical PTY created during
// sandbox startup. It uses the Quarshell extension service while leaving the
// pinned OpenShell gateway descriptor unchanged.
func (c *Client) AttachMainProcessTTY(ctx context.Context, workspace, name string, cols, rows uint32) (SandboxInteractiveOutput, error) {
	if workspace == "" || name == "" {
		return nil, fmt.Errorf("usage: AttachMainProcessTTY(workspace, name, cols, rows)")
	}
	connection, err := c.gatewayConnection()
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := runtimev1.NewMainProcessServiceClient(connection).AttachTTY(streamCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open main-process TTY attachment in sandbox %q: %w", name, err)
	}
	if err := stream.Send(&runtimev1.MainProcessServiceAttachTTYRequest{Payload: &runtimev1.MainProcessServiceAttachTTYRequest_Start{Start: &runtimev1.MainProcessTTYStart{Workspace: workspace, Sandbox: name, Cols: cols, Rows: rows}}}); err != nil {
		cancel()
		return nil, fmt.Errorf("start main-process TTY attachment in sandbox %q: %w", name, err)
	}
	return newGatewayInteractiveSession(streamCtx, cancel, &mainProcessTTYClientStream{stream: stream}), nil
}

// SignalMainProcess sends an allowlisted signal to a sandbox's canonical
// workload process through the Quarshell runtime extension.
func (c *Client) SignalMainProcess(ctx context.Context, workspace, name string, signal runtimev1.MainProcessSignal) error {
	if workspace == "" || name == "" || signal < runtimev1.MainProcessSignal_MAIN_PROCESS_SIGNAL_TERMINATE || signal > runtimev1.MainProcessSignal_MAIN_PROCESS_SIGNAL_HANGUP {
		return fmt.Errorf("usage: SignalMainProcess(workspace, name, supported signal)")
	}
	connection, err := c.gatewayConnection()
	if err != nil {
		return err
	}
	_, err = runtimev1.NewMainProcessServiceClient(connection).Signal(ctx, &runtimev1.MainProcessServiceSignalRequest{
		Workspace: workspace, Sandbox: name, Signal: signal,
	})
	if err != nil {
		return fmt.Errorf("signal main process in sandbox %q: %w", name, err)
	}
	return nil
}

type gatewayInteractiveSession struct {
	stream gatewayInteractiveStream
	cancel context.CancelFunc

	sendMu      sync.Mutex
	inputClosed bool
	closeOnce   sync.Once
	closeErr    error

	dataCh chan *SandboxExecChunk
	done   chan struct{}
	errMu  sync.Mutex
	err    error
	buf    []byte

	exitCode    int
	hasExitCode bool
}

func newGatewayInteractiveSession(ctx context.Context, cancel context.CancelFunc, stream gatewayInteractiveStream) *gatewayInteractiveSession {
	session := &gatewayInteractiveSession{stream: stream, cancel: cancel, dataCh: make(chan *SandboxExecChunk, 64), done: make(chan struct{})}
	go session.readLoop(ctx)
	return session
}

func (s *gatewayInteractiveSession) readLoop(ctx context.Context) {
	defer s.cancel()
	defer close(s.dataCh)
	defer close(s.done)
	for {
		event, err := s.stream.Recv()
		if err != nil {
			if err != io.EOF {
				s.setError(err)
			} else if !s.hasExitCode {
				s.setError(fmt.Errorf("interactive exec stream ended without exit event"))
			}
			return
		}
		if output := event.GetStdout(); output != nil {
			if !s.sendOutput(ctx, SandboxExecStdout, output.GetData()) {
				return
			}
			continue
		}
		if output := event.GetStderr(); output != nil {
			if !s.sendOutput(ctx, SandboxExecStderr, output.GetData()) {
				return
			}
			continue
		}
		if exit := event.GetExit(); exit != nil {
			s.exitCode = int(exit.GetExitCode())
			s.hasExitCode = true
			return
		}
		s.setError(fmt.Errorf("interactive exec returned an empty event"))
		return
	}
}

func (s *gatewayInteractiveSession) sendOutput(ctx context.Context, stream SandboxExecStreamType, data []byte) bool {
	chunk := &SandboxExecChunk{Stream: stream, Data: append([]byte(nil), data...)}
	select {
	case s.dataCh <- chunk:
		return true
	case <-ctx.Done():
		s.setError(ctx.Err())
		return false
	}
}

func (s *gatewayInteractiveSession) setError(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *gatewayInteractiveSession) getError() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *gatewayInteractiveSession) Read(p []byte) (int, error) {
	if len(s.buf) > 0 {
		n := copy(p, s.buf)
		s.buf = s.buf[n:]
		return n, nil
	}
	chunk, err := s.NextOutput()
	if err != nil {
		return 0, err
	}
	data := chunk.Data
	n := copy(p, data)
	if n < len(data) {
		s.buf = append(s.buf[:0], data[n:]...)
	}
	return n, nil
}

// NextOutput returns the next stdout/stderr frame without merging the streams.
func (s *gatewayInteractiveSession) NextOutput() (*SandboxExecChunk, error) {
	chunk, ok := <-s.dataCh
	if !ok {
		if err := s.getError(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return chunk, nil
}

func (s *gatewayInteractiveSession) Write(data []byte) (int, error) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.inputClosed {
		return 0, io.ErrClosedPipe
	}
	if err := s.stream.Send(&pb.ExecSandboxInput{Payload: &pb.ExecSandboxInput_Stdin{Stdin: append([]byte(nil), data...)}}); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *gatewayInteractiveSession) Resize(cols, rows uint32) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.inputClosed {
		return io.ErrClosedPipe
	}
	if err := s.stream.Send(&pb.ExecSandboxInput{Payload: &pb.ExecSandboxInput_Resize{Resize: &pb.ExecSandboxWindowResize{Cols: cols, Rows: rows}}}); err != nil {
		return err
	}
	return nil
}

func (s *gatewayInteractiveSession) ExitCode() (int, error) {
	<-s.done
	if !s.hasExitCode {
		return -1, s.getError()
	}
	return s.exitCode, s.getError()
}

func (s *gatewayInteractiveSession) CloseWrite() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.inputClosed {
		return nil
	}
	s.inputClosed = true
	return s.stream.CloseSend()
}

func (s *gatewayInteractiveSession) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.closeErr = s.CloseWrite()
		<-s.done
	})
	return s.closeErr
}
