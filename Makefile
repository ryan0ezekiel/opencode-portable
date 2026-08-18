SHELL := /usr/bin/env bash
.PHONY: build fetch package verify test vet bundle clean

PLATFORM ?= linux-x64

build:
	./scripts/build-all.sh

fetch:
	@for p in linux-x64 linux-arm64 windows-x64 windows-arm64 macos-x64 macos-arm64; do \
		./scripts/fetch-opencode.sh $$p dist/staging/$$p || exit 1; \
	done

verify:
	@for p in linux-x64 linux-arm64 windows-x64 windows-arm64 macos-x64 macos-arm64; do \
		echo "==> verifying $$p"; \
		./scripts/verify-artifacts.sh dist/staging/$$p internal/app/default.json $$p || exit 1; \
	done

package:
	@for p in linux-x64 linux-arm64 windows-x64 windows-arm64 macos-x64 macos-arm64; do \
		./scripts/package-usb.sh $$p || exit 1; \
	done

test:
	go vet ./...
	go test ./...

# Convenience: build + fetch + verify + package for one platform.
bundle: build
	./scripts/fetch-opencode.sh $(PLATFORM) dist/staging/$(PLATFORM)
	./scripts/verify-artifacts.sh dist/staging/$(PLATFORM) internal/app/default.json $(PLATFORM)
	./scripts/package-usb.sh $(PLATFORM)

clean:
	rm -rf dist