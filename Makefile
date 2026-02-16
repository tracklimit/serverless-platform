.PHONY: build run test clean

BINARY=serverless-platform
CMD=./cmd/api

build:
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD)

test:
	go test ./...

clean:
	rm -rf bin/
