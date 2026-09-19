# Minimal operator targets. Windows without make: run the scripts/*.ps1 directly.
BIN := bin/devsys
ifeq ($(OS),Windows_NT)
BIN := bin/devsys.exe
endif

.PHONY: build install smoke release clean

build:
	go build -o $(BIN) ./cmd/devsys

install: build
	mkdir -p ~/.local/bin
	cp $(BIN) ~/.local/bin/devsys

smoke:
	bash scripts/exchange-demo.sh

release:
	@echo "usage: make release VERSION=<tag> [OUT=dist]"; test -n "$(VERSION)"
	bash scripts/build-release.sh --version "$(VERSION)" --out "$${OUT:-dist}"

clean:
	rm -rf bin/devsys bin/devsys.exe dist
