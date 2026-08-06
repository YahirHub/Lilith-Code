BIN     := dist/li
BIN_EXE := dist/li.exe
PKG     := ./cmd/li
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
GO_TAGS := grammar_set_core
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# Toolchain opcional empaquetada junto al binario para acelerar búsquedas y
# conservar compatibilidad. El binario funciona sin ella; para usar el directorio
# personal (~/.li/tools/bin), basta con no definir LI_TOOLS_DIR.
DIST_TOOLS := dist/tools/bin

.PHONY: build build-tools build-cross test run install clean fmt vet tools tools-check

# `make build` compila el binario para el sistema actual dentro de dist/
# y conserva el bundle opcional de compatibilidad (ripgrep, busybox.exe, …)
# junto al ejecutable, en dist/tools/bin. `go run ./cmd/build build` genera sólo
# los binarios Go estáticos usados por los releases.
build:
	@mkdir -p dist
	CGO_ENABLED=0 go build -tags="$(GO_TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)
	@$(MAKE) build-tools

# Descarga los .exe/binarios auxiliares dentro de dist/tools/bin.
build-tools:
	@mkdir -p $(DIST_TOOLS)
	LI_TOOLS_DIR=$(DIST_TOOLS) go run ./cmd/build install

# Compilación cruzada: genera li y li.exe en dist/ además de la toolchain.
build-cross: build
	@mkdir -p dist
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags="$(GO_TAGS)" -ldflags="$(LDFLAGS)" -o $(BIN_EXE) $(PKG)

test:
	go test -tags="$(GO_TAGS)" ./...

run:
	go run -tags="$(GO_TAGS)" $(PKG)

install:
	CGO_ENABLED=0 go install -tags="$(GO_TAGS)" -ldflags="$(LDFLAGS)" $(PKG)

tools:
	go run ./cmd/build install

tools-check:
	go run ./cmd/build check

fmt:
	gofmt -w .

vet:
	go vet -tags="$(GO_TAGS)" ./...

clean:
	rm -rf bin dist
