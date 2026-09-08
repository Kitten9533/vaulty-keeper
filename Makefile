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
# language-switch links, code-fence parity, relative link targets). It keeps
# the docs/ guides and root READMEs internally consistent.
docs-check: require-node
	node scripts/check-docs.mjs

test: check-ui docs-check
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/vaulty-keeper

# Current guides bundled into every archive so the packaged README's docs/*.md
# relative links resolve offline. Historical implementation records are archived
# under git tag docs-superpowers-archive and are not shipped, by design.
DOCS := docs/README.md docs/README.zh-CN.md \
        docs/security-model.md docs/security-model.zh-CN.md \
        docs/apollo-snapshot-guide.md docs/apollo-snapshot-guide.zh-CN.md \
        docs/ui-guide.md docs/ui-guide.zh-CN.md \
        docs/db-proxy-architecture.md docs/db-proxy-architecture.zh-CN.md \
        docs/db-proxy-examples.md docs/db-proxy-examples.zh-CN.md \
        docs/mongodb-tunnel-guide.md docs/mongodb-tunnel-guide.zh-CN.md

# Cross-compile release binaries into release/ (one tarball/zip per platform,
# including the READMEs, LICENSE, AGENTS.md, CONTRIBUTING.md, SECURITY.md and
# the current docs/ guides), ready to attach to a GitHub release.
release:
	rm -rf release && mkdir -p release/docs
	cp README.md README.zh-CN.md LICENSE AGENTS.md CONTRIBUTING.md CONTRIBUTING.zh-CN.md SECURITY.md SECURITY.zh-CN.md release/
	cp $(DOCS) release/docs/
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
