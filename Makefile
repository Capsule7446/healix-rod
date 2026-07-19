SHELL := bash

GO ?= go
GOTOOLS_BIN ?= $(CURDIR)/.devtools/bin
COVERAGE_FILE ?= coverage.out

.PHONY: fmt fmt-check test-short test race coverage vet build vulncheck gosec check clean

fmt:
	$(GO)fmt -w .

fmt-check:
	@test -z "$$($(GO)fmt -l .)"

test-short:
	$(GO) test -short ./...

test:
	$(GO) test ./... -count=1 -timeout 20m

race:
	CGO_ENABLED=1 $(GO) test -race ./...

coverage:
	$(GO) test -covermode=atomic -coverprofile=$(COVERAGE_FILE) ./...
	$(GO) tool cover -func=$(COVERAGE_FILE)

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

vulncheck:
	GOBIN=$(GOTOOLS_BIN) $(GO) install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	$(GOTOOLS_BIN)/govulncheck ./...

gosec:
	GOBIN=$(GOTOOLS_BIN) $(GO) install github.com/securego/gosec/v2/cmd/gosec@v2.22.10
	$(GOTOOLS_BIN)/gosec -fmt sarif -out gosec.sarif ./...

check: fmt-check vet test-short build

clean:
	rm -f $(COVERAGE_FILE) gosec.sarif
