# Changelog

## [Unreleased]

### Security
- NZB fetches use netguard. Allowing private hosts no longer skips the metadata and link-local checks. The strict profile still refuses private and loopback (NFR-SEC-009).

## [0.2.4] - 2026-10-05


### Security
- gRPC server and peer dials use mesh TLS (meshtls, sdk/go/module v0.6.5) unless the dev insecure flag is set (ADR-0016/0017).

## [0.2.3] - 2026-10-05

### Changed
- Built on core v0.6.14 / sdk/go/module v0.6.4: unregisters on shutdown and re-registers after core restarts (ADR-0022).

## [0.2.2] - 2026-10-05


### Changed
- Reported version comes from muxcore.json (ADR-0021); built on core v0.6.12 / sdk/go/module v0.6.3 (mesh enrollment, ADR-0017).

## [0.2.0] - 2026-10-05

### Changed
- CI runs on GitHub-hosted runners from the umbrella template; retired-origin workflows removed.
- Dependencies resolve from published GitHub tags (no filesystem `replace`); requires core v0.6.0.

## v0.2.0 — 2026-08-21

- SABnzbd parity post-processing: PAR2 verify/repair (`par2engine`), archive unpack (ZIP/RAR/7z)
- Queue statuses: Verifying, Repairing, Extracting
- Settings/env: `USENET_PAR2`, `USENET_UNPACK`, `USENET_CLEANUP`, `UNRAR_CMD`, `SEVENZIP_CMD`

## v0.1.0 — 2026-08-21

- Initial native usenet module (`downloader-native-usenet`)
- `UsenetDownloaderService` gRPC API (compatible with `downloader-sabnzbd`)
- Fixture engine for offline MVP / CI smoke
- Live NNTP engine via `go-newsgroups/nzb` + `nntp`
- `download.started` / `download.completed` / `download.failed` events
- Mesh settings (download dir, engine, NNTP provider)
- MVP stack opt-in: `MVP_ENABLE_DOWNLOADER_USENET=1`
