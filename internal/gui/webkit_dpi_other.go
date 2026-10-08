//go:build (!linux || !cgo || !gtk3) && !nogui

package gui

func prepareNativeWebkit() error { return nil }
