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

# Update the OpenAPI spec from an external source
sync-spec:
ifndef OMNI_OPENAPI_SPEC
	$(error OMNI_OPENAPI_SPEC is not set — point it at the path to your openapi.json)
endif
	cp $(OMNI_OPENAPI_SPEC) api/openapi.json
	cp api/openapi.json cmd/omni/openapi.json
