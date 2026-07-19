BINARY    := pulse
DIST      := dist
FRONTEND  := frontend
VERSION   ?= dev
LDFLAGS   := -s -w -X main.version=$(VERSION)
GOFLAGS   := -trimpath
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build prod frontend clean

# Quick backend build; embeds the placeholder UI (see frontend/placeholder.html).
build:
	go build -o $(BINARY) .

# Build the Vue UI into frontend/dist (not committed; embedded via -tags prod).
frontend:
	cd $(FRONTEND) && npm install && npm run build

# Optimized, stripped, static release builds with the real UI baked in, one per
# platform. -s -w drops the symbol table & DWARF; -trimpath removes local paths;
# CGO_ENABLED=0 makes fully static binaries; -tags prod embeds frontend/dist.
prod: clean frontend
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $(BINARY)-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -tags prod $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-$$os-$$arch .; \
	done
	@ls -lh $(DIST)

clean:
	rm -rf $(BINARY) $(DIST) $(FRONTEND)/dist
