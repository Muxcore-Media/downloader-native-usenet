# Changelog

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
