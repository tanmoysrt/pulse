package main

import _ "embed"

// sw.js and manifest are small committed static assets; index.html is embedded
// per build (placeholder for `go build`, the real UI for `-tags prod`).

//go:embed frontend/public/sw.js
var swJS []byte

//go:embed frontend/public/manifest.webmanifest
var manifestJSON []byte
