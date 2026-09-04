BIN := bin/vaulty-keeper
VERSION := $(shell grep 'const Version' internal/cli/cli.go | sed 's/.*"\(.*\)"/\1/')
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test check-ui install release clean

build:
	go build -o $(BIN) .

# check-ui runs static checks over the embedded frontend (JS syntax, DOM ids,
# variable shadowing, i18n key parity). It catches regressions that Go tests
# can't — e.g. a local variable shadowing the global i18n helper `t()`.
check-ui:
	@command -v node >/dev/null 2>&1 || { echo "check-ui: node is required"; exit 1; }
	node scripts/check-ui.mjs

test: check-ui
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/vaulty-keeper

# Cross-compile release binaries into release/ (one tarball/zip per platform,
# including the README and LICENSE), ready to attach to a GitHub release.
release:
	rm -rf release && mkdir -p release
	cp README.md README.zh-CN.md LICENSE release/
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		case "$$os" in darwin) nameos="macos";; *) nameos="$$os";; esac; \
		case "$$arch" in amd64) namearch="x86_64";; *) namearch="$$arch";; esac; \
		base="vaulty-keeper-$(VERSION)-$$nameos-$$namearch"; \
		echo ">> building $$base..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "release/vaulty-keeper$$ext" .; \
		if [ "$$os" = "windows" ]; then \
			zip -jq "release/$$base.zip" "release/vaulty-keeper$$ext" "release/README.md" "release/README.zh-CN.md" "release/LICENSE"; \
		else \
			tar -C release -czf "release/$$base.tar.gz" "vaulty-keeper" "README.md" "README.zh-CN.md" "LICENSE"; \
		fi; \
		rm -f "release/vaulty-keeper$$ext"; \
	done
	rm -f release/README.md release/README.zh-CN.md release/LICENSE
	@echo ">> done:"
	@ls -lh release/

clean:
	rm -rf bin release
