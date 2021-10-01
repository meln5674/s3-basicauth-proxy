.PHONY: all fmt vet test clean

TIMESTAMP := $(shell date +%s)

all: fmt vet bin/coverage.html test bin/proxy

clean:
	rm -rf bin/

fmt: $(wildcard **/*.go *.go)
	go fmt ./...

vet: $(wildcard **/*.go *.go)
	go vet ./...

bin/coverage.out: main.go $(wildcard **/*.go *.go)
	mkdir -p $(shell dirname $@)
	go test -coverprofile=$@ $<

bin/coverage.html: bin/coverage.out
	go tool cover -html=$< -o $@

test: main.go $(wildcard **/.go *.go)
	go test ./...
	

test: $(wildcard **/*.go *.go) bin/coverage.html

bin/proxy: main.go $(wildcard **/*.go *.go) fmt vet bin/coverage.html
	CGO_ENABLED=0 go build -a -tags='-w -extldflags "-static"' -o=$@ $<

todo:
	grep --exclude=bin/ -R TODO: .
