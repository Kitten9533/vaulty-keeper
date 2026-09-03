BIN := bin/vaulty-keeper
VERSION := $(shell grep 'const Version' internal/cli/cli.go | sed 's/.*"\(.*\)"/\1/')
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test install release clean

build:
	go build -o $(BIN) .

test:
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/vaulty-keeper

# Cross-compile release binaries into release/ (one tarball/zip per platform,
# including the README and LICENSE), ready to attach to a GitHub release.
release:
	rm -rf release && mkdir -p release
	cp README.md LICENSE release/
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		echo ">> building vaulty-keeper-$(VERSION)-$$os-$$arch..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "release/vaulty-keeper$$ext" .; \
		if [ "$$os" = "windows" ]; then \
			zip -jq "release/vaulty-keeper-$(VERSION)-$$os-$$arch.zip" "release/vaulty-keeper$$ext" "release/README.md" "release/LICENSE"; \
		else \
			tar -C release -czf "release/vaulty-keeper-$(VERSION)-$$os-$$arch.tar.gz" "vaulty-keeper" "README.md" "LICENSE"; \
		fi; \
		rm -f "release/vaulty-keeper$$ext"; \
	done
	rm -f release/README.md release/LICENSE
	@echo ">> done:"
	@ls -lh release/

clean:
	rm -rf bin release
