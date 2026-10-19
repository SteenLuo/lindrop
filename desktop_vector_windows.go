package main

import (
	"math"
	"syscall"
	"unsafe"
)

// GDI+ ships with Windows 7. Use its antialiased vector primitives while the
// controls, text, focus, file dialogs and window frame remain native Win32.
var vector = syscall.NewLazyDLL("gdiplus.dll")
var vectorToken uintptr

func vectorStart() {
	input := struct {
		Version                        uint32
		Callback                       uintptr
		SuppressThread, SuppressCodecs int32
	}{Version: 1}
	if call(vector.NewProc("GdiplusStartup"), uintptr(unsafe.Pointer(&vectorToken)), uintptr(unsafe.Pointer(&input)), 0) != 0 {
		vectorToken = 0
	}
}
func real(v float32) uintptr { return uintptr(math.Float32bits(v)) }
func argb(c uintptr) uintptr { return 0xff000000 | (c&255)<<16 | (c & 0xff00) | (c >> 16 & 255) }
func graphics(dc uintptr) uintptr {
	if vectorToken == 0 {
		return 0
	}
	var g uintptr
	if call(vector.NewProc("GdipCreateFromHDC"), dc, uintptr(unsafe.Pointer(&g))) != 0 {
		return 0
	}
	call(vector.NewProc("GdipSetSmoothingMode"), g, 4)
	return g
}
func vectorRound(dc uintptr, r rect, c, border uintptr, radius int, outline bool) bool {
	return vectorRoundStyle(dc, r, c, border, radius, outline, false)
}
func vectorRoundStyle(dc uintptr, r rect, c, border uintptr, radius int, outline, dashed bool) bool {
	g := graphics(dc)
	if g == 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), g)
	x, y := float32(r.L)+.5, float32(r.T)+.5
	w, h := float32(r.R-r.L)-1, float32(r.B-r.T)-1
	// GDI+ arcs take a diameter; CSS/design tokens specify a radius.
	d := float32(sc(radius * 2))
	if d > w {
		d = w
	}
	if d > h {
		d = h
	}
	if d < 1 {
		d = 1
	}
	var path, brush, pen uintptr
	call(vector.NewProc("GdipCreatePath"), 0, uintptr(unsafe.Pointer(&path)))
	defer call(vector.NewProc("GdipDeletePath"), path)
	for _, a := range [][3]float32{{x, y, 180}, {x + w - d, y, 270}, {x + w - d, y + h - d, 0}, {x, y + h - d, 90}} {
		call(vector.NewProc("GdipAddPathArc"), path, real(a[0]), real(a[1]), real(d), real(d), real(a[2]), real(90))
	}
	call(vector.NewProc("GdipClosePathFigure"), path)
	if !outline {
		call(vector.NewProc("GdipCreateSolidFill"), argb(c), uintptr(unsafe.Pointer(&brush)))
		call(vector.NewProc("GdipFillPath"), g, brush, path)
		call(vector.NewProc("GdipDeleteBrush"), brush)
	}
	call(vector.NewProc("GdipCreatePen1"), argb(border), real(1), 2, uintptr(unsafe.Pointer(&pen)))
	if dashed {
		pattern := [2]float32{3, 3}
		call(vector.NewProc("GdipSetPenDashArray"), pen, uintptr(unsafe.Pointer(&pattern[0])), 2)
	}
	call(vector.NewProc("GdipDrawPath"), g, pen, path)
	call(vector.NewProc("GdipDeletePen"), pen)
	return true
}
func vectorLine(dc uintptr, x1, y1, x2, y2 int, c uintptr, width int) bool {
	g := graphics(dc)
	if g == 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), g)
	var pen uintptr
	call(vector.NewProc("GdipCreatePen1"), argb(c), real(float32(guiScale)*float32(width)), 2, uintptr(unsafe.Pointer(&pen)))
	call(vector.NewProc("GdipSetPenStartCap"), pen, 2)
	call(vector.NewProc("GdipSetPenEndCap"), pen, 2)
	defer call(vector.NewProc("GdipDeletePen"), pen)
	call(vector.NewProc("GdipDrawLine"), g, pen, real(float32(sc(x1))+.5), real(float32(sc(y1))+.5), real(float32(sc(x2))+.5), real(float32(sc(y2))+.5))
	return true
}
func focusOutline(dc uintptr, r rect) {
	if !vectorRound(dc, r, 0, accent, 8, true) {
		call(user.NewProc("DrawFocusRect"), dc, uintptr(unsafe.Pointer(&r)))
	}
}
