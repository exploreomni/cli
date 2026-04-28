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

# Update the OpenAPI spec.
#
# By default, fetches the spec from exploreomni/omni@main via `gh api`
# (requires `gh auth` with read access to the monorepo).
#
# To sync from a local checkout instead — useful when testing an unmerged
# spec change — set OMNI_OPENAPI_SPEC to a local path:
#   OMNI_OPENAPI_SPEC=/path/to/openapi.json make sync-spec
#
# To pin to a non-default branch or commit:
#   OMNI_OPENAPI_REF=my-branch make sync-spec
OMNI_OPENAPI_REPO ?= exploreomni/omni
OMNI_OPENAPI_PATH ?= packages/bi-app/app/types/api/openapi/openapi.json
OMNI_OPENAPI_REF  ?= main

sync-spec:
ifdef OMNI_OPENAPI_SPEC
	@echo "Syncing spec from local file: $(OMNI_OPENAPI_SPEC)"
	cp "$(OMNI_OPENAPI_SPEC)" api/openapi.json
else
	@command -v gh >/dev/null 2>&1 || { echo "gh CLI is required (https://cli.github.com). Or set OMNI_OPENAPI_SPEC to a local path." >&2; exit 1; }
	@echo "Fetching spec from $(OMNI_OPENAPI_REPO)@$(OMNI_OPENAPI_REF)"
	gh api "repos/$(OMNI_OPENAPI_REPO)/contents/$(OMNI_OPENAPI_PATH)?ref=$(OMNI_OPENAPI_REF)" -H "Accept: application/vnd.github.raw" > api/openapi.json
endif
	cp api/openapi.json cmd/omni/openapi.json
