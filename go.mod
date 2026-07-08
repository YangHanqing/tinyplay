module tvremote

go 1.22

// Run `go mod tidy` after the first checkout to populate go.sum and pin
// exact versions. Go is not installed in the scaffold environment, so these
// versions are best-effort and may be adjusted by `tidy`.
// fyne.io/systray, go-winio and go-webview2 are only imported on Windows
// (build-tagged), so a macOS `go build` never compiles them.
require (
	fyne.io/systray v1.11.0
	github.com/Microsoft/go-winio v0.6.2
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
)

require (
	github.com/hirochachacha/go-smb2 v1.1.0
	github.com/itchyny/volume-go v0.2.2
)

require (
	github.com/geoffgarside/ber v1.1.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/moutend/go-wca v0.2.0 // indirect
	golang.org/x/crypto v0.0.0-20200728195943-123391ffb6de // indirect
	golang.org/x/sys v0.15.0 // indirect
)
