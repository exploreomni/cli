.PHONY: build clean test spec

BINARY := bin/omni
VERSION ?= dev

build: spec
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/omni

spec:
	cp api/openapi.json cmd/omni/openapi.json

clean:
	rm -f $(BINARY)

test:
	go test ./...

# Update the OpenAPI spec from the monorepo
sync-spec:
	cp ~/src/omni/omni/packages/bi-app/app/types/api/openapi/openapi.json api/openapi.json
	cp api/openapi.json cmd/omni/openapi.json
