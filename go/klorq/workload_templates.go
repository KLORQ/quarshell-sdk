package klorq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	datamodelv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/datamodel/v1"
	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// WorkloadTemplate is the portable, workspace-scoped workload shape exposed by
// the OpenShell template API.
type WorkloadTemplate struct {
	Name             string            `json:"name"`
	Workspace        string            `json:"workspace,omitempty"`
	Image            string            `json:"image"`
	Environment      map[string]string `json:"environment,omitempty"`
	CPU              string            `json:"cpu,omitempty"`
	Memory           string            `json:"memory,omitempty"`
	GPU              bool              `json:"gpu,omitempty"`
	GPUCount         uint32            `json:"gpu_count,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
	DriverConfigJSON string            `json:"driver_config_json,omitempty"`
	ReadyWithin      time.Duration     `json:"ready_within,omitempty"`
	MaxBurst         uint32            `json:"max_burst,omitempty"`
}

// CreateSandboxTemplate creates a template in the selected workspace.
func (c *Client) CreateSandboxTemplate(ctx context.Context, template WorkloadTemplate) (WorkloadTemplate, error) {
	if template.Name == "" || template.Image == "" {
		return WorkloadTemplate{}, fmt.Errorf("create sandbox template: name and image are required")
	}
	if template.ReadyWithin < 0 {
		return WorkloadTemplate{}, fmt.Errorf("create sandbox template: ready-within must be positive")
	}
	workspace := template.Workspace
	if workspace == "" {
		workspace = c.workspace()
	}
	resources := &compatv1.SandboxResources{Cpu: template.CPU, Memory: template.Memory}
	if template.GPU {
		gpu := &compatv1.GpuResourceRequirements{}
		if template.GPUCount > 0 {
			gpu.Count = proto.Uint32(template.GPUCount)
		}
		resources.Gpu = gpu
	}
	var driverConfig *structpb.Struct
	if template.DriverConfigJSON != "" {
		var values map[string]any
		if err := json.Unmarshal([]byte(template.DriverConfigJSON), &values); err != nil || len(values) == 0 {
			return WorkloadTemplate{}, fmt.Errorf("create sandbox template: driver config must be a non-empty JSON object")
		}
		var err error
		driverConfig, err = structpb.NewStruct(values)
		if err != nil {
			return WorkloadTemplate{}, fmt.Errorf("create sandbox template: driver config: %w", err)
		}
	}
	client, err := c.gatewayClient()
	if err != nil {
		return WorkloadTemplate{}, err
	}
	var serviceLevel *compatv1.SandboxServiceLevel
	if template.ReadyWithin > 0 || template.MaxBurst > 0 {
		startup := &compatv1.SandboxStartup{MaxBurst: template.MaxBurst}
		if template.ReadyWithin > 0 {
			startup.ReadyWithin = durationpb.New(template.ReadyWithin)
		}
		serviceLevel = &compatv1.SandboxServiceLevel{Startup: startup}
	}
	response, err := client.CreateSandboxTemplate(ctx, &compatv1.CreateSandboxTemplateRequest{
		WorkspaceScope: namedTemplateWorkspace(workspace),
		Template: &compatv1.SandboxWorkloadTemplate{
			Metadata: &datamodelv1.ObjectMeta{Name: template.Name, Workspace: workspace, Labels: cloneStringMap(template.Labels), Annotations: cloneStringMap(template.Annotations)},
			Spec: &compatv1.SandboxWorkloadTemplateSpec{
				Workload:            &compatv1.SandboxWorkloadConfig{Image: template.Image, Environment: cloneStringMap(template.Environment), Resources: resources},
				DriverConfig:        driverConfig,
				DesiredServiceLevel: serviceLevel,
			},
		},
	})
	if err != nil {
		return WorkloadTemplate{}, fmt.Errorf("create sandbox template %q: %w", template.Name, err)
	}
	return workloadTemplateFromProto(response.GetTemplate()), nil
}

// GetSandboxTemplate returns a template visible in the selected workspace.
func (c *Client) GetSandboxTemplate(ctx context.Context, workspace, name string) (WorkloadTemplate, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return WorkloadTemplate{}, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.GetSandboxTemplate(ctx, &compatv1.GetSandboxTemplateRequest{WorkspaceScope: namedTemplateWorkspace(workspace), Name: name})
	if err != nil {
		return WorkloadTemplate{}, fmt.Errorf("get sandbox template %q: %w", name, err)
	}
	return workloadTemplateFromProto(response.GetTemplate()), nil
}

// ListSandboxTemplates lists templates visible in the selected workspace.
func (c *Client) ListSandboxTemplates(ctx context.Context, workspace string) ([]WorkloadTemplate, error) {
	result := make([]WorkloadTemplate, 0)
	pageToken := ""
	seenTokens := map[string]struct{}{}
	seenTemplates := map[string]struct{}{}
	for {
		page, err := c.ListSandboxTemplatesPage(ctx, workspace, 1000, pageToken, "")
		if err != nil {
			return nil, err
		}
		var appendErr error
		result, appendErr = appendUniquePageItems(result, seenTemplates, page.Templates, func(item WorkloadTemplate) string {
			return item.Workspace + "\x00" + item.Name
		}, "sandbox template")
		if appendErr != nil {
			return nil, appendErr
		}
		pageToken = page.NextPageToken
		if pageToken == "" {
			return result, nil
		}
		if _, seen := seenTokens[pageToken]; seen {
			return nil, fmt.Errorf("list sandbox templates: gateway repeated a pagination token")
		}
		seenTokens[pageToken] = struct{}{}
	}
}

// WorkloadTemplatePage is one cursor page of workspace templates.
type WorkloadTemplatePage struct {
	Templates     []WorkloadTemplate `json:"templates"`
	NextPageToken string             `json:"next_page_token"`
}

// ListSandboxTemplatesPage returns one page of templates. A negative page size
// is rejected before the Gateway request.
func (c *Client) ListSandboxTemplatesPage(ctx context.Context, workspace string, pageSize int32, pageToken, labelSelector string) (WorkloadTemplatePage, error) {
	if pageSize < 0 {
		return WorkloadTemplatePage{}, fmt.Errorf("sandbox template page size must not be negative")
	}
	pageSize = capGatewayPageSize(pageSize)
	client, err := c.gatewayClient()
	if err != nil {
		return WorkloadTemplatePage{}, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.ListSandboxTemplates(ctx, &compatv1.ListSandboxTemplatesRequest{
		WorkspaceScope: namedTemplateWorkspace(workspace), PageSize: pageSize, PageToken: pageToken, LabelSelector: labelSelector,
	})
	if err != nil {
		return WorkloadTemplatePage{}, fmt.Errorf("list sandbox templates: %w", err)
	}
	result := WorkloadTemplatePage{Templates: make([]WorkloadTemplate, 0, len(response.GetTemplates())), NextPageToken: response.GetNextPageToken()}
	for _, template := range response.GetTemplates() {
		result.Templates = append(result.Templates, workloadTemplateFromProto(template))
	}
	return result, nil
}

// DeleteSandboxTemplate deletes a template in the selected workspace.
func (c *Client) DeleteSandboxTemplate(ctx context.Context, workspace, name string) (bool, error) {
	client, err := c.gatewayClient()
	if err != nil {
		return false, err
	}
	if workspace == "" {
		workspace = c.workspace()
	}
	response, err := client.DeleteSandboxTemplate(ctx, &compatv1.DeleteSandboxTemplateRequest{WorkspaceScope: namedTemplateWorkspace(workspace), Name: name, AllowMissing: true})
	if err != nil {
		return false, fmt.Errorf("delete sandbox template %q: %w", name, err)
	}
	return response.GetOutcome() == compatv1.DeletionOutcome_DELETION_OUTCOME_COMPLETED, nil
}

func namedTemplateWorkspace(workspace string) *datamodelv1.WorkspaceSelector {
	return &datamodelv1.WorkspaceSelector{Selection: &datamodelv1.WorkspaceSelector_Workspace{Workspace: workspace}}
}

func workloadTemplateFromProto(template *compatv1.SandboxWorkloadTemplate) WorkloadTemplate {
	if template == nil {
		return WorkloadTemplate{}
	}
	result := WorkloadTemplate{Environment: map[string]string{}, Labels: map[string]string{}, Annotations: map[string]string{}}
	if metadata := template.GetMetadata(); metadata != nil {
		result.Name, result.Workspace, result.Labels, result.Annotations = metadata.GetName(), metadata.GetWorkspace(), cloneStringMap(metadata.GetLabels()), cloneStringMap(metadata.GetAnnotations())
	}
	if spec := template.GetSpec(); spec != nil {
		if config := spec.GetWorkload(); config != nil {
			result.Image, result.Environment = config.GetImage(), cloneStringMap(config.GetEnvironment())
			if resources := config.GetResources(); resources != nil {
				result.CPU, result.Memory = resources.GetCpu(), resources.GetMemory()
				if resources.GetGpu() != nil {
					result.GPU = true
					result.GPUCount = resources.GetGpu().GetCount()
				}
			}
		}
		if config := spec.GetDriverConfig(); config != nil {
			if encoded, err := json.Marshal(config.AsMap()); err == nil {
				result.DriverConfigJSON = string(encoded)
			}
		}
		if startup := spec.GetDesiredServiceLevel().GetStartup(); startup != nil {
			if startup.GetReadyWithin() != nil {
				result.ReadyWithin = startup.GetReadyWithin().AsDuration()
			}
			result.MaxBurst = startup.GetMaxBurst()
		}
	}
	return result
}
