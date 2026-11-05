build:
	go build -ldflags=-linkmode=internal ./...

test:
	go test -race -ldflags=-linkmode=internal ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build test vet bench
