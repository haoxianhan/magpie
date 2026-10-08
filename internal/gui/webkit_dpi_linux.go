//go:build linux && cgo && gtk3 && !nogui

package gui

/*
#cgo pkg-config: gtk+-3.0
#include "webkit_dpi_gtk3.h"
*/
import "C"

import "errors"

// Run is called on the main thread, locked by Wails' desktop init. GTK
// must be initialized here: ApplicationStarted is after pending windows run.
func prepareNativeWebkit() error {
	if C.prepare_native_webkit() == 0 {
		return errors.New("cannot initialize GTK3: no usable display")
	}
	return nil
}
