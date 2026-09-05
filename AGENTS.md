# AGENTS.md — downloader-native-usenet

MuxCore sidecar module (`downloader-native-usenet`). Workspace deploy and SSH: [`../AGENTS.md`](../AGENTS.md). Default ports: [`_mvp/PORTS.md`](../_mvp/PORTS.md).

## Module identity

| Field | Value |
|-------|-------|
| Directory | `downloader-native-usenet` |
| Capabilities | see muxcore.json |
| Contracts | `UsenetDownloaderService` (local proto, same RPC surface as `downloader-sabnzbd`) |

## Agent rules

- Modules run as gRPC sidecars; capabilities are the security boundary.
- TLS required in production (`MUXCORE_INSECURE_DISABLE_TLS` is dev-only).
- Default engine is `fixture` (offline smoke). Live NNTP requires `NNTP_HOST` (+ credentials).
- Match existing Go patterns; run `gofmt` and package tests before finishing.
- Cross-module events: prefer `github.com/Muxcore-Media/contracts-media/events` over deprecated `core/pkg/contracts` aliases.

## Build

```bash
cd downloader-native-usenet
go test ./...
```
