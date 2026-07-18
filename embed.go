package main

import _ "embed"

//go:embed frontend/dist/index.html
var indexHTML []byte

//go:embed frontend/dist/sw.js
var swJS []byte

//go:embed frontend/dist/manifest.webmanifest
var manifestJSON []byte
