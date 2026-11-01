package main

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

var resizeProbeOriginal uintptr
var resizeProbePaints, resizeProbeErases, resizeProbePositions int
var resizeProbeImmediate bool

func resizeProbeProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0xf {
		resizeProbePaints++
	}
	if m == 0x14 {
		resizeProbeErases++
	}
	if m == 0x47 {
		pos := (*struct {
			H, After     uintptr
			X, Y, W, Hgt int32
			Flags        uint32
		})(unsafe.Pointer(l))
		resizeProbePositions++
		if pos.Flags&0x8 == 0 {
			resizeProbeImmediate = true
		}
	}
	return call(user.NewProc("CallWindowProcW"), resizeProbeOriginal, h, uintptr(m), w, l)
}

func TestConversationHeightResizeKeepsRasterAndScroll(t *testing.T) {
	h, v := viewportTestWindow(t)
	// Use a memory-only DC: no screen pixels, visible window or mouse capture.
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	defer call(gdi.NewProc("DeleteDC"), dc)
	bitmap := call(gdi.NewProc("CreateBitmap"), 620, 300, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("offscreen bitmap")
	}
	old := call(gdi.NewProc("SelectObject"), dc, bitmap)
	defer func() { call(gdi.NewProc("SelectObject"), dc, old); call(gdi.NewProc("DeleteObject"), bitmap) }()
	v.scroll(h, 57, false)
	row := v.raster(dc, 0, int(chatClient(h).R), v.Heights[0])
	if row == nil {
		t.Fatal("row raster")
	}
	for _, height := range []int{290, 280, 310, 250, 300} {
		call(user.NewProc("SetWindowPos"), h, 0, 0, 0, 620, uintptr(height), 0x2|0x4|0x8|0x10)
		if v.Rows[0] != row {
			t.Fatal("height-only resize discarded unchanged message raster", height)
		}
		if v.Y != 57 || v.Target != 57 {
			t.Fatal("height-only resize lost scroll offset", v.Y, v.Target)
		}
	}
	// A width change must still invalidate the old width when rasterized.
	width := int(chatClient(h).R)
	other := v.raster(dc, 0, width-40, v.Heights[0])
	if other == nil || other == row || other.W != width-40 {
		t.Fatal("width-dependent raster became stale")
	}
}

func TestComposerResizeBatchesGeometryWithoutIntermediatePainting(t *testing.T) {
	h, v := viewportTestWindow(t)
	oldMain, oldWidth, oldHeight, oldComposer, oldPositions := guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions
	oldDraft, oldPictures := chatDraftDevice, chatPictures
	oldControls := map[int]uintptr{}
	for _, id := range []int{121, 122, 170} {
		oldControls[id] = guiControls[id]
	}
	defer func() {
		guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions = oldMain, oldWidth, oldHeight, oldComposer, oldPositions
		chatDraftDevice, chatPictures = oldDraft, oldPictures
		for id, handle := range oldControls {
			guiControls[id] = handle
		}
	}()
	guiMain = call(createWindow, 0, uintptr(unsafe.Pointer(wp("STATIC"))), 0, 0x82000000, 0, 0, 940, 650, 0, 0, 0, 0)
	if guiMain == 0 {
		t.Fatal("hidden parent")
	}
	defer destroyWindow.Call(guiMain)
	guiWidth, guiHeight, chatComposerHeight = 940, 650, 96
	guiPositions = map[int][4]int{}
	chatDraftDevice = "resize-test"
	chatPictures = map[string][]draftPicture{}
	call(user.NewProc("SetParent"), h, guiMain)
	call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffff0), 0x52210000)
	for _, id := range []int{121, 122, 170} {
		class, style := "BUTTON", uintptr(0x50000000)
		if id == 170 {
			class = "EDIT"
			style |= 4 | 0x40 | 0x1000 | 0x00200000
		}
		guiControls[id] = call(createWindow, 0, uintptr(unsafe.Pointer(wp(class))), 0, style, 0, 0, 0, 0, guiMain, uintptr(id), 0, 0)
		if guiControls[id] == 0 {
			t.Fatal("hidden child")
		}
	}
	g := currentComposerLayout()
	call(user.NewProc("SetWindowPos"), h, 0, 334, 74, 590, uintptr(g.ListHeight), 0x4|0x8|0x10)
	v.scroll(h, 57, false)
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	defer call(gdi.NewProc("DeleteDC"), dc)
	bitmap := call(gdi.NewProc("CreateBitmap"), 620, 300, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("offscreen bitmap")
	}
	old := call(gdi.NewProc("SelectObject"), dc, bitmap)
	defer func() { call(gdi.NewProc("SelectObject"), dc, old); call(gdi.NewProc("DeleteObject"), bitmap) }()
	row := v.raster(dc, 0, int(chatClient(h).R), v.Heights[0])
	if row == nil {
		t.Fatal("raster")
	}
	visibleRunner := os.Getenv("GITHUB_ACTIONS") == "true"
	if visibleRunner {
		// This runs only on the disposable CI desktop. Local execution always
		// keeps both windows hidden and never changes capture or focus.
		showWindow.Call(guiMain, 4)
		call(user.NewProc("RedrawWindow"), guiMain, 0, 0, 0x1|0x80|0x100)
	}
	resizeProbeOriginal = setWindowProcedure(h, syscall.NewCallback(resizeProbeProc))
	defer setWindowProcedure(h, resizeProbeOriginal)
	for _, height := range []int{140, 180, 64, 250, 96} {
		call(user.NewProc("ValidateRect"), guiMain, 0)
		call(user.NewProc("ValidateRect"), h, 0)
		resizeProbePaints, resizeProbeErases, resizeProbePositions = 0, 0, 0
		resizeProbeImmediate = false
		before := currentComposerLayout()
		if !resizeChatComposer(height) {
			t.Fatal("changed height not applied")
		}
		after := currentComposerLayout()
		if resizeProbeImmediate || resizeProbePaints != 0 || resizeProbeErases != 0 {
			t.Fatal("resize exposed an intermediate frame", resizeProbePaints, resizeProbeErases, resizeProbeImmediate)
		}
		if resizeProbePositions != 1 {
			t.Fatal("viewport moved more than once", resizeProbePositions)
		}
		if v.Rows[0] != row || v.Y != 57 {
			t.Fatal("resize lost cached content or scroll offset")
		}
		if int(chatClient(h).B) != after.ListHeight || guiPositions[170][3] != after.TextHeight {
			t.Fatal("native geometry did not commit")
		}
		if visibleRunner {
			var dirty rect
			if call(user.NewProc("GetUpdateRect"), guiMain, uintptr(unsafe.Pointer(&dirty)), 0) == 0 {
				t.Fatal("composer background not invalidated")
			}
			top := before.Top
			if after.Top < top {
				top = after.Top
			}
			if dirty.L < 318 || dirty.T < int32(top-5) {
				t.Fatal("unchanged header or device area invalidated", dirty)
			}
			call(user.NewProc("UpdateWindow"), guiMain)
			call(user.NewProc("UpdateWindow"), h)
			if resizeProbePaints != 1 || resizeProbeErases != 0 {
				t.Fatal("expected one buffered frame without erase", resizeProbePaints, resizeProbeErases)
			}
			if v.Rows[0] != row {
				t.Fatal("final frame rebuilt unchanged message")
			}
		}
		call(user.NewProc("ValidateRect"), guiMain, 0)
		call(user.NewProc("ValidateRect"), h, 0)
		resizeProbePositions = 0
		if resizeChatComposer(height) || resizeProbePositions != 0 || call(user.NewProc("GetUpdateRect"), guiMain, 0, 0) != 0 || call(user.NewProc("GetUpdateRect"), h, 0, 0) != 0 {
			t.Fatal("unchanged drag position repainted")
		}
	}
	if !visibleRunner && call(user.NewProc("IsWindowVisible"), guiMain) != 0 {
		t.Fatal("test showed a window")
	}
}
