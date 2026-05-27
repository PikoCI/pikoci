// Package assets provides embedded static files for the PikoCI web interface.
// It uses Go's embed package to bundle CSS, JavaScript, image, and font files
// into the binary.
package assets

import (
	"embed"
)

// Assets is the embedded filesystem containing all static assets
// (CSS, JavaScript, images, and fonts) served by the HTTP handler.
//
//go:embed css/* js/* images/* fonts/*
var Assets embed.FS
