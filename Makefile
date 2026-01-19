.PHONY: all build generate clean test proto-clean help

# Build all binaries
all: generate build

# Generate proto files
generate:
	@echo "Generating proto files..."
	@export PATH="$$PATH:$$HOME/go/bin" && buf generate
	@echo "✓ Proto files generated"

# Build all Go binaries
build:
	@echo "Building binaries..."
	@go build -o bin/control-plane ./cmd/control-plane
	@go build -o bin/node ./cmd/node
	@go build -o bin/navarch ./cmd/navarch
	@echo "✓ Binaries built in bin/"

# Run tests
test:
	@echo "Running tests..."
	@go test ./pkg/controlplane/... -v

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -cover -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"

# Clean generated proto files
proto-clean:
	@echo "Cleaning generated proto files..."
	@rm -f proto/*.pb.go
	@rm -f proto/protoconnect/*.connect.go
	@echo "✓ Generated proto files removed"

# Clean binaries
bin-clean:
	@echo "Cleaning binaries..."
	@rm -rf bin/
	@rm -f control-plane node navarch
	@echo "✓ Binaries removed"

# Clean everything (proto files, binaries, test artifacts)
clean: proto-clean bin-clean
	@echo "Cleaning test artifacts..."
	@rm -f coverage.out coverage.html
	@go clean -testcache
	@echo "✓ All generated files cleaned"

# Install required tools
install-tools:
	@echo "Installing required tools..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
	@echo "✓ Tools installed"

# Run linter
lint:
	@echo "Running linter..."
	@go vet ./...
	@echo "✓ Lint complete"

# Format Go code
fmt:
	@echo "Formatting Go code..."
	@go fmt ./...
	@echo "✓ Code formatted"

# Format proto files
proto-fmt:
	@echo "Formatting proto files..."
	@export PATH="$$PATH:$$HOME/go/bin" && buf format proto/ --write
	@echo "✓ Proto files formatted"

# Full check (format, lint, test)
check: fmt proto-fmt lint test
	@echo "✓ All checks passed"

# Help target
help:
	@echo "Navarch Makefile targets:"
	@echo ""
	@echo "  make all           - Generate protos and build all binaries (default)"
	@echo "  make generate      - Generate proto files"
	@echo "  make build         - Build all Go binaries"
	@echo "  make test          - Run all tests"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make clean         - Remove all generated files and binaries"
	@echo "  make proto-clean   - Remove only generated proto files"
	@echo "  make bin-clean     - Remove only binaries"
	@echo "  make install-tools - Install required build tools"
	@echo "  make lint          - Run Go linter"
	@echo "  make fmt           - Format Go code"
	@echo "  make proto-fmt     - Format proto files"
	@echo "  make check         - Run format, lint, and tests"
	@echo "  make help          - Show this help message"

