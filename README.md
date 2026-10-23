<h1 align="center">quarshell-sdk</h1>

<p align="center">
  <strong>Go client for quarshell-gateway</strong><br>
  generated native OpenShell gRPC resources.
</p>
<p align="center">
  <a href="https://github.com/KLORQ/quarshell-sdk/actions/workflows/ci.yml"><img src="https://github.com/KLORQ/quarshell-sdk/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/KLORQ/quarshell-sdk"><img src="https://pkg.go.dev/badge/github.com/KLORQ/quarshell-sdk.svg" alt="Go Reference"></a>
  <a href="https://www.apache.org/licenses/LICENSE-2.0"><img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg" alt="License"></a>
  <a href="https://github.com/KLORQ/quarshell-sdk"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go" alt="Go Version"></a>
</p>
<p align="center">
  <sub>Part of the <a href="https://github.com/KLORQ">cautem / cautem</a> ecosystem</sub>
</p>

---

## Overview

The [gateway guide](https://klorq.github.io/sandbox.dev/guides/gateway/) describes the service this client calls. SDK operations use the pinned OpenShell v0.1.4 RPC schema through bindings generated in `quarshell-proto`. Managed inference routes were removed by OpenShell; sandboxes attach providers and call their native endpoints.

**quarshell-sdk** talks to [quarshell-gateway](https://github.com/KLORQ/quarshell-gateway) through typed RPC clients. Its methods use these transports:

| Transport | SDK methods |
| --- | --- |
| OpenShell gRPC | Sandbox lifecycle, inventory and resumable watch; logs; workload templates; providers/profiles; policy reads, updates, drafts and history; services; workspaces; SSH; settings; gateway info and current user |
| HTTP health probe | `Healthz` (`GET /healthz`) |

OpenShell page methods reject negative sizes, cap requests above 1000 records,
and use the Gateway default when page size is zero.

For a Gateway-managed create with named browser services, set
`Sandbox.ServiceExposures` to `[]cautem.ServiceExposure` before calling
`CreateControlSandboxOperation`. The result includes `ServiceURLs`. The Gateway
validates exposure names and ports before provisioning and attempts cleanup if
registration fails; a runtime cleanup failure is returned for reconciliation.

`ListServicesPage(ctx, sandbox, pageSize, pageToken, allWorkspaces)` uses the
pinned OpenShell `ListServices` RPC and returns the next page token.
Cross-workspace listing is authorized by the Gateway for platform
administrators. `ListServicesAllPages(ctx, sandbox, allWorkspaces)` collects
the selected scope and rejects duplicate endpoints or repeated cursors.
Service records retain the Gateway URL, endpoint ID, sandbox ID, workspace,
domain flag, and authorization mode. The Gateway applies `STRIP` when the mode
is omitted or unspecified. Use `ExposeServiceWithOptions` or
`PutServiceWithOptions` to select `ServiceAuthorizationModeStrip` or
`ServiceAuthorizationModeBearerPassthrough`; passthrough forwards one valid
Bearer Authorization header to the service over HTTP and WebSocket.
`ExposeService` keeps browser-facing URL registration enabled by default; use
`ExposeServiceWithDomain` to explicitly disable it. The compatibility
`PutService` keeps the same default, with `PutServiceWithDomain` available for
explicit URL control.
`ListProvidersAllWorkspaces(ctx)` collects the global provider catalog, sorts
by workspace/name, and relies on Gateway platform-administrator authorization.

`ListPolicyRevisionsPage(ctx, sandbox, pageSize, pageToken)` returns one
workspace-scoped OpenShell policy history page. `ListPolicyRevisionsGateway`
collects the full history and errors if the Gateway repeats a cursor or a
revision. `ListPolicyRevisions` is a convenience projection of the same native
history and reports zero byte count because OpenShell does not return YAML sizes.
Platform administrators can use `ListGlobalPolicyRevisionsPage` or
`ListGlobalPolicyRevisionsGateway` for global policy history.

Interactive byte streams stay on the CLI (`cautem connect`). New gateway operations use the existing gRPC contracts.

### Key Features

| Category | Capabilities |
|----------|--------------|
| **Client** | `go/cautem` — resource facade over generated RPC clients; some legacy HTTP methods remain during migration |
| **Execution** | OpenShell `ExecSandbox` RPC; interactive TTY stays on the CLI |
| **Proposals** | List / get / approve / reject |

---

## Installation

For source development, use the sibling `go.work` workspace and run `go test ./...` here. `tests/consumer` is an external-module smoke test: it builds the public SDK with `GOWORK=off`, using only the versioned `quarshell-proto` dependency. CI runs that consumer independently of the monorepo workspace. The control client depends on the gateway's generated Go contract package. Publish the gateway beta containing that package before publishing the matching SDK beta.

**Requirements:** Go 1.27+

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/KLORQ/quarshell-sdk/go/klorq"
)

func main() {
    c := cautem.New("http://127.0.0.1:7443")
    list, err := c.ListControlSandboxes(context.Background(), "default", false)
    if err != nil {
        panic(err)
    }
    fmt.Println(list)
}
```

---

## Package Structure

| Path | Purpose |
|------|---------|
| `go/cautem` | Public Go SDK |
| `internal/gatewayclient/` | Minimal HTTP health transport and shared SDK record types (not importable outside the module) |

`ListControlSandboxes(ctx, workspace, allWorkspaces)`, `GetControlSandbox`,
`GetControlSandboxLogs`, and `WatchControlSandboxLogs` use the pinned OpenShell
Gateway service. `Whoami(ctx)` uses `GetViewer`. `List(ctx)` uses the Gateway API across all workspaces
visible to the caller, while `Get`, `Start`, `Stop`, and `Delete` use
`Client.Workspace` (defaulting to `default`). `Exec(ctx, name, argv...)` uses
that workspace and the locally generated OpenShell gateway gRPC bindings. The CLI uses native gRPC for `sandbox list/get`, filtered log snapshots, and a single
sandbox's live log stream. `logs --all` enumerates caller-visible workspaces and
uses bounded concurrent gRPC streams (up to 24 sandboxes per invocation).

`UploadRootfsTar(ctx, fileName, size, source)` requests an OpenShell staging
slot over gRPC and copies the archive to the returned gateway-local path. The
client must share the gateway filesystem, matching the upstream CLI contract.
Put the returned token in the selected compute driver's
`rootfs_tar_staging_token` config when creating the sandbox.

`CreateWorkspaceWithLabels(ctx, name, labels)` creates a workspace with its
labels through the generated workspace RPC; `CreateWorkspace(ctx, name)` remains
available for workspaces without labels.

`UpdateSandboxPolicyMerge(ctx, sandbox, operations)` submits an atomic batch of
OpenShell policy merge operations through native Gateway gRPC in the selected
workspace. The SDK reads the sandbox resource version first and fails with a
conflict if it changes before the merge. The Gateway applies and delivers the
resulting revision before the call returns.

`ListWorkspacesPage(ctx, WorkspaceListOptions{PageSize, PageToken, LabelSelector})`
returns one page of workspaces and its opaque `NextPageToken`. Label selector
pairs use the upstream `key=value,key=value` syntax and are combined with AND;
pass the returned token in the next call to continue pagination.


---

## Related

| Resource | Link |
|----------|------|
| Roadmap | [ROADMAP.md](./ROADMAP.md) |
| Organization | [https://github.com/KLORQ](https://github.com/KLORQ) |
| Organization overview | [github.com/KLORQ](https://github.com/KLORQ) |
| pkg.go.dev | [`github.com/KLORQ/quarshell-sdk`](https://pkg.go.dev/github.com/KLORQ/quarshell-sdk) |

## License

[Apache-2.0](./LICENSE) © cautem
