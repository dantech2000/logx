set dotenv-load

# List available commands
default:
    @just --list

# Build the CLI tool
build:
    go build -o bin/logx main.go
    go build -o bin/kubectl-logx main.go

# Run tests
test:
    go test ./...

# Build with version information
build-version:
    #!/usr/bin/env bash
    VERSION=$(git describe --tags --always --dirty)
    COMMIT_HASH=$(git rev-parse --short HEAD)
    BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    LDFLAGS="-X github.com/dantech2000/logx/pkg/version.commitHash=${COMMIT_HASH} -X github.com/dantech2000/logx/pkg/version.buildDate=${BUILD_DATE}"
    go build -ldflags "${LDFLAGS}" -o bin/logx main.go
    go build -ldflags "${LDFLAGS}" -o bin/kubectl-logx main.go

# Cross-compile for multiple platforms
cross-compile:
    #!/usr/bin/env bash
    VERSION=$(git describe --tags --always --dirty)
    COMMIT_HASH=$(git rev-parse --short HEAD)
    BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    LDFLAGS="-X github.com/dantech2000/logx/pkg/version.commitHash=${COMMIT_HASH} -X github.com/dantech2000/logx/pkg/version.buildDate=${BUILD_DATE}"
    
    GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/logx-linux-amd64 main.go
    GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/kubectl-logx-linux-amd64 main.go
    GOOS=darwin GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/logx-darwin-amd64 main.go
    GOOS=darwin GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/kubectl-logx-darwin-amd64 main.go
    GOOS=windows GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/logx-windows-amd64.exe main.go
    GOOS=windows GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/kubectl-logx-windows-amd64.exe main.go

# Run linter
lint:
    golangci-lint run

# Format code
fmt:
    go fmt ./...

# Clean build artifacts
clean:
    rm -rf bin/*

# Install dependencies
deps:
    go mod tidy
    go mod verify

# Run the CLI tool
run *ARGS:
    go run main.go {{ ARGS }}

# Create a new release with the specified version
release version:
    #!/usr/bin/env bash
    echo "Attempting to create release with version: {{version}}"
    if [[ ! "{{version}}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Error: Version must be in the format v0.0.0"
        exit 1
    fi
    
    if git rev-parse {{version}} >/dev/null 2>&1; then
        echo "Error: Tag {{version}} already exists."
        echo "To force update the tag, use: just release-force {{version}}"
        exit 1
    fi
    
    echo "Version format valid, creating tag..."
    git tag -a {{version}} -m "Release {{version}}"
    git push origin {{version}}

# Force update an existing release version
release-force version:
    #!/usr/bin/env bash
    echo "Force updating release version: {{version}}"
    if [[ ! "{{version}}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Error: Version must be in the format v0.0.0"
        exit 1
    fi
    
    git tag -fa {{version}} -m "Release {{version}}"
    git push origin {{version}} --force

# Generate and update changelog
changelog:
    git-chglog -o CHANGELOG.md

# Run goreleaser to create a new release
release-goreleaser:
    #!/usr/bin/env bash
    if [ ! -f .env ]; then
        echo "Error: .env file not found"
        exit 1
    fi
    set -a
    source .env
    set +a
    goreleaser release

# Build and run in one step
build-and-run *ARGS: build
    ./bin/logx {{ ARGS }}
