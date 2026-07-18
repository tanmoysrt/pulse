//go:build !prod

package main

import _ "embed"

//go:embed frontend/placeholder.html
var indexHTML []byte
