// Package klorq is the Go SDK for the pinned OpenShell Gateway API.
// Native Gateway RPCs use bearer authentication. Health probing remains HTTP
// because OpenShell exposes /healthz as an HTTP route.
//
// Exec runs over the pinned OpenShell ExecSandbox gRPC method.
// Interactive sessions and IDE access use SSH sessions: CreateSSHSession plus
// `quarshell ssh-proxy` / `quarshell sandbox connect`.
package klorq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
	"google.golang.org/grpc"
)

// EnvToken is the default bearer for New.
const EnvToken = "QUARSHELL_GATEWAY_TOKEN"

// ErrSandboxNotReady is returned when the sandbox supervisor relay is not connected.
var ErrSandboxNotReady = gc.ErrSandboxNotReady

// Client exposes native OpenShell Gateway RPC methods and an HTTP health probe.
type Client struct {
	http        *gc.Client
	base        string
	rpcEndpoint string
	tokenValue  atomic.Value
	edgeToken   string
	// Workspace selects the default workspace for resource-oriented methods.
	Workspace   string
	insecureTLS bool
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string
	tokenMu     sync.Mutex
	rpcMu       sync.Mutex
	gatewayConn *grpc.ClientConn
}

// SetInsecureSkipVerify disables gateway TLS certificate verification for this
// client. Configure it before the first HTTP or RPC operation.
func (c *Client) SetInsecureSkipVerify(skip bool) {
	if c == nil {
		return
	}
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	if c.gatewayConn != nil {
		return
	}
	c.insecureTLS = skip
	if c.http != nil {
		c.http.SetInsecureSkipVerify(skip)
	}
}

// SetGRPCEndpoint configures the native Gateway RPC authority independently
// from the HTTP base URL. The endpoint may be an http(s) URL or host:port.
// Configure it before the first RPC call.
// SetTLSCertificate configures a client certificate and optional CA bundle.
func (c *Client) SetTLSCertificate(certFile, keyFile, caFile string) error {
	if c == nil {
		return nil
	}
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	if c.gatewayConn != nil {
		return nil
	}
	c.tlsCertFile, c.tlsKeyFile, c.tlsCAFile = certFile, keyFile, caFile
	if c.http != nil {
		if err := c.http.SetTLSCertificate(certFile, keyFile, caFile); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SetGRPCEndpoint(endpoint string) {
	if c == nil {
		return
	}
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	if c.gatewayConn == nil {
		c.rpcEndpoint = strings.TrimSpace(endpoint)
	}
}

func (c *Client) workspace() string {
	if c.Workspace != "" {
		return c.Workspace
	}
	return defaultWorkspace
}

// New builds a client for a gateway base URL (e.g. http://127.0.0.1:7443),
// authenticated with $QUARSHELL_GATEWAY_TOKEN when set.
func New(baseURL string) *Client {
	return NewWithToken(baseURL, os.Getenv(EnvToken))
}

// NewWithToken returns a client with bearer auth.
func NewWithToken(base, token string) *Client {
	token = strings.TrimSpace(token)
	client := &Client{http: gc.NewWithToken(base, token), base: strings.TrimRight(base, "/")}
	client.tokenValue.Store(token)
	return client
}

// SetToken replaces the Gateway bearer used by subsequent HTTP and gRPC calls.
func (c *Client) SetToken(token string) {
	if c == nil {
		return
	}
	token = strings.TrimSpace(token)
	c.tokenMu.Lock()
	c.tokenValue.Store(token)
	if c.http != nil {
		c.http.SetToken(token)
	}
	c.tokenMu.Unlock()
}

func (c *Client) authToken() string {
	if c == nil {
		return ""
	}
	if value := c.tokenValue.Load(); value != nil {
		return value.(string)
	}
	return ""
}

// SetEdgeToken configures the Cloudflare token used on OpenShell gRPC metadata.
// Configure it before the first RPC operation.
func (c *Client) SetEdgeToken(token string) {
	if c == nil {
		return
	}
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	if c.gatewayConn != nil {
		return
	}
	c.edgeToken = strings.TrimSpace(token)
}

// Close releases the native gRPC connection opened for RPC-backed methods.
func (c *Client) Close() error {
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	var closeErr error
	if c.gatewayConn != nil {
		closeErr = errors.Join(closeErr, c.gatewayConn.Close())
		c.gatewayConn = nil
	}
	return closeErr
}

// Stable type aliases for quarshell gateway resources.
type (
	Labels                = gc.Labels
	Credentials           = gc.Credentials
	ProviderConfig        = gc.ProviderConfig
	RefreshMaterial       = gc.RefreshMaterial
	CredentialBindings    = gc.CredentialBindings
	CredentialExpiry      = gc.CredentialExpiry
	Sandbox               = gc.Sandbox
	ServiceExposure       = gc.ServiceExposure
	ExecResult            = gc.ExecResult
	LogLine               = gc.LogLine
	ProviderRecord        = gc.ProviderRecord
	ProviderRefreshConfig = gc.ProviderRefreshConfig
	PolicyRevisionMeta    = gc.PolicyRevisionMeta
	ServiceRecord         = gc.ServiceRecord
	SSHSession            = gc.SSHSession
	SSHSessionInfo        = gc.SSHSessionInfo
)

// SandboxExecOptions configures streamed and interactive sandbox execution.
type SandboxExecOptions struct {
	Env               map[string]string
	WorkDir           string
	NoLoginShell      bool
	AttachMainProcess bool
}

// SandboxExecChunk is one stdout or stderr frame from streamed execution.
type SandboxExecChunk struct {
	Stream SandboxExecStreamType
	Data   []byte
}

// SandboxExecStreamType identifies an output stream.
type SandboxExecStreamType string

const (
	SandboxExecStdout SandboxExecStreamType = "stdout"
	SandboxExecStderr SandboxExecStreamType = "stderr"
)

// SandboxExecStream yields output frames and the final process status.
type SandboxExecStream interface {
	Next() (*SandboxExecChunk, error)
	ExitCode() (int, error)
	Close() error
}

// SandboxInteractiveSession provides bidirectional terminal I/O.
type SandboxInteractiveSession interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows uint32) error
	ExitCode() (int, error)
	Close() error
}

// SandboxInteractiveOutput exposes stream identity alongside the usual
// io.Reader view used by terminal consumers.
type SandboxInteractiveOutput interface {
	SandboxInteractiveSession
	NextOutput() (*SandboxExecChunk, error)
}

// Create creates a runtime sandbox through the authorized OpenShell Gateway RPC.
func (c *Client) Create(ctx context.Context, sb Sandbox) error {
	if sb.Workspace == "" {
		sb.Workspace = c.workspace()
	}
	_, err := c.CreateControlSandbox(ctx, sb, nil)
	return err
}

// List returns caller-visible sandboxes through the allowlisted control API.
func (c *Client) List(ctx context.Context) ([]Sandbox, error) {
	return c.ListControlSandboxes(ctx, "", true)
}

// Get returns one redacted sandbox summary by name in the default workspace.
func (c *Client) Get(ctx context.Context, name string) (Sandbox, error) {
	return c.GetControlSandbox(ctx, c.workspace(), name)
}

// Delete removes a runtime sandbox using its current resource version.
func (c *Client) Delete(ctx context.Context, name string) error {
	return c.DeleteControlSandbox(ctx, c.workspace(), name)
}

// Start starts a runtime sandbox in the default workspace.
func (c *Client) Start(ctx context.Context, name string) (Sandbox, error) {
	return c.StartControlSandbox(ctx, c.workspace(), name)
}

// Stop stops a runtime sandbox in the default workspace.
func (c *Client) Stop(ctx context.Context, name string) (Sandbox, error) {
	return c.StopControlSandbox(ctx, c.workspace(), name)
}

// Exec runs argv in the sandbox using the generated OpenShell gateway RPC.
func (c *Client) Exec(ctx context.Context, name string, argv ...string) (ExecResult, error) {
	if name == "" || len(argv) == 0 {
		return ExecResult{}, fmt.Errorf("usage: Exec(name, argv...)")
	}
	stream, err := c.execStreamingWithTimeout(ctx, c.workspace(), name, argv, 0, SandboxExecOptions{})
	if err != nil {
		return ExecResult{}, err
	}
	defer stream.Close()
	var output strings.Builder
	for {
		chunk, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return ExecResult{}, fmt.Errorf("execute in sandbox %q: %w", name, nextErr)
		}
		if chunk != nil {
			_, _ = output.Write(chunk.Data)
		}
	}
	exitCode, err := stream.ExitCode()
	if err != nil {
		return ExecResult{}, fmt.Errorf("execute in sandbox %q: %w", name, err)
	}
	return ExecResult{ExitCode: exitCode, Output: output.String()}, nil
}

// ExecStreaming starts a server-streamed command in the selected workspace.
// Callers must drain the stream and close it when finished.
func (c *Client) ExecStreaming(ctx context.Context, workspace, name string, argv []string, options SandboxExecOptions) (SandboxExecStream, error) {
	return c.execStreamingWithTimeout(ctx, workspace, name, argv, 0, options)
}

// ExecStreamingWithTimeout runs stream-mode exec with an optional server-side
// process deadline in addition to the caller context deadline.
func (c *Client) ExecStreamingWithTimeout(ctx context.Context, workspace, name string, argv []string, timeout time.Duration, options SandboxExecOptions) (SandboxExecStream, error) {
	return c.execStreamingWithTimeout(ctx, workspace, name, argv, timeout, options)
}

// ExecInteractive starts a bidirectional terminal command in the selected workspace.
// The pinned OpenShell SDK currently requests a PTY for every interactive session.
func (c *Client) ExecInteractive(ctx context.Context, workspace, name string, argv []string, cols, rows uint32, options SandboxExecOptions) (SandboxInteractiveSession, error) {
	if name == "" || len(argv) == 0 {
		return nil, fmt.Errorf("usage: ExecInteractive(workspace, name, argv, options)")
	}
	return c.ExecInteractiveWithTTY(ctx, workspace, name, argv, true, cols, rows, options)
}

// CloseSandboxExecInput closes stdin while allowing interactive output to drain.
func CloseSandboxExecInput(session SandboxInteractiveSession) error {
	if closer, ok := session.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return fmt.Errorf("quarshell-sdk: interactive session does not support closing input independently")
}

// Healthz is the unauthenticated HTTP readiness probe used during CLI bootstrap.
func (c *Client) Healthz(ctx context.Context) (map[string]any, error) { return c.http.Healthz(ctx) }

// Info returns authorized gateway diagnostics over OpenShell Gateway RPCs.
func (c *Client) Info(ctx context.Context) (map[string]any, error) { return c.gatewayInfo(ctx) }

// DeleteSandbox deletes a sandbox through the OpenShell lifecycle API.
func (c *Client) DeleteSandbox(ctx context.Context, name string) error {
	return c.DeleteControlSandbox(ctx, c.workspace(), name)
}

// ListSandboxes returns all caller-visible OpenShell sandboxes in this client's workspace.
func (c *Client) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	result := make([]Sandbox, 0)
	seenTokens := make(map[string]struct{})
	seenSandboxes := make(map[string]struct{})
	token := ""
	for {
		page, next, err := c.ListSandboxesPage(ctx, 1000, token, "", false)
		if err != nil {
			return nil, err
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenSandboxes, page, func(item Sandbox) string {
			return item.ID
		}, "sandboxes")
		if appendErr != nil {
			return nil, appendErr
		}
		token = next
		if token == "" {
			return result, nil
		}
		if _, exists := seenTokens[token]; exists {
			return nil, fmt.Errorf("list sandboxes: gateway repeated a pagination token")
		}
		seenTokens[token] = struct{}{}
	}
}

// GetSandbox returns metadata for one OpenShell sandbox.
func (c *Client) GetSandbox(ctx context.Context, name string) (Sandbox, error) {
	return c.GetControlSandbox(ctx, c.workspace(), name)
}
