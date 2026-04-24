BIN_NAME  := ch
INSTALL   := $(HOME)/.local/bin/$(BIN_NAME)
LDFLAGS   := -ldflags "-s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"
COVERAGE_THRESHOLD := 80

.PHONY: build install test test-integration coverage lint clean deps setup check

deps:
	go mod download

build:
	go build $(LDFLAGS) -o $(BIN_NAME) .

install: build
	mv $(BIN_NAME) $(INSTALL)
	@echo "Installed to $(INSTALL)"

test:
	go test ./...

test-integration:
	go test -tags integration ./...

coverage:
	go test -coverprofile=coverage.out ./...
	@COVERAGE=$$(go tool cover -func=coverage.out | grep "^total:" | awk '{gsub(/%/,""); print $$3}'); \
	rm -f coverage.out; \
	echo "Total coverage: $${COVERAGE}%"; \
	awk -v cov="$${COVERAGE}" -v thr="$(COVERAGE_THRESHOLD)" \
	  'BEGIN { if (cov+0 < thr+0) { print "FAIL: " cov "% < " thr "%"; exit 1 } \
	           else { print "OK: " cov "% >= " thr "%" } }'

lint:
	golangci-lint run ./...

clean:
	rm -f $(BIN_NAME) coverage.out

format:
	gofmt -s -w .

check: coverage test-integration lint build
	@echo "All checks passed"

setup: deps check
	@echo "Setup complete"

