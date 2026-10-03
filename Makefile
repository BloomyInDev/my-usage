# my-usage: build the server binary.

BIN := bin/my-usage
PKG := ./cmd/server
LDFLAGS := -s -w
COVERPROFILE := cover.out

.PHONY: build run test test-race coverage clean

## build: compile the Go binary
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

## run: build and run the binary
run: build
	./$(BIN)

## test: run the Go test suite
test:
	go test ./...

## test-race: the test suite under the race detector (needs cgo and a C compiler)
test-race:
	CGO_ENABLED=1 go test -race -count=1 ./...

## coverage: write a profile, print the total and open the annotated source
coverage:
	go test -coverpkg=./... -coverprofile=$(COVERPROFILE) ./...
	go tool cover -func=$(COVERPROFILE) | tail -1
	go tool cover -html=$(COVERPROFILE)

## clean: remove build artifacts
clean:
	rm -rf $(BIN)
