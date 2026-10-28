build:
	go build -ldflags=-linkmode=internal ./...

spike:
	go run -ldflags=-linkmode=internal ./examples/spike

counter:
	go run -ldflags=-linkmode=internal ./examples/counter

hello:
	go run -ldflags=-linkmode=internal ./examples/hello

test:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build spike counter hello test vet bench
