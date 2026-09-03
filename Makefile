BIN := bin/vaulty-keeper

.PHONY: build test install clean

build:
	go build -o $(BIN) .

test:
	go test ./...

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/$(BIN) $(HOME)/.local/bin/vaulty-keeper

clean:
	rm -rf bin
