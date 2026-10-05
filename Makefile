IMAGE ?= stormkeep:latest
WEEWX_IMAGE ?= stormkeep-weewx:latest
# Release tags are v-prefixed. The match glob keeps any other tag from becoming
# the version stamped into the binary and every static asset URL.
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the dashboard and the simulator
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/stormkeep ./cmd/stormkeep
	go build -o bin/tempest-sim ./cmd/tempest-sim

.PHONY: run
run: build ## Run the dashboard on :8080, fed by the simulator (WX_DB=path for real history)
	bin/tempest-sim -to 127.0.0.1:50222 & trap 'kill $$!' EXIT; \
	WX_LISTEN=127.0.0.1:8080 WX_DB=$${WX_DB:-./tmp/weewx.sdb} bin/stormkeep

.PHONY: test
test: ## Run the tests with the race detector
	go test -race ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: fmt
fmt: ## gofmt the tree
	gofmt -w $$(git ls-files '*.go')

.PHONY: check
check: fmt vet test ## Format, vet and test
	node --check internal/web/static/app.js

.PHONY: image
image: ## Build both container images
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .
	docker build -t $(WEEWX_IMAGE) weewx

.PHONY: clean
clean:
	rm -rf bin coverage.out tmp
