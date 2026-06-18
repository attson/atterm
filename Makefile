# Makefile — top-level orchestration of desktop build steps.

HOOK_EMBED := desktop/hookinstall/atterm-hook
HOOK_SOURCES := $(wildcard cmd/atterm-hook/*.go)

.PHONY: atterm-hook-embed dev build verify-hook-embed

$(HOOK_EMBED): $(HOOK_SOURCES)
	./scripts/build-hook-binary.sh

atterm-hook-embed: $(HOOK_EMBED)

dev: atterm-hook-embed
	cd desktop && wails dev

build: atterm-hook-embed
	cd desktop && wails build

# verify-hook-embed: rebuild the embed file and check it parses; intended
# for CI. If the on-disk embed file is stale (someone forgot to run
# atterm-hook-embed), `go test ./desktop/hookinstall/...` will fail
# the embed_test runnable check.
verify-hook-embed: atterm-hook-embed
	go test ./desktop/hookinstall/... -run TestEmbeddedBinary -v
