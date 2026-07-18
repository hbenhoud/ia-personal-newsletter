# Dynamic product build helpers. The static CLI (cmd/newsletter) is unaffected.

TAILWIND := ./bin/tailwindcss
TW_VERSION := latest

# Detect the Tailwind standalone binary for this platform.
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),Darwin)
  ifeq ($(UNAME_M),arm64)
    TW_FILE := tailwindcss-macos-arm64
  else
    TW_FILE := tailwindcss-macos-x64
  endif
else
  ifeq ($(UNAME_M),aarch64)
    TW_FILE := tailwindcss-linux-arm64
  else
    TW_FILE := tailwindcss-linux-x64
  endif
endif

.PHONY: tailwind css css-watch build test

# Download the Tailwind standalone CLI (no Node required).
tailwind:
	mkdir -p bin
	curl -sSL -o $(TAILWIND) "https://github.com/tailwindlabs/tailwindcss/releases/$(TW_VERSION)/download/$(TW_FILE)"
	chmod +x $(TAILWIND)

# Compile the site stylesheet into templates/web/app.css (committed + embedded).
css:
	$(TAILWIND) -i web/tailwind/input.css -o templates/web/app.css --minify

css-watch:
	$(TAILWIND) -i web/tailwind/input.css -o templates/web/app.css --watch

build: css
	go build ./...

test:
	go test ./...
