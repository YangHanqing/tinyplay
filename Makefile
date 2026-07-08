# Go desktop build. The phone frontend lives in ./web and is embedded directly
# by the Go module; no generated frontend copy is needed.

VERSION ?= 0.0.0-dev

.PHONY: sync tidy run build-core-mac build-core-win build-app-mac clean

## sync: compatibility no-op kept for older docs/scripts
sync:
	@true

tidy:
	go mod tidy

## run: run the headless core locally (Linux/macOS dev)
run:
	go run ./cmd/tvremote

## build-core-mac: the macOS arm64 core binary (the SwiftUI app bundles this)
build-core-mac:
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o build/tvremote-core-darwin-arm64 ./cmd/tvremote

## build-core-win: the Windows exe (tray + WebView2 shell, no console window)
build-core-win:
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.version=$(VERSION)" -o build/TinyPlay.exe ./cmd/tvremote

## build-app-mac: assemble the native .app around the core (mpv via PATH unless
## MPV_DIR is set). CI sets MPV_DIR to a self-contained mpv; locally you can omit
## it and the app falls back to `mpv` on PATH.
build-app-mac: build-core-mac
	./macos/build-app.sh

clean:
	rm -rf build dist mpvstage mpv_extract
