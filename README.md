# Downloader Native Usenet

Native NZB/usenet engine for MuxCore — a SABnzbd replacement in the same role as `downloader-native-torrent` is for qBittorrent.

## Key features

- Same gRPC API as `downloader-sabnzbd` (`UsenetDownloaderService`: `AddNZB`, queue/history, pause/resume/delete)
- **Fixture engine (default):** writes a parseable `.mkv` under the download dir and completes instantly (CI / MVP smoke)
- **Live engine:** fetches NZB over HTTP(S), downloads segments over NNTP, yEnc decode via [`go-newsgroups/nzb`](https://github.com/go-newsgroups/nzb)
- **Post-processing (SABnzbd parity):** PAR2 verify/repair ([`par2engine`](https://github.com/hobeone/par2engine)), archive unpack (ZIP native; RAR/7z via `unrar`/`7z` on PATH)
- Queue statuses: `Downloading`, `Verifying`, `Repairing`, `Extracting`, `Completed`, `Failed`
- Publishes `download.started`, `download.completed`, `download.failed` on the core event bus when mesh-connected
- Mesh `settings` capability (download dir, engine mode, NNTP provider, post-processing)

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `USENET_GRPC_ADDR` / `MUXCORE_GRPC_ADDR_OVERRIDE` | `:9622` | gRPC listen address |
| `MUXCORE_HTTP_ADDR` | `:9623` | Health HTTP (`/healthz`) |
| `USENET_DOWNLOAD_DIR` / `DOWNLOAD_DIR` | `/var/lib/downloader-native-usenet/downloads` | Completed download root |
| `USENET_ENGINE` | `fixture` | `fixture` (offline) or `live` (NNTP) |
| `USENET_PAR2` | `auto` | `auto` / `skip` / `require` — PAR2 verify + repair when `.par2` present |
| `USENET_UNPACK` | `auto` | `auto` / `skip` / `require` — extract `.zip`/`.rar`/`.7z` after download |
| `USENET_CLEANUP` | `false` | Delete `.par2`/archive sidecar files after successful unpack |
| `UNRAR_CMD` | `unrar` on PATH | RAR extraction binary |
| `SEVENZIP_CMD` | `7z`, `7zz`, `7za` | 7-Zip extraction binary |
| `NNTP_HOST` | — | Usenet provider hostname (required for live) |
| `NNTP_PORT` | `563` (TLS) / `119` | NNTP port |
| `NNTP_USER` / `NNTP_PASS` | — | AUTHINFO credentials |
| `NNTP_SSL` | `false` | Use implicit TLS (`DialTLS`) |
| `NNTP_TLS_INSECURE` | `false` | Skip TLS verify (operator opt-in) |
| `MUXCORE_GRPC_ADDR` | — | Core mesh (events) |

## Capabilities

- `downloader.usenet` — discovered by `media-automation` for usenet dispatch
- `downloader.native.usenet`
- `settings`

## Optional enhancements (not required for automation parity)

- Multi-provider / backup NNTP server pooling
- WireGuard VPN binding (see `downloader-native-torrent`)
- Direct-unpack while downloading (SABnzbd optimization)

## Development

```bash
make test   # fixture + postproc unit tests; no usenet provider
make build
```
