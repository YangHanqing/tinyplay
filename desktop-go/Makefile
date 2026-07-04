# Go desktop build. The phone frontend lives in the repo-root web/ directory and
# is synced into the module before building (go:embed can't reach outside the
# module).

WEB_SRC := ../web
DIST    := internal/webui/dist

.PHONY: sync tidy run build-core-mac build-core-win build-app-mac clean

## sync: copy the shared frontend into the embed directory
sync:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	cp $(WEB_SRC)/index.html $(WEB_SRC)/app.js $(WEB_SRC)/styles.css $(WEB_SRC)/i18n.js $(WEB_SRC)/manifest.webmanifest $(WEB_SRC)/sw.js $(WEB_SRC)/pwa-icon.svg $(WEB_SRC)/favicon.png $(DIST)/
	touch $(DIST)/.gitkeep

tidy:
	go mod tidy

## run: sync + run the headless core locally (Linux/macOS dev)
run: sync
	go run ./cmd/tvremote

## build-core-mac: the macOS arm64 core binary (the SwiftUI app bundles this)
build-core-mac: sync
	GOOS=darwin GOARCH=arm64 go build -o build/tvremote-core-darwin-arm64 ./cmd/tvremote

## build-core-win: the Windows exe (tray + WebView2 shell, no console window)
build-core-win: sync
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o build/TinyPlay.exe ./cmd/tvremote

## build-app-mac: assemble the native .app around the core (mpv via PATH unless
## MPV_DIR is set). CI sets MPV_DIR to a self-contained mpv; locally you can omit
## it and the app falls back to `mpv` on PATH.
build-app-mac: build-core-mac
	./macos/build-app.sh

clean:
	rm -rf build dist mpvstage mpv_extract $(DIST)
	mkdir -p $(DIST) && touch $(DIST)/.gitkeep
