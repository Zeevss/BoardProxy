WAILS ?= wails
APP_NAME ?= BoardProxy
BUILD_DIR := build/bin

.PHONY: build macos windows windows-installer frontend test clean

# Единый бинарь: GUI и привилегированный TUN-helper — один исполняемый файл
# (helper-режим включается флагом --helper, см. main.go / internal/helper).
# wintun.dll встроена в бинарь (internal/wintundll) и распаковывается рядом с
# exe при первом подъёме TUN — отдельного файла поставлять не нужно.
build:
	$(WAILS) build -clean -nocolour

# Run on macOS. Produces a universal .app for Intel and Apple Silicon.
macos:
	$(WAILS) build -platform darwin/universal -o $(APP_NAME) -nocolour

# Can be cross-built when a MinGW toolchain is installed.
windows:
	$(WAILS) build -platform windows/amd64 -o $(APP_NAME).exe -webview2 browser -nocolour

# NSIS must be installed. The resulting installer is placed in build/bin.
windows-installer:
	$(WAILS) build -platform windows/amd64 -o $(APP_NAME).exe -webview2 browser -nsis -nocolour

frontend:
	cd frontend && pnpm run build

test:
	go test ./...
	cd frontend && pnpm run typecheck

clean:
	rm -rf $(BUILD_DIR) frontend/dist
