.PHONY: build fmt fmt-check lint vet check test

GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2

build:
	go build -trimpath -o bin/unreal-review ./cmd/unreal-review

fmt:
	$(GOLANGCI_LINT) fmt ./...

fmt-check:
	$(GOLANGCI_LINT) fmt --diff ./...

lint:
	$(GOLANGCI_LINT) run ./...

vet:
	go vet ./...

test:
	go test ./...

check: fmt-check lint vet test

install-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
