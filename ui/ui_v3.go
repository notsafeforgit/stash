package ui

import (
	"embed"
	"io/fs"
)

//go:embed v3/build
var uiV3Box embed.FS
var UIV3Box fs.FS

func init() {
	var err error
	UIV3Box, err = fs.Sub(uiV3Box, "v3/build")
	if err != nil {
		panic(err)
	}
}

func Box(enableV3 bool) fs.FS {
	if enableV3 {
		return UIV3Box
	}
	return UIBox
}
