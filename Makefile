BIN_NAME        := ch
INSTALL         := $(HOME)/.local/bin/$(BIN_NAME)
LDFLAGS         := -ldflags "-s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"
DIST_DIR        := dist
VERSION         := $(shell cat VERSION)
RELEASE_LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
COVERAGE_THRESHOLD := 80

.PHONY: build install test test-integration coverage lint clean deps setup check vendor tools release-build release-archive release-checksums

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
	go tool golangci-lint run ./...

vendor:
	go mod vendor

tools:
	go install tool

clean:
	rm -rf $(BIN_NAME) coverage.out $(DIST_DIR)

format:
	gofmt -s -w .

release-build:
	mkdir -p $(DIST_DIR)/$(GOOS)-$(GOARCH)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -trimpath $(RELEASE_LDFLAGS) \
	  -o $(DIST_DIR)/$(GOOS)-$(GOARCH)/$(BIN_NAME) .

release-archive:
	cp LICENSE README.md $(DIST_DIR)/$(GOOS)-$(GOARCH)/
	tar -C $(DIST_DIR)/$(GOOS)-$(GOARCH) -czf \
	  $(DIST_DIR)/$(BIN_NAME)-$(VERSION)-$(GOOS)-$(GOARCH).tar.gz \
	  $(BIN_NAME) LICENSE README.md

release-checksums:
	cd $(DIST_DIR) && sha256sum *.tar.gz > checksums.txt

check: coverage test-integration lint build
	@echo "All checks passed"

setup: deps vendor tools check
	@echo "Setup complete"

