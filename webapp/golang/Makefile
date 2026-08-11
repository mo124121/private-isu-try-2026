.PHONY: all build test

all: build

build: app

test:
	go test ./...

app: *.go isuutil/*.go go.mod go.sum Makefile
	go build -trimpath -o app .
