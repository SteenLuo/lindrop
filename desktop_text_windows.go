package main

import (
	"fmt"
	"unsafe"
)

var vectorFonts = map[uintptr]uintptr{}

type textRasterKey struct {
	Text, Face           string
	Font, Color          uintptr
	Width, Height, Align int
	Scale                float64
}
type textRaster struct {
	Bitmap uintptr
	Bytes  int
	Used   uint64
}

var textRasters = map[textRasterKey]textRaster{}
var textRasterBytes int
var textRasterClock, textRasterBuilds uint64
var textRasterLimit = 24 * 1024 * 1024

func clearTextRasters() {
	for k, v := range textRasters {
		call(vector.NewProc("GdipDisposeImage"), v.Bitmap)
		delete(textRasters, k)
	}
	textRasterBytes = 0
}
func rememberTextRaster(k textRasterKey, bitmap uintptr, size int) bool {
	if size > textRasterLimit {
		return false
	}
	for textRasterBytes+size > textRasterLimit && len(textRasters) > 0 {
		var oldest textRasterKey
		used := ^uint64(0)
		for key, v := range textRasters {
			if v.Used < used {
				oldest, used = key, v.Used
			}
		}
		v := textRasters[oldest]
		call(vector.NewProc("GdipDisposeImage"), v.Bitmap)
		textRasterBytes -= v.Bytes
		delete(textRasters, oldest)
	}
	textRasterClock++
	textRasters[k] = textRaster{bitmap, size, textRasterClock}
	textRasterBytes += size
	return true
}
func drawTextRaster(dc, bitmap uintptr, r rect) bool {
	out := graphics(dc)
	if out == 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), out)
	call(vector.NewProc("GdipSetInterpolationMode"), out, 7)
	return call(vector.NewProc("GdipDrawImageRect"), out, bitmap, real(float32(r.L)), real(float32(r.T)), real(float32(r.R-r.L)), real(float32(r.B-r.T))) == 0
}

// Render all app-owned text with the same antialiased pipeline as the shapes.
// GDI ClearType on a compatible bitmap left pale/fringed small text; grayscale
// antialiasing also keeps it consistent on different RGB/BGR monitor layouts.
func vectorText(dc uintptr, s string, r rect, c, font uintptr, align int) bool {
	info, ok := desktopFontInfo[font]
	if !ok {
		return false
	}
	width, height := int(r.R-r.L), int(r.B-r.T)
	if width <= 0 || height <= 0 {
		return false
	}
	key := textRasterKey{s, guiFontFamily, font, c, width, height, align, guiScale}
	if cached, ok := textRasters[key]; ok {
		textRasterClock++
		cached.Used = textRasterClock
		textRasters[key] = cached
		return drawTextRaster(dc, cached.Bitmap, r)
	}
	f := vectorFonts[font]
	if f == 0 {
		var family uintptr
		if call(vector.NewProc("GdipCreateFontFamilyFromName"), uintptr(unsafe.Pointer(wp(guiFontFamily))), 0, uintptr(unsafe.Pointer(&family))) != 0 {
			return false
		}
		style := uintptr(0)
		if info[1] >= 600 {
			style = 1
		}
		code := call(vector.NewProc("GdipCreateFont"), family, real(float32(info[0])*float32(guiScale)*2), style, 2, uintptr(unsafe.Pointer(&f)))
		call(vector.NewProc("GdipDeleteFontFamily"), family)
		if code != 0 {
			return false
		}
		vectorFonts[font] = f
	}
	// Match the reference browser's 2x rasterization before its 1x screenshot:
	// draw glyphs to a transparent 2x surface and resolve them at display size.
	var bitmap, g uintptr
	if call(vector.NewProc("GdipCreateBitmapFromScan0"), uintptr(width*2), uintptr(height*2), 0, 0x26200a, 0, uintptr(unsafe.Pointer(&bitmap))) != 0 {
		return false
	}
	kept := false
	defer func() {
		if !kept {
			call(vector.NewProc("GdipDisposeImage"), bitmap)
		}
	}()
	if call(vector.NewProc("GdipGetImageGraphicsContext"), bitmap, uintptr(unsafe.Pointer(&g))) != 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), g)
	call(vector.NewProc("GdipGraphicsClear"), g, 0)
	call(vector.NewProc("GdipSetTextRenderingHint"), g, 4)
	var brush, format uintptr
	call(vector.NewProc("GdipCreateSolidFill"), argb(c), uintptr(unsafe.Pointer(&brush)))
	defer call(vector.NewProc("GdipDeleteBrush"), brush)
	call(vector.NewProc("GdipStringFormatGetGenericTypographic"), uintptr(unsafe.Pointer(&format)))
	defer call(vector.NewProc("GdipDeleteStringFormat"), format)
	call(vector.NewProc("GdipSetStringFormatFlags"), format, 0x1000|0x4000|4)
	call(vector.NewProc("GdipSetStringFormatAlign"), format, uintptr(align))
	call(vector.NewProc("GdipSetStringFormatLineAlign"), format, 1)
	call(vector.NewProc("GdipSetStringFormatTrimming"), format, 3)
	box := [4]float32{0, 0, float32(width * 2), float32(height * 2)}
	if call(vector.NewProc("GdipDrawString"), g, uintptr(unsafe.Pointer(wp(s))), ^uintptr(0), f, uintptr(unsafe.Pointer(&box[0])), format, brush) != 0 {
		return false
	}
	call(vector.NewProc("GdipFlush"), g, 1)
	textRasterBuilds++
	kept = rememberTextRaster(key, bitmap, width*height*16)
	return drawTextRaster(dc, bitmap, r)
}
func releaseVectorFonts() {
	clearTextRasters()
	for key, f := range vectorFonts {
		call(vector.NewProc("GdipDeleteFont"), f)
		delete(vectorFonts, key)
	}
}
func progressSize(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
	}
	return formatSize(n)
}
func metaSize(n int64) string {
	if n >= 1024*1024 && n%(1024*1024) == 0 {
		return fmt.Sprintf("%d MB", n/(1024*1024))
	}
	return formatSize(n)
}
