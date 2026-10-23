# Roadmap — quarshell-sdk

Status: pre-stable API · Current module tag: `v0.1.1` · Dependencies are listed in `go.mod`.

## This module

| ID | Item | Notes |
|----|------|-------|
| S1 | **API coverage** | Proposals, providers, effective-policy helpers |
| S2 | **Stable client surface** | Freeze `go/cautem` for alpha consumers |
| S3 | **Examples** | Minimal create/list/exec snippets in README |
| S4 | **Standalone consumer** | A nested consumer module verifies the public SDK with `GOWORK=off` and versioned dependencies; release-tag consumer verification remains a publication gate |


## Release

This module follows its own pre-stable 0.x version sequence. Publish dependencies
first, update consumers to the published tags, and verify with `GOWORK=off`. Never
rewrite a published tag.
