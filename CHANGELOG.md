# Changelog

## [Unreleased]

### Removed

- Remove managed inference-route methods from the Go SDK and legacy HTTP client.

### Added

- Include service exposures in Gateway-managed sandbox create requests and return their URLs with the create result.
- Add explicit service authorization options for strip and bearer passthrough modes.
- Reject duplicate provider, attachment, profile, template, workspace, workspace-member, Control sandbox and proposal records in all-pages SDK methods.
- Cap all Gateway page requests at 1000 records while preserving zero as the server-default size.
- Add cursor-paged OpenShell sandbox policy history and an all-pages wrapper that rejects repeated cursors or revision IDs.
- Add workspace/global policy history page methods; global history uses the Gateway's platform-admin authorization.
- Add a duplicate-safe all-pages provider listing across workspaces for platform administrators.
- Add a duplicate-safe all-pages OpenShell service listing for workspace or platform-admin global scope.
- Preserve Gateway service endpoint URLs, IDs, workspace, domain, and authorization mode in Go SDK records and CLI listings.
- Export service authorization mode names and support explicit strip/bearer passthrough options for service endpoints.
- Add `ExposeServiceWithDomain` to select browser-facing URL registration.
- Add `PutServiceWithDomain` while preserving the existing `PutService` default.
- Preserve IDs, labels, status, resource versions, and creation times when aggregating workspace pages.

## [v0.3.0] - 2026-10-16

### Added

- Add typed APIs for main-process attachment, interactive exec, rootfs upload, workload templates, and workspace listing.
- Add a versioned external Go consumer check for the published SDK surface.

### Changed

- Update the Go client to the published control protocol v0.2.0 and current Gateway resource signatures.
- Refresh `golang.org/x/net` to v0.61.0 and its compatible `x/sync`, `x/sys`, and `x/text` dependencies.

## [v0.2.0] - 2026-10-13

### Changed

- Split the Go control client into focused lifecycle, sandbox, log, and overview files.
- Use generated first-party API types from the published `quarshell-proto` module.

## [v0.1.6] - 2026-10-11

### Changed

- Rename the module, runtime identifiers and project references to the `quarshell` namespace.

## [v0.1.0-beta.1] - 2026-10-10

### Added

- Provide typed Go clients for Control RPC gateway information, inference, sandbox and log management, services, settings and secrets.
- Carry request IDs, resource versions and operation recovery through the management client.

## [v0.1.0-alpha.2] - 2026-10-07

### Added

- Scoped provider profile catalog operations and refresh metadata in the public client API.
- Match OpenShell gateway request/response payloads for workspace services and SSH relay operations.
- Encode URL path segments and decode response bodies with operation context instead of discarding transport details.

### Changed

- Distribute the module under Apache-2.0 and link the public gateway/API documentation from the README.

## [v0.0.2-alpha.1] - 2026-09-28

### Added

- `Proposal.SecurityFlagged` for gateway approval flows that gate bulk approve.
