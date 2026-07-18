package main

import _ "embed"

//go:embed web/index.html
var indexHTML []byte

//go:embed web/sw.js
var swJS []byte

//go:embed web/manifest.webmanifest
var manifestJSON []byte
