package main

import (
	"syscall"
	"testing"
	"unsafe"
)

var footerProbeOriginal = map[uintptr]uintptr{}
var footerProbeMoves int

type composerScrollbarInfo struct {
	Size                                        uint32
	Rect                                        rect
	LineButton, ThumbTop, ThumbBottom, Reserved int32
	State                                       [6]uint32
}

func composerScrollbarBounds(h uintptr) (rect, bool) {
	s := composerScrollbarInfo{}
	s.Size = uint32(unsafe.Sizeof(s))
	object := int32(-5) // OBJID_VSCROLL
	ok := call(user.NewProc("GetScrollBarInfo"), h, uintptr(object), uintptr(unsafe.Pointer(&s))) != 0
	return s.Rect, ok
}

func footerProbeProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x47 {
		footerProbeMoves++
	}
	return call(user.NewProc("CallWindowProcW"), footerProbeOriginal[h], h, uintptr(m), w, l)
}

func TestComposerFooterDoesNotBelongToResizableInputPane(t *testing.T) {
	h, _ := viewportTestWindow(t)
	oldMain, oldWidth, oldHeight, oldComposer, oldPositions := guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions
	oldAll, oldButtons, oldProc, oldPanel := guiAllControls, guiButtonProc, chatComposeProc, guiComposerWindow
	oldDraft, oldPictures := chatDraftDevice, chatPictures
	oldFooter, oldScale := guiComposerFooter, guiScale
	oldControls := map[int]uintptr{}
	for _, id := range []int{121, 122, 170, 171, 480, 481} {
		oldControls[id] = guiControls[id]
	}
	defer func() {
		guiMain, guiWidth, guiHeight, chatComposerHeight, guiPositions = oldMain, oldWidth, oldHeight, oldComposer, oldPositions
		guiAllControls, guiButtonProc, chatComposeProc, guiComposerWindow = oldAll, oldButtons, oldProc, oldPanel
		chatDraftDevice, chatPictures = oldDraft, oldPictures
		guiComposerFooter, guiScale = oldFooter, oldScale
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
	guiButtonProc = map[uintptr]uintptr{}
	guiAllControls = nil
	guiComposerWindow = 0
	guiComposerFooter = 0
	chatDraftDevice = "footer-test"
	chatPictures = map[string][]draftPicture{}
	call(user.NewProc("SetParent"), h, guiMain)
	for _, id := range []int{121, 122} {
		themedControl("BUTTON", "文件", id, 0)
	}
	createChatComposer()
	resizeComposerPanel(currentComposerLayout())
	parent := call(user.NewProc("GetParent"), guiControls[171])
	if parent == 0 || parent == guiComposerWindow {
		t.Fatal("send button is still a child of the height-changing input pane")
	}
	if call(user.NewProc("GetParent"), parent) != guiMain {
		t.Fatal("footer is not independently anchored to main client")
	}
	button := guiControls[171]
	for _, scale := range []float64{1, 2} {
		guiScale = scale
		guiPositions = map[int][4]int{}
		call(user.NewProc("SetWindowPos"), guiMain, 0, 0, 0, uintptr(sc(guiWidth)), uintptr(sc(guiHeight)), 0x4|0x10)
		resizeComposerPanel(currentComposerLayout())
		var anchored rect
		call(user.NewProc("GetWindowRect"), button, uintptr(unsafe.Pointer(&anchored)))
		for _, handle := range []uintptr{parent, button} {
			footerProbeOriginal[handle] = setWindowProcedure(handle, syscall.NewCallback(footerProbeProc))
		}
		footerProbeMoves = 0
		scrollBottom := int32(0)
		for _, height := range []int{64, 65, 66, 67, 80, 96, 180, 260, 96} {
			resizeChatComposer(height)
			scroll, ok := composerScrollbarBounds(guiControls[170])
			if !ok {
				t.Fatal("native edit scrollbar geometry unavailable")
			}
			if scrollBottom == 0 {
				scrollBottom = scroll.B
			} else if scroll.B != scrollBottom {
				t.Fatalf("input scrollbar bottom jumps during resize: scale=%g height=%d bottom=%d want=%d client=%+v", scale, height, scroll.B, scrollBottom, chatClient(guiControls[170]))
			}
			var now rect
			call(user.NewProc("GetWindowRect"), button, uintptr(unsafe.Pointer(&now)))
			if now != anchored || footerProbeMoves != 0 {
				t.Fatal("input height moved fixed footer/button", scale, height, now, anchored, footerProbeMoves)
			}
			if int(chatClient(parent).B) != sc(composerFooterHeight) || guiPositions[171][1] != 9 {
				t.Fatal("footer height or button local baseline changed")
			}
		}
		for handle, proc := range footerProbeOriginal {
			setWindowProcedure(handle, proc)
		}
		footerProbeOriginal = map[uintptr]uintptr{}
		for _, size := range [][2]int{{1100, 800}, {940, 650}} {
			guiWidth, guiHeight = size[0], size[1]
			call(user.NewProc("SetWindowPos"), guiMain, 0, 0, 0, uintptr(sc(guiWidth)), uintptr(sc(guiHeight)), 0x4|0x10)
			resizeComposerPanel(currentComposerLayout())
			var position point
			call(user.NewProc("MapWindowPoints"), parent, guiMain, uintptr(unsafe.Pointer(&position)), 1)
			var nativeButton rect
			call(user.NewProc("GetWindowRect"), button, uintptr(unsafe.Pointer(&nativeButton)))
			if int(position.Y) != sc(guiHeight-composerFooterHeight) || int(chatClient(parent).B) != sc(composerFooterHeight) || int(nativeButton.B-nativeButton.T) != sc(29) {
				t.Fatal("window resize stretched footer/button", scale, size, position, nativeButton)
			}
		}
	}
}

func TestInitialDesktopFrameFitsWorkArea(t *testing.T) {
	for _, work := range []rect{{0, 0, 1024, 720}, {80, 0, 1920, 1040}, {-1920, 40, 0, 1080}, {0, 0, 2560, 1344}} {
		frame := initialDesktopFrame(work, 2400, 1400)
		if frame.L < work.L+20 || frame.R > work.R-20 || frame.T < work.T+20 || frame.B > work.B-20 {
			t.Fatal("initial window/footer overlaps taskbar or work-area edge", work, frame)
		}
	}
}
