# Makefile for Terraform PR Generator

BINARY_NAME=terraform-pr-generator
GOPATH=$(shell go env GOPATH)
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: build clean install test run help deps lint fmt vet

# Default target
help:
	@echo "🚀 Terraform PR Generator"
	@echo ""
	@echo "Available targets:"
	@echo "  build    - Build the binary"
	@echo "  install  - Install binary to GOPATH/bin"
	@echo "  clean    - Remove built binaries"
	@echo "  test     - Run tests"
	@echo "  run      - Run with example module (requires MODULE variable)"
	@echo "  deps     - Install/update dependencies"
	@echo "  lint     - Run golangci-lint (if available)"
	@echo "  fmt      - Format code"
	@echo "  vet      - Run go vet"
	@echo "  help     - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make run MODULE=s3_malware_protection"
	@echo "  make install"

# Build the binary
build: fmt vet
	@echo "🔨 Building $(BINARY_NAME) $(VERSION)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "✅ Build complete: ./$(BINARY_NAME)"

# Install to GOPATH/bin
install: build
	@echo "📦 Installing $(BINARY_NAME) to $(GOPATH)/bin..."
	cp $(BINARY_NAME) $(GOPATH)/bin/
	@echo "✅ Installed! You can now run '$(BINARY_NAME)' from anywhere"

# Clean built binaries
clean:
	@echo "🧹 Cleaning up..."
	rm -f $(BINARY_NAME)
	go clean
	@echo "✅ Clean complete"

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -v ./...

# Run with example (use: make run MODULE=s3_malware_protection)
run: build
	@if [ -z "$(MODULE)" ]; then \
		echo "❌ Please specify MODULE variable: make run MODULE=s3_malware_protection"; \
		exit 1; \
	fi
	@echo "🚀 Running $(BINARY_NAME) with module: $(MODULE)"
	./$(BINARY_NAME) $(MODULE) --verbose

# Development dependencies
deps:
	@echo "📥 Installing/updating dependencies..."
	go mod tidy
	go mod download
	@echo "✅ Dependencies updated"

# Lint code (requires golangci-lint)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "🔍 Running golangci-lint..."; \
		golangci-lint run; \
	else \
		echo "⚠️  golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Format code
fmt:
	@echo "🎨 Formatting code..."
	go fmt ./...

# Vet code
vet:
	@echo "🔍 Running go vet..."
	go vet ./...

# Show binary info
info: build
	@echo "📊 Binary information:"
	@ls -lh $(BINARY_NAME)
	@echo ""
	@./$(BINARY_NAME) --help

# Release build (with optimizations)
release:
	@echo "🚀 Building release version..."
	CGO_ENABLED=0 go build -a -installsuffix cgo $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "✅ Release build complete"

# Cross-platform builds
build-all:
	@echo "🌍 Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe .
	@echo "✅ Cross-platform builds complete"

# Git shortcuts
tag:
	@echo "Current tags:"
	@git tag -l | tail -5
	@echo ""
	@echo "Create new tag with: git tag v1.0.0 && git push origin v1.0.0"