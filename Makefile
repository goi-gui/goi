build:
	go build -ldflags=-linkmode=internal ./...

# The examples are their own module in ../examples, next to this checkout. They
# load their .goi files by relative path, so they run from that directory.
EXAMPLES := ../examples
RUN := go -C $(EXAMPLES) run -ldflags=-linkmode=internal

spike:
	$(RUN) ./spike

search:
	$(RUN) ./search

counter:
	$(RUN) ./counter

gallery:
	$(RUN) ./gallery

hello:
	$(RUN) ./hello

scroll:
	$(RUN) ./scroll

typography:
	$(RUN) ./typography

composition:
	$(RUN) ./composition

theme:
	$(RUN) ./theme

titlebar:
	$(RUN) ./titlebar

datalist:
	$(RUN) ./datalist

bridge:
	$(RUN) ./bridge

svg:
	$(RUN) ./svg

window:
	$(RUN) ./window

format:
	$(RUN) ./format

fmt:
	go run ./cmd/goifmt -w .
	go run ./cmd/goifmt -w $(EXAMPLES)

fmt-list:
	go run ./cmd/goifmt -l .
	go run ./cmd/goifmt -l $(EXAMPLES)

test:
	go test -race -ldflags=-linkmode=internal ./...

vet:
	go vet ./...

bench:
	go test -run XXX -bench . -benchmem ./internal/layout/ ./internal/syntax/ ./internal/paint/

.PHONY: build search spike counter gallery hello scroll typography composition theme titlebar datalist bridge svg window format fmt fmt-list test vet bench
