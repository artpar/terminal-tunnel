// Package web provides the embedded web client for terminal-tunnel
package web

import (
	"embed"
)

//go:embed static/*
var StaticFS embed.FS
