package main

import (
	"syscall"
	"unsafe"
)

const composerPanelID = 480
const composerFooterID = 481

var guiComposerWindow, guiComposerFooter uintptr

// A separate composited subtree keeps native EDIT background erasure and
// owner-drawn toolbar painting in one system buffer. The conversation/video
// surfaces are outside this subtree and retain their existing painting path.
func createComposerPanel() {
	if guiComposerWindow != 0 && guiComposerFooter != 0 {
		return
	}
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(composerPanelProc), Instance: instance, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Name: wp("LinDropComposerPanel")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	for _, item := range []struct {
		id     int
		handle *uintptr
	}{{composerPanelID, &guiComposerWindow}, {composerFooterID, &guiComposerFooter}} {
		if *item.handle != 0 {
			continue
		}
		*item.handle = call(createWindow, 0x02000000|0x00010000, uintptr(unsafe.Pointer(wc.Name)), 0, 0x56000000, 0, 0, 0, 0, guiMain, uintptr(item.id), instance, 0)
		if *item.handle != 0 {
			guiControls[item.id] = *item.handle
			guiAllControls = append(guiAllControls, item.id)
			acceptDesktopDrops(*item.handle)
		}
	}
}

func attachComposerControls() {
	for _, id := range []int{121, 122, 170, 171} {
		parent := guiComposerWindow
		if id == 171 {
			parent = guiComposerFooter
		}
		if h := guiControls[id]; h != 0 && parent != 0 {
			call(user.NewProc("SetParent"), h, parent)
			delete(guiPositions, id)
		}
	}
}

func composerLocalPosition(id, x, y int) (int, int) {
	parent := call(user.NewProc("GetParent"), guiControls[id])
	if guiComposerWindow != 0 && parent == guiComposerWindow {
		return x - 318, y - currentComposerLayout().Top
	}
	if guiComposerFooter != 0 && parent == guiComposerFooter {
		return x - 318, y - currentComposerLayout().FooterTop
	}
	return x, y
}

func composerPanelProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 2:
		if guiComposerWindow == h {
			guiComposerWindow = 0
		}
		if guiComposerFooter == h {
			guiComposerFooter = 0
		}
	case 0x14:
		// This custom background is fully drawn during WM_PAINT. Standard
		// EDIT children are allowed to erase normally inside the system buffer.
		return 1
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), h, uintptr(unsafe.Pointer(&ps)))
		if dc != 0 {
			if h == guiComposerFooter {
				paintComposerFooterOffset(dc, -318, -currentComposerLayout().FooterTop)
			} else {
				paintComposerSurfaceOffset(dc, -318, -currentComposerLayout().Top)
			}
			call(user.NewProc("EndPaint"), h, uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	case 0x111, 0x2b, 0x133, 0x138, wmDropFiles:
		return msg(guiMain, m, w, l)
	case 0x20:
		if composerMouse(guiMain, m, w, l) {
			return 1
		}
	case 0x201, 0x200, 0x202, 0x215, 0x1f:
		p := point{int32(int16(l)), int32(int16(l >> 16))}
		call(user.NewProc("MapWindowPoints"), h, guiMain, uintptr(unsafe.Pointer(&p)), 1)
		mapped := uintptr(uint16(p.X)) | uintptr(uint16(p.Y))<<16
		if composerMouse(guiMain, m, w, mapped) {
			return 0
		}
	}
	return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
}

type composerPlacement struct {
	id       int
	position [4]int
}

// Like mature native splitter implementations, use ordinary positioning and
// let Windows retain/erase valid pixels. Do not discard valid pixels and then
// suppress native control erasure with SWP_NOREDRAW/RDW_NOERASE.
func positionComposerWindows(items []composerPlacement) {
	changed := make([]composerPlacement, 0, len(items))
	for _, item := range items {
		if guiControls[item.id] != 0 && guiPositions[item.id] != item.position {
			changed = append(changed, item)
		}
	}
	if len(changed) == 0 {
		return
	}
	const flags = 0x4 | 0x10 // NOZORDER, NOACTIVATE; normal redraw and valid-bit preservation.
	batch := call(user.NewProc("BeginDeferWindowPos"), uintptr(len(changed)))
	for _, item := range changed {
		if batch == 0 {
			break
		}
		p := item.position
		batch = call(user.NewProc("DeferWindowPos"), batch, guiControls[item.id], 0, uintptr(sc(p[0])), uintptr(sc(p[1])), uintptr(sc(p[2])), uintptr(sc(p[3])), flags)
	}
	committed := batch != 0 && call(user.NewProc("EndDeferWindowPos"), batch) != 0
	for _, item := range changed {
		p := item.position
		if !committed {
			call(user.NewProc("SetWindowPos"), guiControls[item.id], 0, uintptr(sc(p[0])), uintptr(sc(p[1])), uintptr(sc(p[2])), uintptr(sc(p[3])), flags)
		}
		guiPositions[item.id] = p
	}
}

func resizeComposerPanel(g composerGeometry) {
	positionComposerWindows([]composerPlacement{
		{126, [4]int{334, 74, guiWidth - 350, g.ListHeight}},
		{composerPanelID, [4]int{318, g.Top, guiWidth - 318, g.FooterTop - g.Top}},
		{composerFooterID, [4]int{318, g.FooterTop, guiWidth - 318, composerFooterHeight}},
	})
	items := []composerPlacement{
		{121, [4]int{342, g.Top + 9, 80, 27}},
		{122, [4]int{432, g.Top + 9, 80, 27}},
		{170, [4]int{343, g.TextTop, guiWidth - 370, g.TextHeight}},
		{171, [4]int{guiWidth - 103, guiHeight - 36, 78, 29}},
	}
	for i := range items {
		p := &items[i].position
		p[0], p[1] = composerLocalPosition(items[i].id, p[0], p[1])
	}
	// Toolbar/edit share the resizing pane. Send belongs to the fixed footer;
	// each native positioning batch must contain siblings only.
	positionComposerWindows(items[:3])
	positionComposerWindows(items[3:])
}
