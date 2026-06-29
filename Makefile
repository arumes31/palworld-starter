.PHONY: build test lint security deps clean

build:
	go build -o palworld-starter .

test:
	go test -v ./...

lint:
	golangci-lint run ./...

security:
	gosec ./...
	govulncheck ./...

deps:
	go mod tidy
	git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

clean:
	rm -f palworld-starter