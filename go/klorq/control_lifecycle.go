package klorq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	sandboxv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/sandbox/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

const maxControlLogWatches = 24

// ControlMutationResult identifies a lifecycle request so callers can recover
// its authoritative outcome after a timeout or broken connection.
type ControlMutationResult struct {
	Sandbox     Sandbox
	ServiceURLs map[string]string
	RequestID   string
	Deleted     bool
}

// CreateControlSandbox creates a runtime sandbox through the authorized control RPC.
func (c *Client) CreateControlSandbox(ctx context.Context, sandbox Sandbox, command []string) (Sandbox, error) {
	result, err := c.CreateControlSandboxOperation(ctx, sandbox, command)
	return result.Sandbox, err
}

// CreateControlSandboxOperation creates a sandbox and returns durable request
// identifiers even when the RPC outcome is uncertain.
func (c *Client) CreateControlSandboxOperation(ctx context.Context, sandbox Sandbox, command []string) (ControlMutationResult, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return ControlMutationResult{}, err
	}
	workspace := sandbox.Workspace
	if workspace == "" {
		workspace = c.workspace()
	}
	if sandbox.WorkloadTemplateName != "" && (sandbox.Image != "" || sandbox.CPUMillis != 0 || sandbox.MemoryBytes != 0 || sandbox.GPURequested || sandbox.GPUCount != 0 || sandbox.DriverConfigJSON != "" || len(sandbox.Environment) != 0) {
		return ControlMutationResult{}, fmt.Errorf("sandbox workload template cannot be combined with inline workload options")
	}
	if sandbox.PidsLimit != 0 {
		return ControlMutationResult{}, fmt.Errorf("sandbox pids limit is Gateway driver configuration, not a per-sandbox OpenShell field")
	}
	if sandbox.BasePolicyYAML != "" {
		return ControlMutationResult{}, fmt.Errorf("sandbox base_policy_yaml is a legacy Quarshell field; use policy_yaml for OpenShell sandbox policy")
	}
	serviceExposures, err := openShellServiceExposures(sandbox.ServiceExposures)
	if err != nil {
		return ControlMutationResult{}, err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return ControlMutationResult{}, err
	}
	result := ControlMutationResult{RequestID: requestID}
	template, err := openShellSandboxTemplate(sandbox)
	if err != nil {
		return ControlMutationResult{}, err
	}
	var policy *sandboxv1.SandboxPolicy
	if sandbox.PolicyYAML != "" {
		policy, err = parseSandboxPolicyYAML([]byte(sandbox.PolicyYAML))
		if err != nil {
			return ControlMutationResult{}, fmt.Errorf("sandbox policy: %w", err)
		}
	}
	restartPolicy, err := openShellRestartPolicy(sandbox.RestartPolicy)
	if err != nil {
		return ControlMutationResult{}, err
	}
	spec := &gatewayv1.SandboxSpec{Template: template, Environment: sandbox.Environment, Command: append([]string(nil), command...), Tty: sandbox.TTY, Policy: policy, Providers: append([]string(nil), sandbox.AttachedProviders...), RestartPolicy: restartPolicy}
	if sandbox.GPURequested || sandbox.GPUCount > 0 {
		count := sandbox.GPUCount
		gpu := &gatewayv1.GpuResourceRequirements{}
		if count > 0 {
			gpu.Count = &count
		}
		spec.ResourceRequirements = &gatewayv1.ResourceRequirements{Gpu: gpu}
	}
	if len(sandbox.CDIDevices) > 0 {
		return ControlMutationResult{}, fmt.Errorf("sandbox CDI device selection must be expressed in driver_config JSON")
	}
	annotations := map[string]string{}
	if sandbox.Ephemeral {
		annotations["openshell.nvidia.com/retention"] = "ephemeral"
	}
	response, err := client.CreateSandbox(ctx, &gatewayv1.CreateSandboxRequest{
		WorkspaceScope: workspaceSelector(workspace), Name: sandbox.Name, Labels: sandbox.Labels,
		WorkloadTemplate: sandbox.WorkloadTemplateName, Spec: spec, RequestId: requestID, Annotations: annotations,
		ServiceExposures: serviceExposures, AwaitMainProcessAttachment: sandbox.AwaitMainProcessAttachment,
	})
	if err != nil {
		return result, fmt.Errorf("create sandbox %q (request %s): %w", sandbox.Name, requestID, err)
	}
	result.Sandbox = sandboxFromOpenShell(response.GetSandbox())
	result.ServiceURLs = response.GetServiceUrls()
	return result, nil
}

func openShellRestartPolicy(value string) (gatewayv1.SandboxRestartPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "never":
		return gatewayv1.SandboxRestartPolicy_SANDBOX_RESTART_POLICY_NEVER, nil
	case "on-failure":
		return gatewayv1.SandboxRestartPolicy_SANDBOX_RESTART_POLICY_ON_FAILURE, nil
	case "always":
		return gatewayv1.SandboxRestartPolicy_SANDBOX_RESTART_POLICY_ALWAYS, nil
	default:
		return gatewayv1.SandboxRestartPolicy_SANDBOX_RESTART_POLICY_UNSPECIFIED, fmt.Errorf("unsupported sandbox restart policy %q", value)
	}
}

func openShellSandboxTemplate(sandbox Sandbox) (*gatewayv1.SandboxTemplate, error) {
	if sandbox.WorkloadTemplateName != "" {
		return nil, nil
	}
	template := &gatewayv1.SandboxTemplate{Image: sandbox.Image}
	if sandbox.DriverConfigJSON != "" {
		var values map[string]any
		if err := json.Unmarshal([]byte(sandbox.DriverConfigJSON), &values); err != nil {
			return nil, fmt.Errorf("sandbox driver_config JSON: %w", err)
		}
		driverConfig, err := structpb.NewStruct(values)
		if err != nil {
			return nil, fmt.Errorf("sandbox driver_config JSON: %w", err)
		}
		template.DriverConfig = driverConfig
	}
	limits := map[string]any{}
	if sandbox.CPUMillis > 0 {
		limits["cpu"] = strconv.FormatUint(uint64(sandbox.CPUMillis), 10) + "m"
	}
	if sandbox.MemoryBytes > 0 {
		limits["memory"] = strconv.FormatUint(sandbox.MemoryBytes, 10)
	}
	if len(limits) > 0 {
		resources, err := structpb.NewStruct(map[string]any{"limits": limits})
		if err != nil {
			return nil, err
		}
		template.Resources = resources
	}
	return template, nil
}

func openShellServiceExposures(items []ServiceExposure) ([]*gatewayv1.SandboxServiceExposure, error) {
	if len(items) == 0 {
		return nil, nil
	}
	result := make([]*gatewayv1.SandboxServiceExposure, 0, len(items))
	for _, item := range items {
		mode, err := openShellServiceAuthorizationMode(item.AuthorizationMode)
		if err != nil {
			return nil, err
		}
		result = append(result, &gatewayv1.SandboxServiceExposure{
			Service: item.Name, TargetPort: item.TargetPort, AuthorizationMode: mode,
		})
	}
	return result, nil
}

func openShellServiceAuthorizationMode(value string) (gatewayv1.ServiceAuthorizationMode, error) {
	switch value {
	case "", ServiceAuthorizationModeUnspecified, ServiceAuthorizationModeStrip, "strip":
		return gatewayv1.ServiceAuthorizationMode_SERVICE_AUTHORIZATION_MODE_STRIP, nil
	case ServiceAuthorizationModeBearerPassthrough, "bearer-passthrough", "bearer_passthrough":
		return gatewayv1.ServiceAuthorizationMode_SERVICE_AUTHORIZATION_MODE_BEARER_PASSTHROUGH, nil
	default:
		return gatewayv1.ServiceAuthorizationMode_SERVICE_AUTHORIZATION_MODE_UNSPECIFIED, fmt.Errorf("unsupported service authorization mode %q", value)
	}
}

// StartControlSandbox starts one caller-visible runtime sandbox.
func (c *Client) StartControlSandbox(ctx context.Context, workspace, name string) (Sandbox, error) {
	result, err := c.ChangeControlSandboxStateOperation(ctx, workspace, name, "start")
	return result.Sandbox, err
}

// StopControlSandbox stops one caller-visible runtime sandbox.
func (c *Client) StopControlSandbox(ctx context.Context, workspace, name string) (Sandbox, error) {
	result, err := c.ChangeControlSandboxStateOperation(ctx, workspace, name, "stop")
	return result.Sandbox, err
}

// DeleteControlSandbox deletes one caller-visible runtime sandbox.
func (c *Client) DeleteControlSandbox(ctx context.Context, workspace, name string) error {
	_, err := c.DeleteControlSandboxOperation(ctx, workspace, name)
	return err
}

// DeleteControlSandboxOperation deletes a sandbox and exposes its operation ID.
func (c *Client) DeleteControlSandboxOperation(ctx context.Context, workspace, name string) (ControlMutationResult, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return ControlMutationResult{}, err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return ControlMutationResult{}, err
	}
	result := ControlMutationResult{RequestID: requestID}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.DeleteSandbox(ctx, &gatewayv1.DeleteSandboxRequest{
		WorkspaceScope: workspaceSelector(workspace), Name: name, RequestId: requestID,
	})
	if err != nil {
		return result, fmt.Errorf("delete control sandbox %q (request %s): %w", name, requestID, err)
	}
	result.Deleted = response.GetOutcome() != gatewayv1.DeletionOutcome_DELETION_OUTCOME_UNSPECIFIED
	return result, nil
}

// ChangeControlSandboxStateOperation starts or stops a sandbox and exposes the
// request and operation IDs used for conflict and timeout recovery.
func (c *Client) ChangeControlSandboxStateOperation(ctx context.Context, workspace, name, action string) (ControlMutationResult, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return ControlMutationResult{}, err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return ControlMutationResult{}, err
	}
	result := ControlMutationResult{RequestID: requestID}
	if workspace == "" {
		workspace = c.workspace()
	}
	scope := workspaceSelector(workspace)
	switch action {
	case "start":
		response, callErr := client.StartSandbox(ctx, &gatewayv1.StartSandboxRequest{WorkspaceScope: scope, Name: name, RequestId: requestID})
		if callErr != nil {
			return result, fmt.Errorf("start control sandbox %q (request %s): %w", name, requestID, callErr)
		}
		result.Sandbox = sandboxFromOpenShell(response.GetSandbox())
		return result, nil
	case "stop":
		response, callErr := client.StopSandbox(ctx, &gatewayv1.StopSandboxRequest{WorkspaceScope: scope, Name: name, RequestId: requestID})
		if callErr != nil {
			return result, fmt.Errorf("stop control sandbox %q (request %s): %w", name, requestID, callErr)
		}
		result.Sandbox = sandboxFromOpenShell(response.GetSandbox())
		return result, nil
	default:
		return result, fmt.Errorf("unsupported sandbox action %q", action)
	}
}

func newRequestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate sandbox request id: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func newOpenShellRequestID() (string, error) {
	id, err := newRequestID()
	if err != nil {
		return "", err
	}
	return id[:8] + "-" + id[8:12] + "-4" + id[13:16] + "-a" + id[17:20] + "-" + id[20:], nil
}

// ListControlSandboxes returns the caller-visible sandbox inventory through
// the allowlisted control API. An empty workspace selects the default
// workspace; allWorkspaces uses the catalog to enumerate accessible spaces.
