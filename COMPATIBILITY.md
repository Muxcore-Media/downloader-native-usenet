# Compatibility

## Core Version

| Module Version | Core Version | Status |
|----------------|-------------|--------|
| v0.2.0         | 0.5.2+      | Current |
| v0.1.0         | 0.5.2+      | Superseded |

## Capabilities

- `downloader` / `downloader.usenet` / `downloader.native.usenet` / `usenet`
- `settings`

## Notes

Embeds a native NZB/NNTP engine (`go-newsgroups/nzb` + `nntp`) with SABnzbd-class post-processing:

- PAR2 verify/repair via `par2engine`
- Archive unpack: ZIP (stdlib), RAR/7z (external `unrar` / `7z`)

Drop-in replacement for `downloader-sabnzbd` at the same `UsenetDownloaderService` gRPC API.
