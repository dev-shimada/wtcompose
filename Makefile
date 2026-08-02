.PHONY: test build install

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o wtcompose .

install:
	go install .
