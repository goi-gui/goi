build:
	go build -ldflags=-linkmode=internal ./...

spike:
	go run -ldflags=-linkmode=internal ./examples/spike

search:
	go run -ldflags=-linkmode=internal ./examples/search

counter:
	go run -ldflags=-linkmode=internal ./examples/counter

gallery:
	go run -ldflags=-linkmode=internal ./examples/gallery

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

datalist:
	go run -ldflags=-linkmode=internal ./examples/datalist

bridge:
	go run -ldflags=-linkmode=internal ./examples/bridge

format:
	go run -ldflags=-linkmode=internal ./examples/format

test:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build search spike counter gallery hello scroll typography composition theme titlebar datalist bridge format test vet bench
