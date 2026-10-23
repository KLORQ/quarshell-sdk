package gatewayclient

import (
	"errors"
	"time"
)

type Labels = map[string]string
type Credentials = map[string]string
type ProviderConfig = map[string]string
type RefreshMaterial = map[string]string
type CredentialBindings = map[string]string
type CredentialExpiry = map[string]int64

var ErrSandboxNotReady = errors.New("sandbox is not ready (supervisor relay not connected)")

type ProfileInfo struct {
	ID              string `json:"id"`
	Category        string `json:"category,omitempty"`
	Source          string `json:"source"`
	Scope           string `json:"scope,omitempty"`
	ResourceVersion uint64 `json:"resource_version,omitempty"`
}

type ProviderRecord struct {
	Name                  string                           `json:"name"`
	Type                  string                           `json:"type"`
	Workspace             string                           `json:"workspace,omitempty"`
	EnvVars               []string                         `json:"env_vars,omitempty"`
	Credentials           Credentials                      `json:"credentials,omitempty"`
	CredentialExpiresAtMS CredentialExpiry                 `json:"credential_expires_at_ms,omitempty"`
	RuntimeCredentials    bool                             `json:"runtime_credentials,omitempty"`
	Config                ProviderConfig                   `json:"config,omitempty"`
	Refresh               map[string]ProviderRefreshConfig `json:"refresh,omitempty"`
}

type ProviderRefreshConfig struct {
	CredentialKey          string             `json:"credential_key"`
	Strategy               string             `json:"strategy"`
	Material               RefreshMaterial    `json:"material,omitempty"`
	MaterialSecretKeys     []string           `json:"material_secret_keys,omitempty"`
	MaterialCredentialKeys CredentialBindings `json:"material_credential_keys,omitempty"`
	Outputs                CredentialBindings `json:"outputs,omitempty"`
	RefreshBeforeSeconds   int64              `json:"refresh_before_seconds,omitempty"`
	MaxLifetimeSeconds     int64              `json:"max_lifetime_seconds,omitempty"`
	ExpiresAtMS            int64              `json:"expires_at_ms,omitempty"`
}

type SandboxProviderAttachment struct {
	Name    string   `json:"name"`
	Type    string   `json:"type,omitempty"`
	EnvVars []string `json:"env_vars,omitempty"`
}

// ProviderReadinessStatus is the redacted operator view of one provider
// mutation and the current supervisor evidence for it.
type ProviderReadinessStatus struct {
	ReceiptID         string                        `json:"receipt_id,omitempty"`
	MutationID        string                        `json:"mutation_id,omitempty"`
	Provider          string                        `json:"provider,omitempty"`
	Workspace         string                        `json:"workspace,omitempty"`
	Kind              string                        `json:"kind,omitempty"`
	State             string                        `json:"state,omitempty"`
	Reason            string                        `json:"reason,omitempty"`
	Observed          *ProviderReadinessObservation `json:"observed,omitempty"`
	NetworkInstanceID string                        `json:"network_instance_id,omitempty"`
	ObservedTime      time.Time                     `json:"observed_time,omitempty"`
	EvaluatedTime     time.Time                     `json:"evaluated_time,omitempty"`
}

// ProviderReadinessObservation is installation evidence reported by the
// authenticated supervisor. It never contains credential material.
type ProviderReadinessObservation struct {
	SessionID                  string `json:"session_id,omitempty"`
	Sequence                   uint64 `json:"sequence,omitempty"`
	AttachmentEpoch            string `json:"attachment_epoch,omitempty"`
	ProviderEnvRevision        uint64 `json:"provider_env_revision,omitempty"`
	ConfigRevision             uint64 `json:"config_revision,omitempty"`
	PolicyHash                 string `json:"policy_hash,omitempty"`
	CredentialsInstalled       bool   `json:"credentials_installed,omitempty"`
	PolicyActive               bool   `json:"policy_active,omitempty"`
	LaunchEnvironmentInstalled bool   `json:"launch_environment_installed,omitempty"`
	ProcessInstanceID          string `json:"process_instance_id,omitempty"`
	Reason                     string `json:"reason,omitempty"`
}

type Sandbox struct {
	Name                       string            `json:"name"`
	ID                         string            `json:"id,omitempty"`
	Image                      string            `json:"image,omitempty"`
	Workspace                  string            `json:"workspace,omitempty"`
	WorkloadTemplateName       string            `json:"workload_template_name,omitempty"`
	CPUMillis                  uint32            `json:"cpu_millis,omitempty"`
	MemoryBytes                uint64            `json:"memory_bytes,omitempty"`
	GPUCount                   uint32            `json:"gpu_count,omitempty"`
	GPURequested               bool              `json:"gpu_requested,omitempty"`
	AwaitMainProcessAttachment bool              `json:"await_main_process_attachment,omitempty"`
	PidsLimit                  int64             `json:"pids_limit,omitempty"`
	CDIDevices                 []string          `json:"cdi_devices,omitempty"`
	TTY                        bool              `json:"tty,omitempty"`
	Environment                map[string]string `json:"environment,omitempty"`
	DriverConfigJSON           string            `json:"driver_config_json,omitempty"`
	ResourceVersion            uint64            `json:"resource_version,omitempty"`
	ComputeDriver              string            `json:"compute_driver,omitempty"`
	Network                    string            `json:"network,omitempty"`
	Status                     string            `json:"status,omitempty"`
	Labels                     Labels            `json:"labels,omitempty"`
	BasePolicyYAML             string            `json:"base_policy_yaml,omitempty"`
	PolicyYAML                 string            `json:"policy_yaml,omitempty"`
	AttachedProviders          []string          `json:"attached_providers,omitempty"`
	RestartPolicy              string            `json:"restart_policy,omitempty"`
	Ephemeral                  bool              `json:"ephemeral,omitempty"`
	ServiceExposures           []ServiceExposure `json:"service_exposures,omitempty"`
}

type ServiceExposure struct {
	Name              string `json:"name"`
	TargetPort        uint32 `json:"target_port"`
	AuthorizationMode string `json:"authorization_mode,omitempty"`
}

type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

type ServiceRecord struct {
	ID                string `json:"id,omitempty"`
	SandboxID         string `json:"sandbox_id,omitempty"`
	Workspace         string `json:"workspace,omitempty"`
	Name              string `json:"name"`
	Sandbox           string `json:"sandbox"`
	Port              int    `json:"port"`
	URL               string `json:"url,omitempty"`
	Domain            bool   `json:"domain"`
	AuthorizationMode string `json:"authorization_mode,omitempty"`
	BackendHost       string `json:"backend_host"`
	BackendPort       int    `json:"backend_port"`
}

type WorkspaceMember struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
}

type WorkspaceRecord struct {
	ID              string            `json:"id,omitempty"`
	Name            string            `json:"name"`
	ResourceVersion uint64            `json:"resource_version,omitempty"`
	Members         []WorkspaceMember `json:"members,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	CreatedAt       string            `json:"created_at,omitempty"`
	Status          string            `json:"status,omitempty"`
}

type SSHSession struct {
	SessionID     string `json:"session_id"`
	SandboxID     string `json:"sandbox_id"`
	Token         string `json:"token"`
	GatewayScheme string `json:"gateway_scheme"`
	GatewayHost   string `json:"gateway_host"`
	GatewayPort   int    `json:"gateway_port"`
	ExpiresAtMS   int64  `json:"expires_at_ms"`
}

type SSHSessionInfo struct {
	ID          string `json:"id"`
	Sandbox     string `json:"sandbox"`
	Subject     string `json:"subject,omitempty"`
	CreatedAtMS int64  `json:"created_at_ms"`
	ExpiresAtMS int64  `json:"expires_at_ms,omitempty"`
	Revoked     bool   `json:"revoked,omitempty"`
}

type LogLine struct {
	TS     time.Time `json:"ts"`
	Source string    `json:"source"`
	Level  string    `json:"level"`
	Text   string    `json:"text"`
}
