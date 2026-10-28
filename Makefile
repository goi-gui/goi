build:
	go build -ldflags=-linkmode=internal ./...

spike:
	go run -ldflags=-linkmode=internal ./examples/spike

test:
	go test -race ./...

vet:
	go vet ./...

.PHONY: build spike test vet
