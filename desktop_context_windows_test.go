package main

import (
	"image"
	imagecolor "image/color"
	"image/png"
	"os"
	"runtime"
	"testing"
	"unsafe"
)

func TestThemedContextMenuPaletteAndPlacement(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	oldItems, oldHover, oldScale, oldFont, oldTiny := guiContextItems, guiContextHover, guiScale, guiFont, guiTiny
	defer func() {
		guiContextItems, guiContextHover, guiScale, guiFont, guiTiny = oldItems, oldHover, oldScale, oldFont, oldTiny
	}()
	guiContextItems = []contextMenuItem{{172, "复制文件", "Ctrl+C"}, {176, "打开文件", ""}, {153, "打开所在文件夹", ""}}
	guiContextHover = 2
	vectorStart()
	if vectorToken == 0 {
		t.Fatal("GDI+ unavailable")
	}
	defer func() { releaseVectorFonts(); call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	for _, scale := range []float64{1, 2} {
		releaseVectorFonts()
		guiScale = scale
		guiFont, guiTiny = fontSize(13, 400), fontSize(9, 400)
		w, h := sc(contextMenuWidth), sc(2*contextMenuPadding+contextMenuRow*len(guiContextItems))
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
			t.Fatal("menu bitmap allocation")
		}
		old := call(gdi.NewProc("SelectObject"), dc, bitmap)
		paintContextMenu(dc, rect{0, 0, int32(w), int32(h)})
		background := call(gdi.NewProc("GetPixel"), dc, uintptr(sc(12)), uintptr(sc(60)))
		hover := call(gdi.NewProc("GetPixel"), dc, uintptr(sc(12)), uintptr(sc(96)))
		if background != paper || hover != tint {
			t.Fatal("context menu did not use app palette", scale, background, hover)
		}
		if path := os.Getenv("LINDROP_MENU_PREVIEW"); path != "" && scale == 2 {
			data := unsafe.Slice((*byte)(unsafe.Pointer(bits)), w*h*4)
			im := image.NewNRGBA(image.Rect(0, 0, w, h))
			region := call(gdi.NewProc("CreateRoundRectRgn"), 0, 0, uintptr(w+1), uintptr(h+1), uintptr(sc(20)), uintptr(sc(20)))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					p := (y*w + x) * 4
					a := byte(255)
					if call(gdi.NewProc("PtInRegion"), region, uintptr(x), uintptr(y)) == 0 {
						a = 0
					}
					im.SetNRGBA(x, y, imagecolor.NRGBA{R: data[p+2], G: data[p+1], B: data[p], A: a})
				}
			}
			call(gdi.NewProc("DeleteObject"), region)
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, im)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		call(gdi.NewProc("SelectObject"), dc, old)
		call(gdi.NewProc("DeleteObject"), bitmap)
		call(gdi.NewProc("DeleteDC"), dc)
	}
	for _, work := range []rect{{0, 0, 1200, 800}, {-1920, 0, 0, 1080}} {
		r := contextMenuRect(point{work.R - 2, work.B - 2}, 248, 120, work)
		if r.L < work.L || r.T < work.T || r.R > work.R || r.B > work.B {
			t.Fatal("context menu escaped monitor", r)
		}
	}
}

func TestThemedContextMenuKeyboardAndFocusDismissal(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	oldWindow, oldItems, oldHover, oldPressed, oldTask, oldFocus := guiContextMenu, guiContextItems, guiContextHover, guiContextPressed, guiContextTask, guiContextPreviousFocus
	defer func() {
		closeThemedContextMenu(false)
		guiContextMenu, guiContextItems, guiContextHover, guiContextPressed, guiContextTask, guiContextPreviousFocus = oldWindow, oldItems, oldHover, oldPressed, oldTask, oldFocus
	}()
	guiContextItems = []contextMenuItem{{172, "复制文件", "Ctrl+C"}, {153, "打开所在文件夹", ""}}
	guiContextHover = -1
	h := createContextMenuWindow(0, rect{0, 0, 248, 84})
	if h == 0 {
		t.Fatal("hidden context menu creation")
	}
	guiContextMenu = h
	msg(h, 0x100, 40, 0)
	if guiContextHover != 0 {
		t.Fatal("down did not select first item")
	}
	msg(h, 0x100, 38, 0)
	if guiContextHover != 1 {
		t.Fatal("up did not wrap")
	}
	msg(h, 0x100, 27, 0)
	if guiContextMenu != 0 || call(user.NewProc("IsWindow"), h) != 0 {
		t.Fatal("escape did not destroy menu")
	}
	guiContextItems = []contextMenuItem{{153, "打开所在文件夹", ""}}
	h = createContextMenuWindow(0, rect{0, 0, 248, 48})
	guiContextMenu = h
	msg(h, 6, 0, 0)
	if guiContextMenu != 0 {
		t.Fatal("deactivation did not dismiss menu")
	}
}
