package klorq

import (
	"context"
	"fmt"
	"strings"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// PolicyRevisionRecord is one revision returned by the pinned OpenShell
// Gateway history API. The API exposes policy identity and status, but not the
// source YAML byte count returned by the legacy control API.
type PolicyRevisionRecord struct {
	Revision   uint32            `json:"revision" yaml:"revision"`
	PolicyHash string            `json:"policy_hash" yaml:"policy_hash"`
	Status     string            `json:"status" yaml:"status"`
	LoadError  string            `json:"load_error,omitempty" yaml:"load_error,omitempty"`
	CreatedAt  *time.Time        `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	LoadedAt   *time.Time        `json:"loaded_at,omitempty" yaml:"loaded_at,omitempty"`
	Provenance map[string]string `json:"provenance,omitempty" yaml:"provenance,omitempty"`
}

// PolicyRevisionPage contains one Gateway page of sandbox or global history.
type PolicyRevisionPage struct {
	Revisions     []PolicyRevisionRecord `json:"revisions" yaml:"revisions"`
	NextPageToken string                 `json:"next_page_token" yaml:"next_page_token"`
}

// ListPolicyRevisionsPage reads one cursor page through the pinned Gateway
// contract. Zero pageSize uses the server default; values above 1000 are
// clamped by the server.
func (c *Client) ListPolicyRevisionsPage(ctx context.Context, sandbox string, pageSize int32, pageToken string) (PolicyRevisionPage, error) {
	if strings.TrimSpace(sandbox) == "" {
		return PolicyRevisionPage{}, fmt.Errorf("sandbox name must not be empty")
	}
	if pageSize < 0 {
		return PolicyRevisionPage{}, fmt.Errorf("policy revision page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return PolicyRevisionPage{}, err
	}
	response, err := client.ListSandboxPolicies(ctx, &gatewayv1.ListSandboxPoliciesRequest{
		WorkspaceScope: workspaceSelector(c.workspace()),
		Sandbox:        strings.TrimSpace(sandbox),
		PageSize:       pageSize,
		PageToken:      pageToken,
	})
	if err != nil {
		return PolicyRevisionPage{}, fmt.Errorf("list sandbox policy revisions %q: %w", sandbox, err)
	}
	return policyRevisionPageFromGateway(response), nil
}

// ListGlobalPolicyRevisionsPage reads one platform-wide history page. The
// Gateway requires platform-administrator authorization for this request.
func (c *Client) ListGlobalPolicyRevisionsPage(ctx context.Context, pageSize int32, pageToken string) (PolicyRevisionPage, error) {
	if pageSize < 0 {
		return PolicyRevisionPage{}, fmt.Errorf("policy revision page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return PolicyRevisionPage{}, err
	}
	response, err := client.ListSandboxPolicies(ctx, &gatewayv1.ListSandboxPoliciesRequest{
		Global: true, PageSize: pageSize, PageToken: pageToken,
	})
	if err != nil {
		return PolicyRevisionPage{}, fmt.Errorf("list global policy revisions: %w", err)
	}
	return policyRevisionPageFromGateway(response), nil
}

// ListPolicyRevisionsGateway collects Gateway history pages and protects
// callers from a server that repeats a non-empty cursor.
func (c *Client) ListPolicyRevisionsGateway(ctx context.Context, sandbox string) ([]PolicyRevisionRecord, error) {
	return c.collectPolicyRevisionPages(ctx, sandbox, false)
}

// ListGlobalPolicyRevisionsGateway collects platform-wide history pages. The
// Gateway requires platform-administrator authorization.
func (c *Client) ListGlobalPolicyRevisionsGateway(ctx context.Context) ([]PolicyRevisionRecord, error) {
	return c.collectPolicyRevisionPages(ctx, "", true)
}

func (c *Client) collectPolicyRevisionPages(ctx context.Context, sandbox string, global bool) ([]PolicyRevisionRecord, error) {
	resource := "sandbox"
	if global {
		resource = "global"
	}
	result := make([]PolicyRevisionRecord, 0)
	seen := make(map[string]struct{})
	seenRevisions := make(map[uint32]struct{})
	token := ""
	for {
		var page PolicyRevisionPage
		var err error
		if global {
			page, err = c.ListGlobalPolicyRevisionsPage(ctx, 1000, token)
		} else {
			page, err = c.ListPolicyRevisionsPage(ctx, sandbox, 1000, token)
		}
		if err != nil {
			return nil, err
		}
		for _, revision := range page.Revisions {
			if _, ok := seenRevisions[revision.Revision]; ok {
				return nil, fmt.Errorf("list %s policy revisions: gateway repeated revision %d", resource, revision.Revision)
			}
			seenRevisions[revision.Revision] = struct{}{}
			result = append(result, revision)
		}
		token = page.NextPageToken
		if token == "" {
			return result, nil
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("list %s policy revisions: gateway repeated a page cursor", resource)
		}
		seen[token] = struct{}{}
	}
}

func policyRevisionPageFromGateway(response *gatewayv1.ListSandboxPoliciesResponse) PolicyRevisionPage {
	page := PolicyRevisionPage{
		Revisions:     make([]PolicyRevisionRecord, 0, len(response.GetRevisions())),
		NextPageToken: response.GetNextPageToken(),
	}
	for _, revision := range response.GetRevisions() {
		if revision == nil {
			continue
		}
		page.Revisions = append(page.Revisions, policyRevisionRecordFromGateway(revision))
	}
	return page
}

func policyRevisionRecordFromGateway(revision *gatewayv1.SandboxPolicyRevision) PolicyRevisionRecord {
	record := PolicyRevisionRecord{
		Revision: revision.GetVersion(), PolicyHash: revision.GetPolicyHash(),
		Status: revision.GetStatus().String(), LoadError: revision.GetLoadError(),
		Provenance: revision.GetProvenance(),
	}
	if created := revision.GetCreatedTime(); created != nil {
		value := created.AsTime()
		record.CreatedAt = &value
	}
	if loaded := revision.GetLoadedTime(); loaded != nil {
		value := loaded.AsTime()
		record.LoadedAt = &value
	}
	return record
}
