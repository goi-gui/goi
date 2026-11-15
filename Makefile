# The build is cgo-free: purego loads SDL3 at runtime, and nofakecgo lets goffi
# and purego share one fake C runtime.
GO_ENV := CGO_ENABLED=0
GO_TAGS := -tags nofakecgo

build:
	$(GO_ENV) go build $(GO_TAGS) ./...

test:
	$(GO_ENV) go test $(GO_TAGS) ./...

vet:
	$(GO_ENV) go vet $(GO_TAGS) ./...

bench:
	$(GO_ENV) go test $(GO_TAGS) -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build test vet bench
