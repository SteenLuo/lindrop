package main

import (
	"testing"
	"unsafe"
)

// A shape regression, not a formula test: sample the actual GDI+ output where
// an 8px-radius CSS corner is transparent but the old 4px corner was filled.
func TestDesktopRoundCornerPixels(t *testing.T) {
	vectorStart()
	if vectorToken == 0 {
		t.Fatal("GDI+ unavailable")
	}
	defer func() { call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	oldScale := guiScale
	defer func() { guiScale = oldScale }()
	for _, scale := range []float64{1, 2} {
		guiScale = scale
		w, h := sc(32), sc(24)
		dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
		header := struct {
			Size                   uint32
			Width, Height          int32
			Planes, Bits           uint16
			Compression, ImageSize uint32
			X, Y                   int32
			Used, Important        uint32
		}{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, Bits: 32}
		var bits uintptr
		bitmap := call(gdi.NewProc("CreateDIBSection"), dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
		if bitmap == 0 || bits == 0 {
			t.Fatal("DIB allocation failed")
		}
		old := call(gdi.NewProc("SelectObject"), dc, bitmap)
		fill(dc, rct(0, 0, 32, 24), paper)
		if !vectorRound(dc, rct(0, 0, 32, 24), accent, accent, 8, false) {
			t.Fatal("shape draw failed")
		}
		pixels := unsafe.Slice((*byte)(unsafe.Pointer(bits)), w*h*4)
		corner := (sc(1)*w + sc(2)) * 4
		center := (sc(12)*w + sc(16)) * 4
		if pixels[corner] < 245 || pixels[corner+1] < 245 || pixels[corner+2] < 245 {
			t.Errorf("%.0fx: radius was collapsed; outer corner unexpectedly filled", scale)
		}
		if pixels[center] > 100 || pixels[center+1] > 120 || pixels[center+2] > 80 {
			t.Errorf("%.0fx: primary fill missing", scale)
		}
		call(gdi.NewProc("SelectObject"), dc, old)
		call(gdi.NewProc("DeleteObject"), bitmap)
		call(gdi.NewProc("DeleteDC"), dc)
	}
}
