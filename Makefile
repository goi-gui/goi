build:
	go build -ldflags=-linkmode=internal ./...

spike:
	go run -ldflags=-linkmode=internal ./examples/spike

counter:
	go run -ldflags=-linkmode=internal ./examples/counter

gallery:
	go run -ldflags=-linkmode=internal ./examples/gallery

hello:
	go run -ldflags=-linkmode=internal ./examples/hello

test:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build spike counter gallery hello test vet bench
