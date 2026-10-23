package klorq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	sandboxv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// GetGlobalPolicy reads the latest native OpenShell global policy revision.
func (c *Client) GetGlobalPolicy(ctx context.Context) ([]byte, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetSandboxPolicyStatus(ctx, &gatewayv1.GetSandboxPolicyStatusRequest{Global: true})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get global OpenShell policy: %w", err)
	}
	return sandboxPolicyYAML(response.GetRevision().GetPolicy())
}

// PutGlobalPolicy updates the full native OpenShell global policy.
func (c *Client) PutGlobalPolicy(ctx context.Context, document []byte) error {
	policy, err := parseSandboxPolicyYAML(document)
	if err != nil {
		return err
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return err
	}
	_, err = client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		Global: true, Policy: policy, RequestId: requestID,
	})
	if err != nil {
		return fmt.Errorf("update global OpenShell policy: %w", err)
	}
	return nil
}

// GetSandboxPolicy reads the latest native policy revision. OpenShell has one
// policy value per revision, so legacy base/full view labels resolve to it.
func (c *Client) GetSandboxPolicy(ctx context.Context, sandbox, view string) ([]byte, error) {
	view = strings.ToLower(strings.TrimSpace(view))
	if view == "" || view == "effective" {
		view = "full"
	}
	if view != "base" && view != "full" {
		return nil, fmt.Errorf("policy view must be base or full")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	response, err := client.GetSandboxPolicyStatus(ctx, &gatewayv1.GetSandboxPolicyStatusRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox,
	})
	if err != nil {
		return nil, fmt.Errorf("get OpenShell policy for sandbox %q: %w", sandbox, err)
	}
	return sandboxPolicyYAML(response.GetRevision().GetPolicy())
}

func (c *Client) EffectivePolicy(ctx context.Context, sandbox string) ([]byte, error) {
	return c.GetSandboxPolicy(ctx, sandbox, "full")
}

// PutSandboxPolicy applies a complete native OpenShell policy at sandbox scope.
func (c *Client) PutSandboxPolicy(ctx context.Context, sandbox string, document []byte) ([]byte, uint32, error) {
	policy, err := parseSandboxPolicyYAML(document)
	if err != nil {
		return nil, 0, err
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, 0, err
	}
	current, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Name: sandbox,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read sandbox resource version before policy update: %w", err)
	}
	requestID, err := newOpenShellRequestID()
	if err != nil {
		return nil, 0, err
	}
	updated, err := client.UpdateConfig(ctx, &gatewayv1.UpdateConfigRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox,
		Policy: policy, ExpectedResourceVersion: current.GetSandbox().GetMetadata().GetResourceVersion(),
		RequestId: requestID,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("update OpenShell policy for sandbox %q: %w", sandbox, err)
	}
	statusResponse, err := client.GetSandboxPolicyStatus(ctx, &gatewayv1.GetSandboxPolicyStatusRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Version: updated.GetVersion(),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read updated OpenShell policy for sandbox %q: %w", sandbox, err)
	}
	result, err := sandboxPolicyYAML(statusResponse.GetRevision().GetPolicy())
	return result, updated.GetVersion(), err
}

// GetPolicyRevisionStatus reads lifecycle status for one native policy revision.
func (c *Client) GetPolicyRevisionStatus(ctx context.Context, sandbox string, version uint32) (PolicyRevisionRecord, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return PolicyRevisionRecord{}, err
	}
	response, err := client.GetSandboxPolicyStatus(ctx, &gatewayv1.GetSandboxPolicyStatusRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Version: version,
	})
	if err != nil {
		return PolicyRevisionRecord{}, fmt.Errorf("get OpenShell policy revision status %q/%d: %w", sandbox, version, err)
	}
	if response.GetRevision() == nil {
		return PolicyRevisionRecord{}, fmt.Errorf("gateway returned no policy revision for %q/%d", sandbox, version)
	}
	return policyRevisionRecordFromGateway(response.GetRevision()), nil
}

func (c *Client) ListPolicyRevisions(ctx context.Context, sandbox string) ([]PolicyRevisionMeta, error) {
	revisions, err := c.ListPolicyRevisionsGateway(ctx, sandbox)
	if err != nil {
		return nil, err
	}
	result := make([]PolicyRevisionMeta, 0, len(revisions))
	for _, revision := range revisions {
		updatedAt := time.Time{}
		if revision.CreatedAt != nil {
			updatedAt = *revision.CreatedAt
		}
		status := strings.ToLower(strings.TrimPrefix(revision.Status, "POLICY_STATUS_"))
		result = append(result, PolicyRevisionMeta{Rev: int(revision.Revision), UpdatedAt: updatedAt, Status: status})
	}
	return result, nil
}

func (c *Client) GetPolicyRevision(ctx context.Context, sandbox string, revision int) (string, error) {
	if revision <= 0 {
		return "", fmt.Errorf("policy revision must be positive")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return "", err
	}
	response, err := client.GetSandboxPolicyStatus(ctx, &gatewayv1.GetSandboxPolicyStatusRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Version: uint32(revision),
	})
	if err != nil {
		return "", fmt.Errorf("get OpenShell policy revision %q/%d: %w", sandbox, revision, err)
	}
	data, err := sandboxPolicyYAML(response.GetRevision().GetPolicy())
	return string(data), err
}

func parseSandboxPolicyYAML(data []byte) (*sandboxv1.SandboxPolicy, error) {
	if len(data) == 0 {
		return &sandboxv1.SandboxPolicy{Version: 1}, nil
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("policy document must not exceed 1 MiB")
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse OpenShell policy YAML: %w", err)
	}
	if document == nil {
		return nil, fmt.Errorf("policy document must be a YAML mapping")
	}
	if filesystem, ok := document["filesystem_policy"]; ok {
		if _, exists := document["filesystem"]; exists {
			return nil, fmt.Errorf("policy document cannot contain both filesystem and filesystem_policy")
		}
		document["filesystem"] = filesystem
		delete(document, "filesystem_policy")
	}
	policyEnumNames(document, true)
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	result := &sandboxv1.SandboxPolicy{}
	if err := protojson.Unmarshal(encoded, result); err != nil {
		return nil, fmt.Errorf("decode OpenShell policy: %w", err)
	}
	if result.GetVersion() == 0 {
		result.Version = 1
	}
	return result, nil
}

func sandboxPolicyYAML(policy *sandboxv1.SandboxPolicy) ([]byte, error) {
	if policy == nil {
		return nil, fmt.Errorf("OpenShell policy response did not include a policy")
	}
	encoded, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(policy)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	policyEnumNames(document, false)
	return yaml.Marshal(document)
}

func policyEnumNames(value any, toProto bool) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if text, ok := child.(string); ok {
				if converted, ok := policyEnumValue(key, text, toProto); ok {
					item[key] = converted
					continue
				}
			}
			policyEnumNames(child, toProto)
		}
	case []any:
		for _, child := range item {
			policyEnumNames(child, toProto)
		}
	}
}

func policyEnumValue(key, value string, toProto bool) (string, bool) {
	if toProto {
		var prefix string
		switch key {
		case "tls":
			prefix = "NETWORK_TLS_MODE_"
		case "enforcement":
			prefix = "NETWORK_ENFORCEMENT_MODE_"
		case "access":
			prefix = "NETWORK_ACCESS_PRESET_"
		default:
			return "", false
		}
		normalized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(value, "-", "_"), " ", "_"))
		if normalized == "" {
			return "", false
		}
		return prefix + normalized, true
	}
	for _, prefix := range []string{"NETWORK_TLS_MODE_", "NETWORK_ENFORCEMENT_MODE_", "NETWORK_ACCESS_PRESET_"} {
		if strings.HasPrefix(value, prefix) {
			short := strings.ToLower(strings.TrimPrefix(value, prefix))
			if key == "access" {
				short = strings.ReplaceAll(short, "_", "-")
			}
			return short, true
		}
	}
	return "", false
}
