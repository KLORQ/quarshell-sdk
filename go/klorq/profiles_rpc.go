package klorq

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	gc "github.com/KLORQ/quarshell-sdk/internal/gatewayclient"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// ListProfiles and its scoped form use the pinned OpenShell Gateway API.
func (c *Client) ListProfiles(ctx context.Context) ([]gc.ProfileInfo, error) {
	return c.ListProfilesScoped(ctx, "global", "")
}

// ProfileInfoPage contains one paginated provider-profile catalog response.
type ProfileInfoPage struct {
	Profiles      []gc.ProfileInfo `json:"profiles"`
	NextPageToken string           `json:"next_page_token"`
}

// ListProviderProfilesPage uses the pinned OpenShell Gateway RPC. A workspace
// scope lists profiles visible in that workspace; global lists platform ones.
func (c *Client) ListProviderProfilesPage(ctx context.Context, scope, workspace string, pageSize int32, pageToken string) (ProfileInfoPage, error) {
	if pageSize < 0 {
		return ProfileInfoPage{}, fmt.Errorf("provider profile page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "global" && scope != "workspace" {
		return ProfileInfoPage{}, fmt.Errorf("provider profile scope must be global or workspace")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return ProfileInfoPage{}, err
	}
	var workspaceScope *datamodelv1.WorkspaceSelector
	if scope == "workspace" {
		if strings.TrimSpace(workspace) == "" {
			return ProfileInfoPage{}, fmt.Errorf("workspace profile scope requires a workspace name")
		}
		workspaceScope = workspaceSelector(strings.TrimSpace(workspace))
	}
	response, err := client.ListProviderProfiles(ctx, &gatewayv1.ListProviderProfilesRequest{WorkspaceScope: workspaceScope, PageSize: pageSize, PageToken: pageToken})
	if err != nil {
		return ProfileInfoPage{}, fmt.Errorf("list provider profiles: %w", err)
	}
	page := ProfileInfoPage{Profiles: make([]gc.ProfileInfo, 0, len(response.GetProfiles())), NextPageToken: response.GetNextPageToken()}
	for _, profile := range response.GetProfiles() {
		if profile == nil {
			continue
		}
		category := strings.ToLower(strings.TrimPrefix(profile.GetCategory().String(), "PROVIDER_PROFILE_CATEGORY_"))
		page.Profiles = append(page.Profiles, gc.ProfileInfo{ID: profile.GetId(), Category: category, Source: profile.GetSource(), Scope: profile.GetScope(), ResourceVersion: profile.GetResourceVersion()})
	}
	return page, nil
}

func (c *Client) ListProfilesScoped(ctx context.Context, scope, workspace string) ([]gc.ProfileInfo, error) {
	result := make([]gc.ProfileInfo, 0)
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenProfiles := map[string]struct{}{}
	for {
		page, err := c.ListProviderProfilesPage(ctx, scope, workspace, 1000, pageToken)
		if err != nil {
			return nil, err
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenProfiles, page.Profiles, func(item gc.ProfileInfo) string {
			return item.ID
		}, "provider profiles")
		if appendErr != nil {
			return nil, appendErr
		}
		pageToken = page.NextPageToken
		if pageToken == "" {
			break
		}
		if _, seen := seenTokens[pageToken]; seen {
			return nil, fmt.Errorf("list provider profiles: gateway repeated a pagination token")
		}
		seenTokens[pageToken] = struct{}{}
	}
	return result, nil
}

func (c *Client) GetProfile(ctx context.Context, id string) ([]byte, string, string, error) {
	return c.GetProfileScoped(ctx, id, "global", "")
}

func (c *Client) GetProfileScoped(ctx context.Context, id, scope, workspace string) ([]byte, string, string, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return nil, "", "", err
	}
	response, err := client.GetProviderProfile(ctx, &gatewayv1.GetProviderProfileRequest{
		WorkspaceScope: profileWorkspaceSelector(scope, workspace), Id: id,
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("get provider profile %q: %w", id, err)
	}
	profile := response.GetProfile()
	if profile == nil {
		return nil, "", "", fmt.Errorf("get provider profile %q: gateway returned no profile", id)
	}
	data, err := providerProfileYAML(profile)
	if err != nil {
		return nil, "", "", fmt.Errorf("encode provider profile %q: %w", id, err)
	}
	return data, profile.GetSource(), strconv.FormatUint(profile.GetResourceVersion(), 10), nil
}

func (c *Client) CreateProfile(ctx context.Context, id string, profile []byte) error {
	return c.CreateProfileScoped(ctx, id, profile, "global", "")
}

func (c *Client) CreateProfileScoped(ctx context.Context, id string, profile []byte, scope, workspace string) error {
	parsed, err := parseProviderProfileYAML(profile)
	if err != nil {
		return err
	}
	if len(profile) == 0 || len(profile) > 1<<20 {
		return fmt.Errorf("profile document must be between 1 byte and 1 MiB")
	}
	if strings.TrimSpace(parsed.GetId()) != strings.TrimSpace(id) {
		return fmt.Errorf("profile id does not match request")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	_, err = client.ImportProviderProfiles(ctx, &gatewayv1.ImportProviderProfilesRequest{
		WorkspaceScope: profileWorkspaceSelector(scope, workspace),
		Profiles:       []*gatewayv1.ProviderProfileImportItem{{Profile: parsed}},
	})
	if err != nil {
		return fmt.Errorf("import provider profile %q: %w", id, err)
	}
	return nil
}

func (c *Client) PutProfile(ctx context.Context, id string, profile []byte, expectedVersion string) error {
	return c.PutProfileScoped(ctx, id, profile, expectedVersion, "global", "")
}

func (c *Client) PutProfileScoped(ctx context.Context, id string, profile []byte, expectedVersion, scope, workspace string) error {
	version, err := strconv.ParseUint(strings.Trim(expectedVersion, `"`), 10, 64)
	if err != nil || version == 0 {
		return fmt.Errorf("profile update requires a positive resource version")
	}
	if len(profile) == 0 || len(profile) > 1<<20 {
		return fmt.Errorf("profile document must be between 1 byte and 1 MiB")
	}
	parsed, err := parseProviderProfileYAML(profile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.GetId()) != strings.TrimSpace(id) {
		return fmt.Errorf("profile id does not match request")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	_, err = client.UpdateProviderProfiles(ctx, &gatewayv1.UpdateProviderProfilesRequest{
		WorkspaceScope: profileWorkspaceSelector(scope, workspace), Id: id,
		ExpectedResourceVersion: version, Profile: &gatewayv1.ProviderProfileImportItem{Profile: parsed},
	})
	if err != nil {
		return fmt.Errorf("update provider profile %q: %w", id, err)
	}
	return nil
}

func (c *Client) DeleteProfile(ctx context.Context, id string) error {
	return c.DeleteProfileScoped(ctx, id, "global", "")
}

func (c *Client) DeleteProfileScoped(ctx context.Context, id, scope, workspace string) error {
	client, err := c.gatewayClient()
	if err != nil {
		return err
	}
	_, err = client.DeleteProviderProfile(ctx, &gatewayv1.DeleteProviderProfileRequest{
		WorkspaceScope: profileWorkspaceSelector(scope, workspace), Id: id,
	})
	if err != nil {
		return fmt.Errorf("delete provider profile %q: %w", id, err)
	}
	return nil
}

func profileWorkspaceSelector(scope, workspace string) *datamodelv1.WorkspaceSelector {
	if strings.EqualFold(strings.TrimSpace(scope), "global") {
		return nil
	}
	return workspaceSelector(strings.TrimSpace(workspace))
}

func parseProviderProfileYAML(data []byte) (*gatewayv1.ProviderProfile, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, fmt.Errorf("profile document must be between 1 byte and 1 MiB")
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse provider profile YAML: %w", err)
	}
	if document == nil {
		return nil, fmt.Errorf("profile document must be a YAML mapping")
	}
	if category, ok := document["category"].(string); ok {
		document["category"] = "PROVIDER_PROFILE_CATEGORY_" + strings.ToUpper(category)
	}
	delete(document, "source")
	delete(document, "scope")
	delete(document, "resource_version")
	jsonData, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode provider profile YAML: %w", err)
	}
	profile := new(gatewayv1.ProviderProfile)
	if err := protojson.Unmarshal(jsonData, profile); err != nil {
		return nil, fmt.Errorf("decode OpenShell provider profile: %w", err)
	}
	return profile, nil
}

func providerProfileYAML(profile *gatewayv1.ProviderProfile) ([]byte, error) {
	jsonData, err := protojson.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(jsonData, &document); err != nil {
		return nil, err
	}
	if category, ok := document["category"].(string); ok {
		document["category"] = strings.ToLower(strings.TrimPrefix(category, "PROVIDER_PROFILE_CATEGORY_"))
	}
	return yaml.Marshal(document)
}
