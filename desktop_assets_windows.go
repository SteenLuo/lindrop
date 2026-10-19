package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// The icons are the actual vector symbols in the approved A reference,
// rasterized at high density with transparent edges, not approximated in GDI.
//
//go:embed assets/desktop/*.png
var desktopAssets embed.FS
var desktopImages = map[string]uintptr{}
var desktopFonts = map[[2]int]uintptr{}
var desktopFontInfo = map[uintptr][2]int{}

func loadDesktopAssets(dir string) error {
	folder := filepath.Join(dir, ".ui-assets")
	if e := os.MkdirAll(folder, 0700); e != nil {
		return e
	}
	entries, e := desktopAssets.ReadDir("assets/desktop")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".png") {
			continue
		}
		raw, e := desktopAssets.ReadFile("assets/desktop/" + name)
		if e != nil {
			return e
		}
		file := filepath.Join(folder, name)
		if e = os.WriteFile(file, raw, 0600); e != nil {
			return e
		}
		var img uintptr
		if code := call(vector.NewProc("GdipLoadImageFromFile"), uintptr(unsafe.Pointer(wp(file))), uintptr(unsafe.Pointer(&img))); code != 0 {
			return fmt.Errorf("图标加载失败：%s (%d)", name, code)
		}
		desktopImages[strings.TrimSuffix(name, ".png")] = img
	}
	return nil
}
func releaseDesktopAssets() {
	for key, img := range desktopImages {
		call(vector.NewProc("GdipDisposeImage"), img)
		delete(desktopImages, key)
	}
}
func sourceIcon(dc uintptr, kind string, x, y, size int, c uintptr) bool {
	variant := "accent"
	switch c {
	case muted:
		variant = "muted"
	case paper:
		variant = "paper"
	case ink:
		variant = "ink"
	}
	img := desktopImages[kind+"-"+variant]
	if img == 0 {
		return false
	}
	g := graphics(dc)
	if g == 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), g)
	call(vector.NewProc("GdipSetInterpolationMode"), g, 7)
	call(vector.NewProc("GdipDrawImageRect"), g, img, real(float32(sc(x))), real(float32(sc(y))), real(float32(sc(size))), real(float32(sc(size))))
	return true
}
func fontSize(size, weight int) uintptr {
	key := [2]int{size, weight}
	if v := desktopFonts[key]; v != 0 {
		return v
	}
	v := newFont(size, weight)
	desktopFonts[key] = v
	desktopFontInfo[v] = key
	return v
}
func dot(dc uintptr, x, y, size int, c uintptr) { rounded(dc, rct(x, y, size, size), c, c, size) }
