.PHONY: build fmt fmt-check lint vet check test prove canary

GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2
BEND ?= bend

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

prove:
	BEND=$(BEND) sh tools/prove.sh

check: fmt-check lint vet test prove

canary: build
	bash tools/canary.sh
	UNREAL_REVIEW_BIN=$(CURDIR)/bin/unreal-review go test -tags canary -count=1 -timeout 180s ./cmd/unreal-review -run '^TestCanary'

install-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
