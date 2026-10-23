package klorq

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultWorkspace = "default"
const runtimeCredentialsAnnotation = "quarshell.io/runtime-credentials"

// ListProviders lists provider metadata through the pinned OpenShell contract.
// Credential values are never copied into the public SDK record.
func (c *Client) ListProviders(ctx context.Context) ([]ProviderRecord, error) {
	return c.listProvidersPages(ctx, false)
}

// ListProvidersAllWorkspaces lists provider metadata across workspaces. The
// Gateway requires platform-administrator authorization for this request.
func (c *Client) ListProvidersAllWorkspaces(ctx context.Context) ([]ProviderRecord, error) {
	return c.listProvidersPages(ctx, true)
}

func (c *Client) listProvidersPages(ctx context.Context, allWorkspaces bool) ([]ProviderRecord, error) {
	result := make([]ProviderRecord, 0)
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenProviders := map[string]struct{}{}
	for {
		page, err := c.ListProvidersPage(ctx, 1000, pageToken, "", allWorkspaces)
		if err != nil {
			return nil, err
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenProviders, page.Providers, func(item ProviderRecord) string {
			return item.Workspace + "\x00" + item.Name
		}, "providers")
		if appendErr != nil {
			return nil, appendErr
		}
		pageToken = page.NextPageToken
		if pageToken == "" {
			break
		}
		if _, seen := seenTokens[pageToken]; seen {
			return nil, fmt.Errorf("list providers: gateway repeated a pagination token")
		}
		seenTokens[pageToken] = struct{}{}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Workspace != result[j].Workspace {
			return result[i].Workspace < result[j].Workspace
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// ProviderPage contains one page of workspace-scoped provider instances.
type ProviderPage struct {
	Providers     []ProviderRecord `json:"providers"`
	NextPageToken string           `json:"next_page_token"`
}

// ListProvidersPage returns one Gateway page. Zero pageSize selects the server
// default; negative sizes are rejected before making an RPC.
func (c *Client) ListProvidersPage(ctx context.Context, pageSize int32, pageToken, labelSelector string, allWorkspaces bool) (ProviderPage, error) {
	if pageSize < 0 {
		return ProviderPage{}, fmt.Errorf("provider page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return ProviderPage{}, err
	}
	scope := workspaceSelector(c.workspace())
	if allWorkspaces {
		scope = &datamodelv1.WorkspaceSelector{Selection: &datamodelv1.WorkspaceSelector_AllWorkspaces{AllWorkspaces: &datamodelv1.AllWorkspaces{}}}
	}
	response, err := client.ListProviders(ctx, &gatewayv1.ListProvidersRequest{WorkspaceScope: scope, PageSize: pageSize, PageToken: pageToken, LabelSelector: labelSelector})
	if err != nil {
		return ProviderPage{}, fmt.Errorf("list providers: %w", err)
	}
	page := ProviderPage{Providers: make([]ProviderRecord, 0, len(response.GetProviders())), NextPageToken: response.GetNextPageToken()}
	for _, item := range response.GetProviders() {
		page.Providers = append(page.Providers, providerRecordFromProto(item))
	}
	return page, nil
}

// GetProvider reads provider metadata through the pinned OpenShell contract.
func (c *Client) GetProvider(ctx context.Context, name string) (ProviderRecord, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return ProviderRecord{}, err
	}
	response, err := client.GetProvider(ctx, &gatewayv1.GetProviderRequest{WorkspaceScope: workspaceSelector(c.workspace()), Name: name})
	if err != nil {
		return ProviderRecord{}, fmt.Errorf("get provider %q: %w", name, err)
	}
	return providerRecordFromProto(response.GetProvider()), nil
}

// PutProvider creates or updates a provider through the pinned OpenShell RPC.
func (c *Client) PutProvider(ctx context.Context, record ProviderRecord) error {
	if record.Name == "" || record.Type == "" {
		return fmt.Errorf("provider name and type are required")
	}
	// OpenShell Provider.Update replaces the credential map. Use the narrow
	// quarshell RPC for a partial patch so old secret values never need to be
	// read or sent back by the client.
	if !sameCredentialKeys(record.EnvVars, record.Credentials) {
		return c.UpdateProviderCredentials(ctx, record.Workspace, record.Name, record.Credentials)
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	workspace := record.Workspace
	if workspace == "" {
		workspace = c.workspace()
	}
	provider := &datamodelv1.Provider{
		Metadata: &datamodelv1.ObjectMeta{Name: record.Name, Workspace: workspace, Annotations: map[string]string{
			runtimeCredentialsAnnotation: strconv.FormatBool(record.RuntimeCredentials),
		}},
		Type: record.Type, Credentials: map[string]string(record.Credentials), Config: map[string]string(record.Config),
		CredentialExpirationTimes: make(map[string]*timestamppb.Timestamp, len(record.CredentialExpiresAtMS)),
	}
	for key, expiresAtMS := range record.CredentialExpiresAtMS {
		provider.CredentialExpirationTimes[key] = timestamppb.New(time.UnixMilli(expiresAtMS))
	}
	current, getErr := client.GetProvider(ctx, &gatewayv1.GetProviderRequest{WorkspaceScope: workspaceSelector(workspace), Name: record.Name})
	if getErr == nil {
		provider.Metadata.Id = current.GetProvider().GetMetadata().GetId()
		provider.Metadata.ResourceVersion = current.GetProvider().GetMetadata().GetResourceVersion()
		_, err = client.UpdateProvider(ctx, &gatewayv1.UpdateProviderRequest{WorkspaceScope: workspaceSelector(workspace), Provider: provider})
	} else if status.Code(getErr) == codes.NotFound {
		_, err = client.CreateProvider(ctx, &gatewayv1.CreateProviderRequest{WorkspaceScope: workspaceSelector(workspace), Provider: provider})
	} else {
		return fmt.Errorf("inspect provider %q before update: %w", record.Name, getErr)
	}
	if err != nil {
		return fmt.Errorf("write provider %q: %w", record.Name, err)
	}
	for key, refresh := range record.Refresh {
		strategy, ok := compatRefreshStrategy(refresh.Strategy)
		if !ok {
			return fmt.Errorf("provider %q credential %q: unsupported refresh strategy %q", record.Name, key, refresh.Strategy)
		}
		config := &gatewayv1.ConfigureProviderRefreshRequest{
			WorkspaceScope: workspaceSelector(workspace), Provider: record.Name, CredentialKey: key, Strategy: strategy,
			Material: refresh.Material,
		}
		if refresh.ExpiresAtMS > 0 {
			config.ExpirationTime = timestamppb.New(time.UnixMilli(refresh.ExpiresAtMS))
		}
		if _, err := client.ConfigureProviderRefresh(ctx, config); err != nil {
			return fmt.Errorf("configure refresh for provider %q credential %q: %w", record.Name, key, err)
		}
	}
	return nil
}

func sameCredentialKeys(keys []string, credentials map[string]string) bool {
	if len(keys) != len(credentials) {
		return false
	}
	for _, key := range keys {
		if _, ok := credentials[key]; !ok {
			return false
		}
	}
	return true
}

// DeleteProvider deletes a provider through the pinned OpenShell contract.
func (c *Client) DeleteProvider(ctx context.Context, name string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.DeleteProvider(ctx, &gatewayv1.DeleteProviderRequest{WorkspaceScope: workspaceSelector(c.workspace()), Name: name}); err != nil {
		return fmt.Errorf("delete provider %q: %w", name, err)
	}
	return nil
}

// AttachProvider attaches a provider with an optimistic sandbox version check.
func (c *Client) AttachProvider(ctx context.Context, sandbox, provider string) error {
	_, err := c.AttachProviderWithReceipt(ctx, sandbox, provider)
	return err
}

// AttachProviderWithReceipt attaches a provider and returns the immutable
// change receipt ID returned by the Gateway.
func (c *Client) AttachProviderWithReceipt(ctx context.Context, sandbox, provider string) (string, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return "", err
	}
	current, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{Name: sandbox, WorkspaceScope: workspaceSelector(c.workspace())})
	if err != nil {
		return "", fmt.Errorf("get sandbox %q before provider attach: %w", sandbox, err)
	}
	response, err := client.AttachSandboxProvider(ctx, &gatewayv1.AttachSandboxProviderRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Provider: provider,
		ExpectedResourceVersion: current.GetSandbox().GetMetadata().GetResourceVersion(),
	})
	if err != nil {
		return "", fmt.Errorf("attach provider %q to sandbox %q: %w", provider, sandbox, err)
	}
	return response.GetReceipt().GetReceiptId(), nil
}

// DetachProvider detaches a provider with an optimistic sandbox version check.
func (c *Client) DetachProvider(ctx context.Context, sandbox, provider string) error {
	_, err := c.DetachProviderWithReceipt(ctx, sandbox, provider)
	return err
}

// DetachProviderWithReceipt detaches a provider and returns the immutable
// change receipt ID returned by the Gateway.
func (c *Client) DetachProviderWithReceipt(ctx context.Context, sandbox, provider string) (string, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return "", err
	}
	current, err := client.GetSandbox(ctx, &gatewayv1.GetSandboxRequest{Name: sandbox, WorkspaceScope: workspaceSelector(c.workspace())})
	if err != nil {
		return "", fmt.Errorf("get sandbox %q before provider detach: %w", sandbox, err)
	}
	response, err := client.DetachSandboxProvider(ctx, &gatewayv1.DetachSandboxProviderRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, Provider: provider,
		ExpectedResourceVersion: current.GetSandbox().GetMetadata().GetResourceVersion(),
	})
	if err != nil {
		return "", fmt.Errorf("detach provider %q from sandbox %q: %w", provider, sandbox, err)
	}
	return response.GetReceipt().GetReceiptId(), nil
}

// ListSandboxProviders lists attached provider metadata without credential values.
func (c *Client) ListSandboxProviders(ctx context.Context, sandbox string) ([]gc.SandboxProviderAttachment, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	result := make([]gc.SandboxProviderAttachment, 0)
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenProviders := map[string]struct{}{}
	for {
		page, err := client.ListSandboxProviders(ctx, &gatewayv1.ListSandboxProvidersRequest{
			WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox, PageSize: 1000, PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list providers attached to sandbox %q: %w", sandbox, err)
		}
		pageItems := make([]gc.SandboxProviderAttachment, 0, len(page.GetProviders()))
		for _, provider := range page.GetProviders() {
			record := providerRecordFromProto(provider)
			pageItems = append(pageItems, gc.SandboxProviderAttachment{Name: record.Name, Type: record.Type, EnvVars: record.EnvVars})
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenProviders, pageItems, func(item gc.SandboxProviderAttachment) string {
			return item.Name
		}, "sandbox providers")
		if appendErr != nil {
			return nil, fmt.Errorf("list providers attached to sandbox %q: %w", sandbox, appendErr)
		}
		if page.GetNextPageToken() == "" {
			break
		}
		pageToken = page.GetNextPageToken()
		if _, seen := seenTokens[pageToken]; seen {
			return nil, fmt.Errorf("list providers attached to sandbox %q: gateway repeated a pagination token", sandbox)
		}
		seenTokens[pageToken] = struct{}{}
	}
	return result, nil
}

// ListSandboxProvidersPage returns one page of redacted provider attachment
// metadata for a sandbox.
func (c *Client) ListSandboxProvidersPage(ctx context.Context, sandbox string, pageSize int32, pageToken string) ([]gc.SandboxProviderAttachment, string, error) {
	if pageSize < 0 {
		return nil, "", fmt.Errorf("sandbox provider page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return nil, "", err
	}
	page, err := client.ListSandboxProviders(ctx, &gatewayv1.ListSandboxProvidersRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox,
		PageSize: pageSize, PageToken: pageToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list providers attached to sandbox %q: %w", sandbox, err)
	}
	providers := make([]gc.SandboxProviderAttachment, 0, len(page.GetProviders()))
	for _, item := range page.GetProviders() {
		record := providerRecordFromProto(item)
		providers = append(providers, gc.SandboxProviderAttachment{Name: record.Name, Type: record.Type, EnvVars: record.EnvVars})
	}
	return providers, page.GetNextPageToken(), nil
}

func providerRecordFromProto(provider *datamodelv1.Provider) gc.ProviderRecord {
	if provider == nil {
		return gc.ProviderRecord{}
	}
	metadata := provider.GetMetadata()
	record := gc.ProviderRecord{
		Name: metadata.GetName(), Type: provider.GetType(), Workspace: metadata.GetWorkspace(),
		Config: gc.ProviderConfig(provider.GetConfig()), Credentials: gc.Credentials{},
		CredentialExpiresAtMS: gc.CredentialExpiry{},
	}
	record.RuntimeCredentials, _ = strconv.ParseBool(metadata.GetAnnotations()[runtimeCredentialsAnnotation])
	for key, expires := range provider.GetCredentialExpirationTimes() {
		if expires != nil {
			record.CredentialExpiresAtMS[key] = expires.AsTime().UnixMilli()
		}
	}
	for key := range provider.GetCredentials() {
		record.EnvVars = append(record.EnvVars, key)
	}
	sort.Strings(record.EnvVars)
	return record
}

// CreateSSHSession issues a short-lived session over the OpenShell RPC.
func (c *Client) CreateSSHSession(ctx context.Context, sandbox string) (SSHSession, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return SSHSession{}, err
	}
	session, err := client.CreateSshSession(ctx, &gatewayv1.CreateSshSessionRequest{
		WorkspaceScope: workspaceSelector(c.workspace()), Sandbox: sandbox,
	})
	if err != nil {
		code := status.Code(err)
		message := status.Convert(err).Message()
		if (code == codes.FailedPrecondition || code == codes.Aborted || code == codes.AlreadyExists) && strings.Contains(strings.ToLower(message), "not ready") {
			return SSHSession{}, fmt.Errorf("%w: %s", ErrSandboxNotReady, message)
		}
		return SSHSession{}, fmt.Errorf("create SSH session for sandbox %q: %w", sandbox, err)
	}
	expiresAtMS := int64(0)
	if session.GetExpirationTime() != nil {
		expiresAtMS = session.GetExpirationTime().AsTime().UnixMilli()
	}
	return SSHSession{
		SandboxID: session.GetSandboxId(), Token: session.GetToken(),
		GatewayHost: session.GetGatewayHost(), GatewayPort: int(session.GetGatewayPort()),
		GatewayScheme: session.GetGatewayScheme(), ExpiresAtMS: expiresAtMS,
	}, nil
}

// RevokeSSHSession revokes a session by its secret token using OpenShell RPC.
func (c *Client) RevokeSSHSession(ctx context.Context, token string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.RevokeSshSession(ctx, &gatewayv1.RevokeSshSessionRequest{Token: token}); err != nil {
		return fmt.Errorf("revoke SSH session: %w", err)
	}
	return nil
}

// ConfigureProviderRefresh configures gateway-managed credential rotation via OpenShell RPC.
func (c *Client) ConfigureProviderRefresh(ctx context.Context, name, key, strategy string, material map[string]string, expiresAtMS int64, secretMaterialKeys ...string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	openStrategy, ok := compatRefreshStrategy(strategy)
	if !ok {
		return fmt.Errorf("unsupported refresh strategy %q", strategy)
	}
	config := &gatewayv1.ConfigureProviderRefreshRequest{WorkspaceScope: workspaceSelector(c.workspace()), Provider: name, CredentialKey: key, Strategy: openStrategy, Material: material, SecretMaterialKeys: append([]string(nil), secretMaterialKeys...)}
	if expiresAtMS > 0 {
		config.ExpirationTime = timestamppb.New(time.UnixMilli(expiresAtMS))
	}
	if _, err := client.ConfigureProviderRefresh(ctx, config); err != nil {
		return fmt.Errorf("configure provider refresh: %w", err)
	}
	return nil
}

// RotateProviderRefresh rotates one provider credential through OpenShell RPC.
func (c *Client) RotateProviderRefresh(ctx context.Context, name, key string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.RotateProviderCredential(ctx, &gatewayv1.RotateProviderCredentialRequest{WorkspaceScope: workspaceSelector(c.workspace()), Provider: name, CredentialKey: key}); err != nil {
		return fmt.Errorf("rotate provider credential: %w", err)
	}
	return nil
}

// DeleteProviderRefresh removes refresh configuration through OpenShell RPC.
func (c *Client) DeleteProviderRefresh(ctx context.Context, name, key string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.DeleteProviderRefresh(ctx, &gatewayv1.DeleteProviderRefreshRequest{WorkspaceScope: workspaceSelector(c.workspace()), Provider: name, CredentialKey: key}); err != nil {
		return fmt.Errorf("delete provider refresh: %w", err)
	}
	return nil
}

// ListWorkspaces lists workspaces through the pinned OpenShell contract.
func (c *Client) ListWorkspaces(ctx context.Context) ([]gc.WorkspaceRecord, error) {
	result := make([]gc.WorkspaceRecord, 0)
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenWorkspaces := map[string]struct{}{}
	for {
		page, err := c.ListWorkspacesPage(ctx, WorkspaceListOptions{PageSize: 1000, PageToken: pageToken})
		if err != nil {
			return nil, fmt.Errorf("list workspaces: %w", err)
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenWorkspaces, page.Workspaces, func(item gc.WorkspaceRecord) string {
			if item.ID != "" {
				return item.ID
			}
			return item.Name
		}, "workspaces")
		if appendErr != nil {
			return nil, appendErr
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
		if _, seen := seenTokens[pageToken]; seen {
			return nil, fmt.Errorf("list workspaces: gateway repeated a pagination token")
		}
		seenTokens[pageToken] = struct{}{}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// GetWorkspace reads one workspace and its members through OpenShell RPC.
func (c *Client) GetWorkspace(ctx context.Context, name string) (gc.WorkspaceRecord, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return gc.WorkspaceRecord{}, err
	}
	workspace, err := client.GetWorkspace(ctx, &gatewayv1.GetWorkspaceRequest{Name: name})
	if err != nil {
		return gc.WorkspaceRecord{}, fmt.Errorf("get workspace %q: %w", name, err)
	}
	metadata := workspace.GetWorkspace().GetMetadata()
	result := gc.WorkspaceRecord{Name: name, Members: make([]gc.WorkspaceMember, 0)}
	if metadata != nil {
		result.ID, result.ResourceVersion = metadata.GetId(), metadata.GetResourceVersion()
		result.Labels = cloneStringMap(metadata.GetLabels())
		if created := metadata.GetCreatedTime(); created != nil {
			result.CreatedAt = created.AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
		}
	}
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenMembers := map[string]struct{}{}
	for {
		members, listErr := client.ListWorkspaceMembers(ctx, &gatewayv1.ListWorkspaceMembersRequest{
			WorkspaceScope: workspaceSelector(name), PageSize: 1000, PageToken: pageToken,
		})
		if listErr != nil {
			return gc.WorkspaceRecord{}, fmt.Errorf("list workspace members %q: %w", name, listErr)
		}
		pageItems := make([]gc.WorkspaceMember, 0, len(members.GetMembers()))
		for _, member := range members.GetMembers() {
			role := "user"
			if member.GetRole() == gatewayv1.WorkspaceRole_WORKSPACE_ROLE_ADMIN {
				role = "admin"
			}
			pageItems = append(pageItems, gc.WorkspaceMember{Subject: member.GetPrincipalSubject(), Role: role})
		}
		var appendErr error
		result.Members, appendErr = appendUniquePageItems(result.Members, seenMembers, pageItems, func(item gc.WorkspaceMember) string {
			return item.Subject
		}, "workspace members")
		if appendErr != nil {
			return gc.WorkspaceRecord{}, fmt.Errorf("list workspace members %q: %w", name, appendErr)
		}
		if members.GetNextPageToken() == "" {
			break
		}
		pageToken = members.GetNextPageToken()
		if _, seen := seenTokens[pageToken]; seen {
			return gc.WorkspaceRecord{}, fmt.Errorf("list workspace members %q: gateway repeated a pagination token", name)
		}
		seenTokens[pageToken] = struct{}{}
	}
	return result, nil
}

// WorkspaceMembersPage contains a single paginated workspace-member listing.
type WorkspaceMembersPage struct {
	Members       []gc.WorkspaceMember `json:"members"`
	NextPageToken string               `json:"next_page_token"`
}

// ListWorkspaceMembersPage returns one Gateway page of workspace members.
func (c *Client) ListWorkspaceMembersPage(ctx context.Context, workspace string, pageSize int32, pageToken string) (WorkspaceMembersPage, error) {
	if strings.TrimSpace(workspace) == "" {
		return WorkspaceMembersPage{}, fmt.Errorf("workspace is required")
	}
	if pageSize < 0 {
		return WorkspaceMembersPage{}, fmt.Errorf("workspace member page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return WorkspaceMembersPage{}, err
	}
	response, err := client.ListWorkspaceMembers(ctx, &gatewayv1.ListWorkspaceMembersRequest{
		WorkspaceScope: workspaceSelector(strings.TrimSpace(workspace)), PageSize: pageSize, PageToken: pageToken,
	})
	if err != nil {
		return WorkspaceMembersPage{}, fmt.Errorf("list members for workspace %q: %w", workspace, err)
	}
	page := WorkspaceMembersPage{Members: make([]gc.WorkspaceMember, 0, len(response.GetMembers())), NextPageToken: response.GetNextPageToken()}
	for _, member := range response.GetMembers() {
		role := "user"
		if member.GetRole() == gatewayv1.WorkspaceRole_WORKSPACE_ROLE_ADMIN {
			role = "admin"
		}
		page.Members = append(page.Members, gc.WorkspaceMember{Subject: member.GetPrincipalSubject(), Role: role})
	}
	return page, nil
}

// CreateWorkspace creates a workspace through the pinned OpenShell contract.
func (c *Client) CreateWorkspace(ctx context.Context, name string) (gc.WorkspaceRecord, error) {
	return c.CreateWorkspaceWithLabels(ctx, name, nil)
}

// CreateWorkspaceWithLabels creates a workspace with metadata labels.
func (c *Client) CreateWorkspaceWithLabels(ctx context.Context, name string, labels map[string]string) (gc.WorkspaceRecord, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return gc.WorkspaceRecord{}, err
	}
	response, err := client.CreateWorkspace(ctx, &gatewayv1.CreateWorkspaceRequest{Name: name, Labels: cloneStringMap(labels)})
	if err != nil {
		return gc.WorkspaceRecord{}, fmt.Errorf("create workspace %q: %w", name, err)
	}
	metadata := response.GetWorkspace().GetMetadata()
	result := gc.WorkspaceRecord{Name: name, Labels: cloneStringMap(labels)}
	if metadata != nil {
		result.ID, result.ResourceVersion = metadata.GetId(), metadata.GetResourceVersion()
	}
	return result, nil
}

// DeleteWorkspace deletes a workspace through the pinned OpenShell contract.
func (c *Client) DeleteWorkspace(ctx context.Context, name string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.DeleteWorkspace(ctx, &gatewayv1.DeleteWorkspaceRequest{Name: name}); err != nil {
		return fmt.Errorf("delete workspace %q: %w", name, err)
	}
	return nil
}

// WorkspaceMemberAdd adds or updates one workspace member through OpenShell RPC.
func (c *Client) WorkspaceMemberAdd(ctx context.Context, name, subject, role string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	openRole := gatewayv1.WorkspaceRole_WORKSPACE_ROLE_USER
	if strings.EqualFold(role, "admin") {
		openRole = gatewayv1.WorkspaceRole_WORKSPACE_ROLE_ADMIN
	}
	if _, err := client.AddWorkspaceMember(ctx, &gatewayv1.AddWorkspaceMemberRequest{WorkspaceScope: workspaceSelector(name), PrincipalSubject: subject, Role: openRole}); err != nil {
		return fmt.Errorf("add workspace member %q: %w", subject, err)
	}
	return nil
}

// WorkspaceMemberRemove removes a workspace member through OpenShell RPC.
func (c *Client) WorkspaceMemberRemove(ctx context.Context, name, subject string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	if _, err := client.RemoveWorkspaceMember(ctx, &gatewayv1.RemoveWorkspaceMemberRequest{WorkspaceScope: workspaceSelector(name), PrincipalSubject: subject}); err != nil {
		return fmt.Errorf("remove workspace member %q: %w", subject, err)
	}
	return nil
}

func workspaceSelector(name string) *datamodelv1.WorkspaceSelector {
	return &datamodelv1.WorkspaceSelector{Selection: &datamodelv1.WorkspaceSelector_Workspace{Workspace: name}}
}

func compatRefreshStrategy(strategy string) (gatewayv1.ProviderCredentialRefreshStrategy, bool) {
	switch strings.ToLower(strings.ReplaceAll(strategy, "_", "-")) {
	case "oauth2-refresh-token":
		return gatewayv1.ProviderCredentialRefreshStrategy_PROVIDER_CREDENTIAL_REFRESH_STRATEGY_OAUTH2_REFRESH_TOKEN, true
	case "oauth2-client-credentials":
		return gatewayv1.ProviderCredentialRefreshStrategy_PROVIDER_CREDENTIAL_REFRESH_STRATEGY_OAUTH2_CLIENT_CREDENTIALS, true
	default:
		return gatewayv1.ProviderCredentialRefreshStrategy_PROVIDER_CREDENTIAL_REFRESH_STRATEGY_UNSPECIFIED, false
	}
}
