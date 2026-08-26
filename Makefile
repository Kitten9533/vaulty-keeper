BIN := bin/ai-tools

.PHONY: build test install clean

build:
	go build -o $(BIN) .

test:
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/ai-tools

clean:
	rm -rf bin
