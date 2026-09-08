.PHONY: all test test-cgo0 test-race build build-examples benchmark cross-compile fmt vet clean help

all: test-cgo0 build

## test: Run standard unit tests
test:
	go test -v ./...

## test-cgo0: Run unit tests strictly without cgo (CGO_ENABLED=0)
test-cgo0:
	CGO_ENABLED=0 go test -v ./...

## test-race: Run race detector on handwritten Go packages
test-race:
	go test -race -gcflags=all=-d=checkptr=0 -v . ./ogg ./wav ./player ./test ./opus

## build: Build all command-line binaries in cmd/
build:
	go build -v ./cmd/...

## build-examples: Verify all example submodules build cleanly
build-examples:
	@for dir in examples/*/; do \
		echo "Building $$dir..."; \
		(cd "$$dir" && go build -o /dev/null .) || exit 1; \
	done

## benchmark: Run benchmarks with memory allocations
benchmark:
	go test -v -bench=. -benchmem -run=^$$ ./ogg ./opus

## cross-compile: Verify cross-compilation across platforms with CGO_ENABLED=0
cross-compile:
	@for target in "linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64" "windows/amd64" "windows/arm64"; do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		echo "Cross-compiling for $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build ./... || exit 1; \
	done

## fmt: Format all handwritten Go source files
fmt:
	@find . -name "*.go" -not -path "./opuscc/*" -not -path "./opusccenc/*" -exec gofmt -w {} +

## vet: Run go vet on handwritten packages (excluding transpiled C shims)
vet:
	@output=$$(go vet -unsafeptr=false ./... 2>&1 | grep -vE 'opuscc|opusccenc' || true); \
	if [ -n "$$output" ]; then \
		echo "$$output"; \
		exit 1; \
	else \
		echo "go vet passed successfully across all packages."; \
	fi

## clean: Remove build artifacts and temporary files
clean:
	rm -f oggopus2wav oggopusdump oggopusextract wav2oggopus *.pprof
	go clean -cache -testcache

## help: Display this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | awk -F ': ' '{printf "  %-16s %s\n", $$1, $$2}'
