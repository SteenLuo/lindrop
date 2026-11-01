package main

import (
	"strings"
	"testing"
	"unsafe"
)

func checkComposerScrollbarNavigation(t *testing.T, edit uintptr) {
	t.Helper()
	bar := guiControls[composerScrollbarID]
	if call(user.NewProc("GetParent"), bar) != call(user.NewProc("GetParent"), edit) {
		t.Fatal("text and scrollbar must share the composited client surface")
	}
	if call(user.NewProc("GetWindowLongW"), bar, uintptr(0xfffffff0))&0x10000 != 0 {
		t.Fatal("scrollbar must not add an extra Tab stop")
	}
	read := func() composerScrollInfo {
		s := composerScrollInfo{Size: uint32(unsafe.Sizeof(composerScrollInfo{})), Mask: 1 | 2 | 4}
		if call(user.NewProc("GetScrollInfo"), bar, 2, uintptr(unsafe.Pointer(&s))) == 0 {
			t.Fatal("scroll info unavailable")
		}
		return s
	}
	msg(edit, 0xb1, 5, 11) // Keep the real native selection during navigation.
	selection := msg(edit, 0xb0, 0, 0)
	scrollComposerInput(6) // SB_TOP
	if msg(edit, 0xce, 0, 0) != 0 {
		t.Fatal("top arrow command did not reach native edit")
	}
	first := read()
	if first.Max <= int32(first.Page) {
		t.Fatal("wrapped multiline text has no scroll range")
	}
	scrollComposerInput(3) // SB_PAGEDOWN
	if msg(edit, 0xce, 0, 0) == 0 {
		t.Fatal("page down did not scroll native edit")
	}
	scrollComposerInput(7) // SB_BOTTOM
	if got := read(); got.Pos != int32(msg(edit, 0xce, 0, 0)) || got.Pos == 0 {
		t.Fatal("scrollbar does not follow native text position", got)
	}
	scrollComposerInput(6)
	msg(edit, 0x20a, uintptr(uint32(0xff88)<<16), 0) // One negative wheel notch.
	if got := read(); got.Pos == 0 || got.Pos != int32(msg(edit, 0xce, 0, 0)) {
		t.Fatal("wheel and scrollbar lost synchronization", got)
	}
	if msg(edit, 0xb0, 0, 0) != selection {
		t.Fatal("scrolling changed text selection")
	}
	scrollComposerInput(6)
	msg(bar, 0x20a, uintptr(uint32(0xff88)<<16), 0)
	if got := read(); got.Pos == 0 || got.Pos != int32(msg(edit, 0xce, 0, 0)) {
		t.Fatal("wheel over scrollbar did not move input text", got)
	}
	call(user.NewProc("MoveWindow"), edit, 0, 0, 500, 192, 0)
	if got := read(); got.Page <= first.Page {
		t.Fatal("scroll page did not follow input height", first, got)
	}
	// More than 65535 wrapped lines must not truncate a thumb target to HIWORD.
	set(edit, strings.Repeat("a\r\n", 70000))
	scrollComposerInputTo(65570)
	updateComposerScrollbar()
	if got := read(); got.Max < 70000 || got.Pos != 65570 || msg(edit, 0xce, 0, 0) != 65570 {
		t.Fatal("large text thumb position truncated", got)
	}
	set(edit, "")
	if got := read(); got.Max != 0 || got.Pos != 0 {
		t.Fatal("empty input kept a stale scroll range", got)
	}
	for i := 0; i < 3; i++ {
		enabled(170, true)
		updateComposerScrollbar()
		if call(user.NewProc("IsWindowEnabled"), bar) != 0 {
			t.Fatal("blank scrollbar was re-enabled by layout")
		}
	}
	set(edit, strings.Repeat("line\r\n", 100))
	enabled(170, false)
	if call(user.NewProc("IsWindowEnabled"), bar) != 0 {
		t.Fatal("disabled input left scrollbar enabled")
	}
	enabled(170, true)
	if call(user.NewProc("IsWindowEnabled"), bar) == 0 {
		t.Fatal("scrollable input did not restore scrollbar")
	}
}
