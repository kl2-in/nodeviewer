BINARY     := knv
BUILD_DIR  := ./bin
INSTALL_DIR := /usr/local/bin
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -s -w"
GOFLAGS    :=

.PHONY: all build install uninstall clean run mock help

all: build

## build: compile the binary into ./bin/knv
build:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .
	@echo "built: $(BUILD_DIR)/$(BINARY)"

## install: build and copy binary to $(INSTALL_DIR)  (may need sudo)
install: build
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "installed: $(INSTALL_DIR)/$(BINARY)"

## uninstall: remove binary from $(INSTALL_DIR)
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "removed: $(INSTALL_DIR)/$(BINARY)"

## run: build and run against current kubeconfig context
run: build
	$(BUILD_DIR)/$(BINARY)

## mock: build and run with mock data (no cluster needed)
mock: build
	$(BUILD_DIR)/$(BINARY) --mock

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## help: show this help
help:
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
