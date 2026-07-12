BINARY  := pulse
DIST    := dist
GOOS    ?= linux
LDFLAGS := -s -w
GOFLAGS := -trimpath

.PHONY: build prod clean

# Quick local build for development.
build:
	go build -o $(BINARY) .

# Optimized, stripped, static builds for release (amd64 + arm64).
# -s -w drops the symbol table & DWARF; -trimpath removes local paths;
# CGO_ENABLED=0 makes a fully static binary.
prod: clean
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$(GOOS)-amd64 .
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$(GOOS)-arm64 .
	@ls -lh $(DIST)

clean:
	rm -rf $(BINARY) $(DIST)
