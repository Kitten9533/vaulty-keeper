BIN := bin/vaulty-keeper
VERSION := $(shell grep 'const Version' internal/cli/cli.go | sed 's/.*"\(.*\)"/\1/')
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test check-ui docs-check require-node install release clean

build:
	go build -o $(BIN) .

require-node:
	@command -v node >/dev/null 2>&1 || { echo "node is required"; exit 1; }

# check-ui runs static checks over the embedded frontend (JS syntax, DOM ids,
# variable shadowing, i18n key parity). It catches regressions that Go tests
# can't — e.g. a local variable shadowing the global i18n helper `t()`.
check-ui: require-node
	node scripts/check-ui.mjs

# docs-check runs static checks over the Markdown tree (bilingual pairing,
# language-switch links, code-fence parity, relative link targets, index
# coverage, Makefile packaging, backtick docs/scripts paths). It keeps
# the docs/ guides and root READMEs internally consistent.
docs-check: require-node
	node scripts/check-docs.mjs

test: check-ui docs-check
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/vaulty-keeper

# Current guides bundled into every archive so the packaged README's
# docs/... relative links resolve offline. Copy the docs/ tree (including
# docs/tunnel/) rather than flattening. Historical implementation records
# are archived under git tag docs-superpowers-archive and are not shipped.

# Cross-compile release binaries into release/ (one tarball/zip per platform,
# including the READMEs, LICENSE, AGENTS.md, CONTRIBUTING.md, SECURITY.md and
# the current docs/ guides), ready to attach to a GitHub release.
release:
	rm -rf release && mkdir -p release
	cp README.md README.zh-CN.md LICENSE AGENTS.md CONTRIBUTING.md CONTRIBUTING.zh-CN.md SECURITY.md SECURITY.zh-CN.md release/
	cp -R docs release/
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
			(cd release && zip -rq "$$base.zip" "vaulty-keeper$$ext" "README.md" "README.zh-CN.md" "LICENSE" "AGENTS.md" "CONTRIBUTING.md" "CONTRIBUTING.zh-CN.md" "SECURITY.md" "SECURITY.zh-CN.md" "docs"); \
		else \
			tar -C release -czf "release/$$base.tar.gz" "vaulty-keeper" "README.md" "README.zh-CN.md" "LICENSE" "AGENTS.md" "CONTRIBUTING.md" "CONTRIBUTING.zh-CN.md" "SECURITY.md" "SECURITY.zh-CN.md" "docs"; \
		fi; \
		rm -f "release/vaulty-keeper$$ext"; \
	done
	rm -f release/README.md release/README.zh-CN.md release/LICENSE release/AGENTS.md release/CONTRIBUTING.md release/CONTRIBUTING.zh-CN.md release/SECURITY.md release/SECURITY.zh-CN.md
	rm -rf release/docs
	shasum -a 256 release/*.tar.gz release/*.zip > release/sha256sums.txt
	@echo ">> done:"
	@ls -lh release/

clean:
	rm -rf bin release
