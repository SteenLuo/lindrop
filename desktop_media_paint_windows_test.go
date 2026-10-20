package main

import (
	"image"
	imagecolor "image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var mediaPaintTestCount, mediaTimerTestCount int

func mediaCountTestProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0xf {
		mediaPaintTestCount++
	}
	if m == 0x113 {
		mediaTimerTestCount++
	}
	return mediaWindowProc(h, m, w, l)
}
func pumpMediaTest(duration time.Duration) {
	end := time.Now().Add(duration)
	for time.Now().Before(end) {
		var m winMsg
		for call(user.NewProc("PeekMessageW"), uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1) != 0 {
			call(user.NewProc("TranslateMessage"), uintptr(unsafe.Pointer(&m)))
			call(user.NewProc("DispatchMessageW"), uintptr(unsafe.Pointer(&m)))
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestImagePreviewIdleHasNoPeriodicRepaint(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vectorStart()
	if vectorToken == 0 {
		t.Fatal("GDI+ unavailable")
	}
	defer func() { releaseChatMedia(); call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	path := filepath.Join(t.TempDir(), "静态图片.png")
	img := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			img.SetNRGBA(x, y, imagecolor.NRGBA{R: 90, G: 140, B: 180, A: 255})
		}
	}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
	f.Close()
	oldBrush := guiWhiteBrush
	guiWhiteBrush = call(gdi.NewProc("CreateSolidBrush"), paper)
	defer func() { call(gdi.NewProc("DeleteObject"), guiWhiteBrush); guiWhiteBrush = oldBrush }()
	showMediaWindow(path, "image")
	if mediaWindow == 0 {
		t.Fatal("preview did not open")
	}
	defer func() {
		if mediaWindow != 0 {
			destroyWindow.Call(mediaWindow)
		}
	}()
	setWindowProcedure(mediaWindow, syscall.NewCallback(mediaCountTestProc))
	pumpMediaTest(150 * time.Millisecond)
	mediaPaintTestCount = 0
	mediaTimerTestCount = 0
	pumpMediaTest(1650 * time.Millisecond)
	if mediaTimerTestCount != 0 || mediaPaintTestCount != 0 {
		t.Fatalf("static preview keeps refreshing: %d timer ticks, %d paints", mediaTimerTestCount, mediaPaintTestCount)
	}
	// A delayed old timer message must also leave a static image untouched.
	call(user.NewProc("ValidateRect"), mediaWindow, 0)
	msg(mediaWindow, 0x113, 1, 0)
	if call(user.NewProc("GetUpdateRect"), mediaWindow, 0, 0) != 0 {
		t.Fatal("stale timer invalidated a static image")
	}
	// Real resize/exposure still repaints and keeps image content visible.
	mediaPaintTestCount = 0
	call(user.NewProc("SetWindowPos"), mediaWindow, 0, 0, 0, 640, 480, 0x16)
	pumpMediaTest(100 * time.Millisecond)
	if mediaPaintTestCount == 0 {
		t.Fatal("resize did not repaint")
	}
	dc := call(user.NewProc("GetDC"), mediaWindow)
	pixel := call(gdi.NewProc("GetPixel"), dc, 320, 160)
	call(user.NewProc("ReleaseDC"), mediaWindow, dc)
	if pixel == paper || pixel == 0xffffffff {
		t.Fatalf("image missing after resize: %x", pixel)
	}
	// Video progress updates may invalidate only the status strip.
	mediaType = "video"
	call(user.NewProc("ValidateRect"), mediaWindow, 0)
	msg(mediaWindow, 0x113, 1, 0)
	var update, client rect
	call(user.NewProc("GetClientRect"), mediaWindow, uintptr(unsafe.Pointer(&client)))
	if call(user.NewProc("GetUpdateRect"), mediaWindow, uintptr(unsafe.Pointer(&update)), 0) == 0 || update.T < client.B-int32(sc(88)) || update.B > client.B-int32(sc(60)) {
		t.Fatal("video timer invalidated image/player area", update, client)
	}
	mediaType = "image"
}
