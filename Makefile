# Minimal operator targets. Windows without make: run the scripts/*.ps1 directly.
BIN := bin/workloom
ifeq ($(OS),Windows_NT)
BIN := bin/workloom.exe
endif
ALIAS_BIN := $(subst workloom,devsys,$(BIN))

.PHONY: build install smoke release clean

build: $(BIN) $(ALIAS_BIN)

$(BIN):
	go build -o $@ ./cmd/workloom

$(ALIAS_BIN):
	go build -o $@ ./cmd/devsys

install: build
	mkdir -p ~/.local/bin
	cp $(BIN) ~/.local/bin/workloom
	cp $(ALIAS_BIN) ~/.local/bin/devsys

smoke:
	bash scripts/exchange-demo.sh

release:
	@echo "usage: make release VERSION=<tag> [OUT=dist]"; test -n "$(VERSION)"
	bash scripts/build-release.sh --version "$(VERSION)" --out "$${OUT:-dist}"

clean:
	rm -rf bin/workloom bin/workloom.exe bin/devsys bin/devsys.exe dist
