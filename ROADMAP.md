# Roadmap — quarshell-sdk

Status: **v0.3.0** (release prepared) · Uses [quarshell-proto](https://github.com/KLORQ/quarshell-proto) `v0.2.0` and talks to [quarshell-gateway](https://github.com/KLORQ/quarshell-gateway)

## This module

| ID | Item | Notes |
|----|------|-------|
| S1 | **API coverage** | Proposals, providers, effective-policy helpers |
| S2 | **Stable client surface** | Freeze `go/cautem` for alpha consumers |
| S3 | **Examples** | Minimal create/list/exec snippets in README |
| S4 | **Standalone consumer** | A nested consumer module verifies the public SDK with `GOWORK=off` and versioned dependencies; release-tag consumer verification remains a publication gate |

## Release

Leaf module for the Go release cascade; it consumes the published protocol module.
