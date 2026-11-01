build:
	go build -ldflags=-linkmode=internal ./...

spike:
	go run -ldflags=-linkmode=internal ./examples/spike

counter:
	go run -ldflags=-linkmode=internal ./examples/counter

gallery:
	go run -ldflags=-linkmode=internal ./examples/gallery -fps

hello:
	go run -ldflags=-linkmode=internal ./examples/hello

scroll:
	go run -ldflags=-linkmode=internal ./examples/scroll

typography:
	go run -ldflags=-linkmode=internal ./examples/typography

composition:
	go run -ldflags=-linkmode=internal ./examples/composition

theme:
	go run -ldflags=-linkmode=internal ./examples/theme

titlebar:
	go run -ldflags=-linkmode=internal ./examples/titlebar

test:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build spike counter gallery hello scroll typography composition theme titlebar test vet bench
