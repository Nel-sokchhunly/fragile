//go:build !(linux && cgo && (production || dev))

package main

// ClipboardImage is the Linux paste fallback (see clipboard.go); elsewhere the
// paste event carries clipboard images itself, and plain go build/test (no
// production or dev tag) has no GTK to read from.
func (a *App) ClipboardImage() string { return "" }
