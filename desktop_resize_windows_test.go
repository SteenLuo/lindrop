package main

import (
	"syscall"
	"testing"
	"unsafe"
)

var resizeProbeOriginal uintptr
var resizeProbePositions int

func resizeProbeProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x47 {
		pos := (*struct {
			H, After     uintptr
			X, Y, W, Hgt int32
			Flags        uint32
		})(unsafe.Pointer(l))
		resizeProbePositions++
		_ = pos
	}
	return call(user.NewProc("CallWindowProcW"), resizeProbeOriginal, h, uintptr(m), w, l)
}

func TestConversationHeightResizeKeepsRasterAndScroll(t *testing.T) {
	h, v := viewportTestWindow(t)
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	defer call(gdi.NewProc("DeleteDC"), dc)
	bitmap := call(gdi.NewProc("CreateBitmap"), 620, 300, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("offscreen bitmap")
	}
	old := call(gdi.NewProc("SelectObject"), dc, bitmap)
	defer func() { call(gdi.NewProc("SelectObject"), dc, old); call(gdi.NewProc("DeleteObject"), bitmap) }()
	v.scroll(h, 57, false)
	bottom := v.Y + float64(chatClient(h).B)
	row := v.raster(dc, 0, int(chatClient(h).R), v.Heights[0])
	if row == nil {
		t.Fatal("row raster")
	}
	for _, height := range []int{290, 280, 310, 250, 300} {
		call(user.NewProc("SetWindowPos"), h, 0, 0, 0, 620, uintptr(height), 0x2|0x4|0x8|0x10)
		if v.Rows[0] != row {
			t.Fatal("height-only resize discarded unchanged message raster", height)
		}
		if v.Y+float64(chatClient(h).B) != bottom || v.Target != v.Y {
			t.Fatal("height-only resize moved visible bottom", v.Y, v.Target, bottom)
		}
	}
	v.scroll(h, v.maximum(h), false)
	for _, height := range []int{270, 340, 250, 300} {
		call(user.NewProc("SetWindowPos"), h, 0, 0, 0, 620, uintptr(height), 0x2|0x4|0x8|0x10)
		if v.Y != v.maximum(h) {
			t.Fatal("latest conversation was covered instead of bottom anchored")
		}
	}
	width := int(chatClient(h).R)
	other := v.raster(dc, 0, width-40, v.Heights[0])
	if other == nil || other == row || other.W != width-40 {
		t.Fatal("width-dependent raster became stale")
	}
}

func TestComposerPanelPositionsAndNativeRedraw(t *testing.T) {
	h, v := viewportTestWindow(t)
	oldMain, oldWidth, oldHeight, oldComposer, oldPositions := guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions
	oldDraft, oldPictures, oldPanel := chatDraftDevice, chatPictures, guiComposerWindow
	oldFooter := guiComposerFooter
	oldAll, oldButtons, oldProc := guiAllControls, guiButtonProc, chatComposeProc
	oldActive, oldFrame := chatResizeActive, chatDragFrame
	oldControls := map[int]uintptr{}
	for _, id := range []int{121, 122, 170, 171, composerPanelID, composerFooterID, composerScrollbarID, composerDownArrowID} {
		oldControls[id] = guiControls[id]
	}
	defer func() {
		guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions = oldMain, oldWidth, oldHeight, oldComposer, oldPositions
		chatDraftDevice, chatPictures, guiComposerWindow = oldDraft, oldPictures, oldPanel
		guiComposerFooter = oldFooter
		guiAllControls, guiButtonProc, chatComposeProc = oldAll, oldButtons, oldProc
		chatResizeActive, chatDragFrame = oldActive, oldFrame
		for id, handle := range oldControls {
			guiControls[id] = handle
		}
	}()
	guiMain = call(createWindow, 0, uintptr(unsafe.Pointer(wp("STATIC"))), 0, 0x82000000, 0, 0, 940, 650, 0, 0, 0, 0)
	if guiMain == 0 {
		t.Fatal("hidden parent")
	}
	defer destroyWindow.Call(guiMain)
	defer cancelComposerResize()
	guiWidth, guiHeight, chatComposerHeight = 940, 650, 96
	guiPositions = map[int][4]int{}
	guiButtonProc = map[uintptr]uintptr{}
	guiAllControls = nil
	guiComposerWindow = 0
	guiComposerFooter = 0
	chatDraftDevice = "resize-test"
	chatPictures = map[string][]draftPicture{}
	call(user.NewProc("SetParent"), h, guiMain)
	call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffff0), 0x52210000)
	for _, id := range []int{121, 122} {
		themedControl("BUTTON", "文件", id, 0)
	}
	createChatComposer()
	if guiComposerWindow == 0 {
		t.Fatal("composer panel creation")
	}
	ex := call(user.NewProc("GetWindowLongW"), guiComposerWindow, uintptr(0xffffffec))
	if ex&0x02010000 != 0x02010000 {
		t.Fatal("composer lacks system buffering or dialog navigation")
	}
	for _, id := range []int{121, 122, 170, composerScrollbarID} {
		if call(user.NewProc("GetParent"), guiControls[id]) != guiComposerWindow {
			t.Fatal("native input/tool not in composited subtree", id)
		}
	}
	if call(user.NewProc("GetParent"), guiControls[171]) != guiComposerFooter {
		t.Fatal("send button must belong to fixed footer")
	}
	if call(user.NewProc("GetParent"), h) != guiMain {
		t.Fatal("conversation must stay outside composited subtree")
	}
	resizeComposerPanel(currentComposerLayout())
	v.scroll(h, 57, false)
	anchoredBottom := v.Y + float64(chatClient(h).B)
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
	resizeProbeOriginal = setWindowProcedure(h, syscall.NewCallback(resizeProbeProc))
	defer setWindowProcedure(h, resizeProbeOriginal)
	for _, height := range []int{140, 180, 64, 250, 96} {
		resizeProbePositions = 0
		if !resizeChatComposer(height) {
			t.Fatal("changed height not applied")
		}
		g := currentComposerLayout()
		if resizeProbePositions != 1 {
			t.Fatal("resize moved viewport more than once", resizeProbePositions)
		}
		if v.Rows[0] != row || v.Y+float64(chatClient(h).B) != anchoredBottom {
			t.Fatal("resize lost cache or bottom position")
		}
		if int(chatClient(h).B) != g.ListHeight || guiPositions[170][3] != g.TextHeight || guiPositions[170][1] != g.TextTop-g.Top {
			t.Fatal("child geometry does not match panel coordinates")
		}
		if guiPositions[171][1] != 9 {
			t.Fatal("send button left fixed footer baseline")
		}
		resizeProbePositions = 0
		if resizeChatComposer(height) || resizeProbePositions != 0 {
			t.Fatal("unchanged drag position moved native windows")
		}
	}
	chatResizeActive = true
	resizeProbePositions = 0
	for _, height := range []int{180, 220, 170} {
		requestComposerResize(height)
	}
	if currentComposerLayout().TextHeight != 96 || resizeProbePositions != 0 {
		t.Fatal("pointer handling performed immediate layout")
	}
	flushComposerResize()
	if currentComposerLayout().TextHeight != 170 || resizeProbePositions != 1 {
		t.Fatal("frame did not coalesce pointer positions")
	}
	requestComposerResize(180)
	finishComposerResize()
	if currentComposerLayout().TextHeight != 180 || chatResizeActive || chatDragFrame.Pending || chatDragFrame.Armed {
		t.Fatal("release did not flush latest height and stop cadence")
	}
	if call(user.NewProc("IsWindowVisible"), guiMain) != 0 {
		t.Fatal("local test showed a window")
	}
}

func TestConversationBottomMessageSurvivesRowRebuild(t *testing.T) {
	h, v := viewportTestWindow(t)
	id := guiTasks[8].ID
	// A message can span multiple raster rows after a layout rebuild.
	guiTasks = append(guiTasks[:9], append([]Task{guiTasks[8]}, guiTasks[9:]...)...)
	v.Heights = append(v.Heights[:9], append([]int{70}, v.Heights[9:]...)...)
	v.Heights[8] = 20
	v.Dirty = true
	v.index()
	if !restoreChatBottomMessage(h, id, 60) || v.Y+float64(chatClient(h).B) != float64(v.Tops[8]+60) {
		t.Fatal("bottom anchor was lost across message raster parts")
	}
	oldBottom := v.Y + float64(chatClient(h).B)
	guiTasks = append([]Task{{ID: "inserted-before-history"}}, guiTasks...)
	v.Heights = append([]int{99}, v.Heights...)
	v.Dirty = true
	if !restoreChatBottomMessage(h, id, 60) || v.Y+float64(chatClient(h).B) != oldBottom+99 {
		t.Fatal("inserting above history changed the bottom message offset")
	}
	if restoreChatBottomMessage(h, "removed-message", 60) {
		t.Fatal("missing bottom message must use the caller's fallback")
	}
	if !restoreChatBottomMessage(h, id, 1000) || v.Y+float64(chatClient(h).B) != float64(v.Tops[11]) {
		t.Fatal("shorter message must clamp anchor to its end")
	}
}

func TestViewportDirtyStripKeepsOtherPixels(t *testing.T) {
	h, v := viewportTestWindow(t)
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	defer call(gdi.NewProc("DeleteDC"), dc)
	bitmap := call(gdi.NewProc("CreateBitmap"), 620, 300, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("offscreen bitmap")
	}
	old := call(gdi.NewProc("SelectObject"), dc, bitmap)
	defer func() { call(gdi.NewProc("SelectObject"), dc, old); call(gdi.NewProc("DeleteObject"), bitmap) }()
	sentinel := color(201, 12, 177)
	fill(dc, rect{0, 0, 620, 300}, sentinel)
	v.scroll(h, 57, false)
	width := int(chatClient(h).R)
	paintChatViewportRange(dc, v, width, 100, 130, 300)
	for _, y := range []int{0, 99, 130, 299} {
		if call(gdi.NewProc("GetPixel"), dc, 100, uintptr(y)) != sentinel {
			t.Fatal("strip painting escaped dirty rows", y)
		}
	}
	i := v.at(57 + 110)
	row := v.Rows[i]
	if row == nil {
		t.Fatal("visible strip did not use message raster")
	}
	wanted := call(gdi.NewProc("GetPixel"), row.DC, 100, uintptr(110+57-v.Tops[i]))
	if got := call(gdi.NewProc("GetPixel"), dc, 100, 110); got != wanted {
		t.Fatal("partial paint lost pixel scroll offset", got, wanted)
	}
}

func TestComposerDragBackgroundUsesOnlyExposedGap(t *testing.T) {
	oldActive, oldPage, oldTasks, oldScale := chatResizeActive, guiPage, guiTasks, guiScale
	defer func() { chatResizeActive, guiPage, guiTasks, guiScale = oldActive, oldPage, oldTasks, oldScale }()
	chatResizeActive = true
	guiPage = 0
	guiTasks = []Task{{ID: "sample"}}
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	defer call(gdi.NewProc("DeleteDC"), dc)
	bitmap := call(gdi.NewProc("CreateBitmap"), 1000, 800, 1, 32, 0)
	if bitmap == 0 {
		t.Fatal("offscreen bitmap")
	}
	old := call(gdi.NewProc("SelectObject"), dc, bitmap)
	defer func() { call(gdi.NewProc("SelectObject"), dc, old); call(gdi.NewProc("DeleteObject"), bitmap) }()
	for _, scale := range []float64{1, 2} {
		guiScale = scale
		sentinel := color(201, 12, 177)
		fill(dc, rect{0, 0, 1000, 800}, sentinel)
		area := rct(318, 100, 100, 10)
		if !paintComposerDragBackground(dc, area) {
			t.Fatal("resize background used full scene")
		}
		if call(gdi.NewProc("GetPixel"), dc, uintptr(area.L+1), uintptr(area.T+1)) != canvas {
			t.Fatal("gap was not filled")
		}
		if call(gdi.NewProc("GetPixel"), dc, uintptr(area.L-1), uintptr(area.T+1)) != sentinel {
			t.Fatal("gap fill escaped its update region")
		}
		if paintComposerDragBackground(dc, rect{0, 0, 1000, 800}) {
			t.Fatal("partial path attempted to draw header/device area")
		}
	}
}
