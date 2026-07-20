package main

import _ "embed"

// sw.js and manifest are small committed static assets; index.html is embedded
// per build (placeholder for `go build`, the real UI for `-tags prod`).

//go:embed frontend/public/sw.js
var swJS []byte

//go:embed frontend/public/manifest.webmanifest
var manifestJSON []byte

//go:embed frontend/public/icons/icon-192.png
var icon192PNG []byte

//go:embed frontend/public/icons/icon-512.png
var icon512PNG []byte

//go:embed frontend/public/icons/apple-touch-icon.png
var appleTouchIconPNG []byte

//go:embed frontend/public/icons/badge.png
var badgePNG []byte
