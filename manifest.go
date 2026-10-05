package manifest

import _ "embed"

// ManifestJSON is the module's muxcore.json, embedded so the reported version
// has a single source (ADR-0021, docs/adr/0021-module-version-single-source.md).
//
//go:embed muxcore.json
var ManifestJSON []byte
