//go:build !darwin && !windows

package server

// osVersion has no supported implementation outside the two shipped desktop
// platforms; the report still carries GOOS/GOARCH.
func osVersion() string { return "" }
