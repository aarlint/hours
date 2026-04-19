.PHONY: build build-arm64 build-all install clean run test release deps app dev mcp-install

VERSION ?= dev
WAILS := $(shell go env GOPATH)/bin/wails

# ---- Headless / legacy builds (MCP stdio + --serve HTTP) ----

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o hours-mcp .

build-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o hours-mcp .

build-all:
	./scripts/build-all.sh $(VERSION)

install: build-arm64
	mkdir -p ~/.local/bin
	cp hours-mcp ~/.local/bin/
	@echo "Installed to ~/.local/bin/hours-mcp"

# ---- Native Wails app bundle ----

app:
	cd internal/web && npm install && npm run build
	$(WAILS) build -clean -platform darwin/arm64 -ldflags="-X main.version=$(VERSION)"
	@echo ""
	@echo "Built build/bin/Hours.app"
	@echo "Drag it to /Applications, then point Claude Desktop at:"
	@echo "  /Applications/Hours.app/Contents/MacOS/Hours --mcp"

dev:
	$(WAILS) dev

# ---- MCP config helper ----

INSTALL_DIR ?= $(HOME)/Applications

mcp-install: app
	@mkdir -p "$(INSTALL_DIR)"
	@if [ -d "$(INSTALL_DIR)/Hours.app" ]; then \
		echo "Hours.app already in $(INSTALL_DIR) — replacing"; \
		rm -rf "$(INSTALL_DIR)/Hours.app"; \
	fi
	cp -R build/bin/Hours.app "$(INSTALL_DIR)/"
	@echo ""
	@echo "Installed to $(INSTALL_DIR)/Hours.app"
	@echo ""
	@echo "Claude Desktop config:"
	@echo '  "hours": {'
	@echo '    "command": "$(INSTALL_DIR)/Hours.app/Contents/MacOS/Hours",'
	@echo '    "args": ["--mcp"]'
	@echo '  }'
	@echo ""
	@echo "To install to /Applications instead: sudo make mcp-install INSTALL_DIR=/Applications"

# ---- Misc ----

release: build-all
	tar -czf hours-mcp-$(VERSION).tar.gz -C dist .

clean:
	rm -f hours-mcp hours-mcp-*.tar.gz
	rm -rf dist/ build/

run:
	go run . --serve

test:
	go test ./...

deps:
	go mod download
	go mod tidy
