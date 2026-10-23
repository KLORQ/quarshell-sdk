package klorq

import (
	"context"
	"fmt"
	"strings"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetSandboxProviderStatus reads the redacted readiness status for one sandbox
// provider. An empty receiptID reconstructs the current desired state and
// records an OBSERVE receipt on the Gateway.
func (c *Client) GetSandboxProviderStatus(ctx context.Context, sandbox, provider, receiptID string) (gc.ProviderReadinessStatus, error) {
	if strings.TrimSpace(sandbox) == "" || strings.TrimSpace(provider) == "" {
		return gc.ProviderReadinessStatus{}, fmt.Errorf("sandbox and provider are required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return gc.ProviderReadinessStatus{}, err
	}
	response, err := client.GetSandboxProviderStatus(ctx, &gatewayv1.GetSandboxProviderStatusRequest{
		WorkspaceScope: workspaceSelector(c.workspace()),
		Sandbox:        sandbox,
		Provider:       provider,
		ReceiptId:      receiptID,
	})
	if err != nil {
		return gc.ProviderReadinessStatus{}, fmt.Errorf("get provider status for %q in sandbox %q: %w", provider, sandbox, err)
	}
	return providerReadinessStatusFromProto(response.GetStatus()), nil
}

func providerReadinessStatusFromProto(status *gatewayv1.ProviderReadinessStatus) gc.ProviderReadinessStatus {
	if status == nil {
		return gc.ProviderReadinessStatus{}
	}
	receipt := status.GetReceipt()
	result := gc.ProviderReadinessStatus{
		ReceiptID:         receipt.GetReceiptId(),
		MutationID:        receipt.GetMutationId(),
		Provider:          receipt.GetProvider(),
		Workspace:         receipt.GetWorkspace(),
		Kind:              receipt.GetKind().String(),
		State:             status.GetState().String(),
		Reason:            status.GetReason().String(),
		NetworkInstanceID: status.GetNetworkInstanceId(),
	}
	if observed := status.GetObserved(); observed != nil {
		result.Observed = &gc.ProviderReadinessObservation{
			SessionID:                  observed.GetSessionId(),
			Sequence:                   observed.GetSequence(),
			AttachmentEpoch:            observed.GetAttachmentEpoch(),
			ProviderEnvRevision:        observed.GetProviderEnvRevision(),
			ConfigRevision:             observed.GetConfigRevision(),
			PolicyHash:                 observed.GetPolicyHash(),
			CredentialsInstalled:       observed.GetCredentialsInstalled(),
			PolicyActive:               observed.GetPolicyActive(),
			LaunchEnvironmentInstalled: observed.GetLaunchEnvironmentInstalled(),
			ProcessInstanceID:          observed.GetProcessInstanceId(),
			Reason:                     observed.GetReason().String(),
		}
	}
	result.ObservedTime = protoTimestamp(result.ObservedTime, status.GetObservedTime())
	result.EvaluatedTime = protoTimestamp(result.EvaluatedTime, status.GetEvaluatedTime())
	return result
}

func protoTimestamp(current time.Time, value *timestamppb.Timestamp) time.Time {
	if value == nil || !value.IsValid() {
		return current
	}
	return value.AsTime()
}
