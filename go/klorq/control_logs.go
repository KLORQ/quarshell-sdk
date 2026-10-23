package klorq

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Client) GetControlSandboxLogs(ctx context.Context, workspace, name string, limit uint32) ([]LogLine, error) {
	return c.GetControlSandboxLogsFiltered(ctx, workspace, name, limit, "", "", "")
}

// GetControlSandboxLogsFiltered returns a bounded tail with optional time,
// source, and level filters. since accepts a Go duration or RFC3339 timestamp.
func (c *Client) GetControlSandboxLogsFiltered(ctx context.Context, workspace, name string, limit uint32, since, source, level string) ([]LogLine, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	if workspace == "" {
		workspace = "default"
	}
	if limit == 0 {
		limit = 200
	} else if limit > 1000 {
		limit = 1000
	}
	request := &gatewayv1.GetSandboxLogsRequest{
		WorkspaceScope: workspaceSelector(workspace), Sandbox: name, Lines: limit,
	}
	if sinceMS := parseLogSince(since); sinceMS > 0 {
		request.SinceTime = timestamppb.New(time.UnixMilli(sinceMS))
	}
	if source != "" {
		request.Sources = []string{source}
	}
	response, err := client.GetSandboxLogs(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get sandbox logs %q: %w", name, err)
	}
	lines := make([]LogLine, 0, len(response.GetLogs()))
	for _, line := range response.GetLogs() {
		if line == nil || level != "" && !controlLogLevelMatches(line.GetLevel(), level) {
			continue
		}
		ts := time.Time{}
		if line.GetEventTime() != nil {
			ts = line.GetEventTime().AsTime()
		}
		lines = append(lines, LogLine{
			TS: ts, Source: line.GetSource(),
			Level: line.GetLevel(), Text: line.GetMessage(),
		})
	}
	return lines, nil
}

func controlLogLevelMatches(got, want string) bool {
	if got == want {
		return true
	}
	switch strings.ToLower(want) {
	case "warn", "warning":
		return got == "MED" || got == "WARN" || got == "HIGH"
	case "error":
		return got == "HIGH" || got == "CRIT" || got == "FATAL" || got == "ERROR"
	case "debug", "info":
		return got == "INFO" || got == "LOW" || got == "OCSF" || got == ""
	default:
		return false
	}
}

// WatchControlSandboxLogs follows the gateway's bounded log stream. The
// gateway sends a fresh bounded tail on a new watch and the caller should
// reconnect with a new watch after a stream error.
func (c *Client) WatchControlSandboxLogs(ctx context.Context, workspace, name string, initialLimit uint32, w io.Writer) error {
	return c.WatchControlSandboxLogsFiltered(ctx, workspace, name, initialLimit, "", "", "", w)
}

// WatchControlSandboxLogsFiltered follows a filtered workspace-scoped log stream.
func (c *Client) WatchControlSandboxLogsFiltered(ctx context.Context, workspace, name string, initialLimit uint32, since, source, level string, w io.Writer) error {
	return c.watchControlSandboxLogs(ctx, workspace, name, initialLimit, parseLogSince(since), source, level, "", w)
}

// WatchControlSandboxSet follows every supplied sandbox concurrently.
func (c *Client) WatchControlSandboxSet(ctx context.Context, sandboxes []Sandbox, initialLimit uint32, since, source, level string, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("watch control sandbox logs: writer required")
	}
	if len(sandboxes) == 0 {
		return nil
	}
	if len(sandboxes) > maxControlLogWatches {
		return fmt.Errorf("watch control sandbox logs: %d sandboxes exceed the concurrent watch limit of %d", len(sandboxes), maxControlLogWatches)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var writers sync.Mutex
	var wait sync.WaitGroup
	errCh := make(chan error, len(sandboxes))
	for _, sandbox := range sandboxes {
		sandbox := sandbox
		wait.Add(1)
		go func() {
			defer wait.Done()
			label := sandbox.Workspace + "/" + sandbox.Name
			err := c.watchControlSandboxLogs(watchCtx, sandbox.Workspace, sandbox.Name, initialLimit, parseLogSince(since), source, level, label, writerLocked{mu: &writers, w: w})
			if err != nil && watchCtx.Err() == nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
			}
		}()
	}
	wait.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

type writerLocked struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w writerLocked) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (c *Client) watchControlSandboxLogs(ctx context.Context, workspace, name string, initialLimit uint32, sinceUnixMS int64, source, level, label string, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("watch control sandbox logs: writer required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if workspace == "" {
		workspace = "default"
	}
	if initialLimit == 0 {
		initialLimit = 200
	} else if initialLimit > 1000 {
		initialLimit = 1000
	}
	request := &gatewayv1.WatchSandboxRequest{
		WorkspaceScope: workspaceSelector(workspace), Sandbox: name,
		FollowLogs: true, LogTailLines: initialLimit,
	}
	if sinceUnixMS > 0 {
		request.SinceTime = timestamppb.New(time.UnixMilli(sinceUnixMS))
	}
	if source != "" {
		request.LogSources = []string{source}
	}
	stream, err := client.WatchSandbox(ctx, request)
	if err != nil {
		return fmt.Errorf("watch sandbox logs %q: %w", name, err)
	}
	for {
		event, recvErr := stream.Recv()
		if recvErr == io.EOF {
			return nil
		}
		if recvErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("watch sandbox logs %q: %w", name, recvErr)
		}
		line := event.GetLog()
		if line == nil || level != "" && !controlLogLevelMatches(line.GetLevel(), level) {
			continue
		}
		prefix := line.GetSource()
		if prefix == "" {
			prefix = "gateway"
		}
		if label != "" {
			prefix = label + " " + prefix
		}
		if _, err := fmt.Fprintf(w, "[%s] %s\n", prefix, line.GetMessage()); err != nil {
			return err
		}
	}
}

func parseLogSince(since string) int64 {
	if since == "" {
		return 0
	}
	if duration, err := time.ParseDuration(since); err == nil {
		return time.Now().UTC().Add(-duration).UnixMilli()
	}
	if timestamp, err := time.Parse(time.RFC3339Nano, since); err == nil {
		return timestamp.UnixMilli()
	}
	return 0
}
