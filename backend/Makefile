.PHONY: build-api build-engine run-api run-engine test vet clean

# Build
build-api:
	go build -o bin/api ./cmd/api

build-engine:
	go build -o bin/engine ./cmd/engine

build: build-api build-engine

# Run locally
run-api:
	go run ./cmd/api

run-engine:
	go run ./cmd/engine

# Quality
test:
	go test ./...

vet:
	go vet ./...

# Clean
clean:
	rm -rf bin/
