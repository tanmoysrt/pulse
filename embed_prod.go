//go:build prod

package main

import _ "embed"

//go:embed frontend/dist/index.html
var indexHTML []byte
