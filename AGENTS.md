# Agent instructions

## Scope

This repository contains the Go client for the cautem gateway, including
generated gRPC clients and higher-level sandbox APIs.

## Working rules

- Preserve public API and wire compatibility. Check generated protocol sources
  and update them from their schemas instead of editing generated files by hand.
- Keep REST and native gRPC usage claims in the README synchronized with the
  actual client methods and gateway routes.
- Keep local Go workspace replacements in the parent workspace's `go.work`,
  never in this repository's `go.mod`.

## Checks

- Run `go test ./...` from this checkout for Go changes.
- From the parent workspace, `task check` runs the shared Go gate; verify clean
  published dependency builds for module path or version changes.
