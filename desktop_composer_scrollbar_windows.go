package main

import (
	"syscall"
	"unsafe"
)

const composerScrollbarID = 482

var composerWheelRemainder int64
var composerScrollbarProc uintptr

type composerScrollInfo struct {
	Size, Mask uint32
	Min, Max   int32
	Page       uint32
	Pos, Track int32
}

func createComposerScrollbar() {
	composerWheelRemainder = 0
	h := themedControl("SCROLLBAR", "", composerScrollbarID, 1) // SBS_VERT
	style := call(user.NewProc("GetWindowLongW"), h, uintptr(0xfffffff0))
	call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffff0), style&^0x10000)
	composerScrollbarProc = setWindowProcedure(h, syscall.NewCallback(composerScrollbarWindowProc))
}

func composerScrollbarWindowProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x20a {
		scrollComposerWheel(w)
		return 0
	}
	return call(user.NewProc("CallWindowProcW"), composerScrollbarProc, h, uintptr(m), w, l)
}

func composerScrollbarWidth() int {
	pixels := call(user.NewProc("GetSystemMetrics"), 2) // SM_CXVSCROLL
	if pixels == 0 {
		pixels = 17
	}
	return int(float64(pixels)/guiScale + .999)
}

func composerInputScrollState() composerScrollInfo {
	h := guiControls[170]
	count := int32(msg(h, 0xba, 0, 0)) // Includes native soft-wrapped lines.
	if count < 1 {
		count = 1
	}
	var format rect
	msg(h, 0xb2, 0, uintptr(unsafe.Pointer(&format))) // EM_GETRECT
	// A memory DC avoids flushing the composited edit surface midway through
	// a resize. TEXTMETRICW starts with LONG tmHeight; reserve its full size.
	var metrics [64]byte
	dc := call(gdi.NewProc("CreateCompatibleDC"), 0)
	font := msg(h, 0x31, 0, 0)
	old := call(gdi.NewProc("SelectObject"), dc, font)
	call(gdi.NewProc("GetTextMetricsW"), dc, uintptr(unsafe.Pointer(&metrics[0])))
	call(gdi.NewProc("SelectObject"), dc, old)
	call(gdi.NewProc("DeleteDC"), dc)
	lineHeight := *(*int32)(unsafe.Pointer(&metrics[0]))
	if lineHeight < 1 {
		lineHeight = int32(sc(16))
	}
	page := (format.B - format.T) / lineHeight
	if page < 1 {
		page = 1
	}
	return composerScrollInfo{Size: uint32(unsafe.Sizeof(composerScrollInfo{})), Mask: 1 | 2 | 4 | 8,
		Max: count - 1, Page: uint32(page), Pos: int32(msg(h, 0xce, 0, 0))}
}

func updateComposerScrollbar() {
	h, bar := guiControls[170], guiControls[composerScrollbarID]
	if h == 0 || bar == 0 {
		return
	}
	next := composerInputScrollState()
	old := composerScrollInfo{Size: next.Size, Mask: 1 | 2 | 4}
	call(user.NewProc("GetScrollInfo"), bar, 2, uintptr(unsafe.Pointer(&old)))
	if old.Min != next.Min || old.Max != next.Max || old.Page != next.Page || old.Pos != next.Pos {
		call(user.NewProc("SetScrollInfo"), bar, 2, uintptr(unsafe.Pointer(&next)), 1)
	}
	// SIF_DISABLENOSCROLL may disable a blank scrollbar. Do not let ordinary
	// device readiness re-enable it on every full window layout.
	enabled(composerScrollbarID, call(user.NewProc("IsWindowEnabled"), h) != 0 && int64(next.Max)+1 > int64(next.Page))
}

func scrollComposerInput(w uintptr) {
	h := guiControls[170]
	command := w & 0xffff
	if command == 4 || command == 5 { // Full 32-bit thumb position, not HIWORD.
		s := composerScrollInfo{Size: uint32(unsafe.Sizeof(composerScrollInfo{})), Mask: 0x10}
		call(user.NewProc("GetScrollInfo"), guiControls[composerScrollbarID], 2, uintptr(unsafe.Pointer(&s)))
		scrollComposerInputTo(s.Track)
	} else {
		msg(h, 0x115, command, 0)
	}
	updateComposerScrollbar()
}

func scrollComposerInputTo(line int32) {
	h := guiControls[170]
	current := int32(msg(h, 0xce, 0, 0))
	msg(h, 0xb6, 0, uintptr(line-current)) // EM_LINESCROLL, 32-bit signed line delta.
}

func scrollComposerWheel(w uintptr) {
	lines := uint32(3)
	call(user.NewProc("SystemParametersInfoW"), 0x68, 0, uintptr(unsafe.Pointer(&lines)), 0) // SPI_GETWHEELSCROLLLINES
	state := composerInputScrollState()
	if lines == ^uint32(0) {
		lines = state.Page
	} // WHEEL_PAGESCROLL
	composerWheelRemainder += int64(int16(w>>16)) * int64(lines)
	steps := composerWheelRemainder / 120
	composerWheelRemainder %= 120
	if steps == 0 {
		return
	}
	target := int64(state.Pos) - steps
	if target < 0 {
		target = 0
	}
	if target > int64(state.Max) {
		target = int64(state.Max)
	}
	scrollComposerInputTo(int32(target))
	updateComposerScrollbar()
}
