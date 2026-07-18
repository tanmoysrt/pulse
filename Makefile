BINARY   := pulse
DIST     := dist
FRONTEND := frontend
GOOS     ?= linux
LDFLAGS  := -s -w
GOFLAGS  := -trimpath

.PHONY: build prod frontend clean

# Quick local build for development (embeds the committed frontend/dist).
build:
	go build -o $(BINARY) .

# Build the Vue UI into frontend/dist, which the Go binary embeds.
frontend:
	cd $(FRONTEND) && npm install && npm run build

# Optimized, stripped, static builds for release (amd64 + arm64), UI included.
# -s -w drops the symbol table & DWARF; -trimpath removes local paths;
# CGO_ENABLED=0 makes a fully static binary.
prod: clean frontend
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$(GOOS)-amd64 .
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$(GOOS)-arm64 .
	@ls -lh $(DIST)

clean:
	rm -rf $(BINARY) $(DIST)
