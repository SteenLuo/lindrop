package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

var bufferTestDraws int

func bufferTestProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x2b {
		bufferTestDraws++
		paintItem((*drawItem)(unsafe.Pointer(l)))
		return 1
	}
	if m == 0x2c {
		p := (*struct {
			Type, ID, ItemID, Width, Height uint32
			Data                            uintptr
		})(unsafe.Pointer(l))
		p.Height = 116
		return 1
	}
	return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
}
func TestBufferedListPrintsOwnerDrawAndHoverStaysLocal(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	oldNode, oldTasks, oldParts, oldScale := guiNode, guiTasks, chatParts, guiScale
	oldList := guiControls[126]
	defer func() {
		guiNode = oldNode
		guiTasks = oldTasks
		chatParts = oldParts
		guiScale = oldScale
		guiControls[126] = oldList
		chatHoverID = ""
	}()
	guiScale = 1
	guiNode = &Node{id: "self"}
	guiTasks = []Task{{ID: "image", Media: "image", Direction: "in", Status: "waiting", Title: "image.png"}, {ID: "words", Kind: "text", Direction: "in", Status: "done"}}
	chatTextCache["words"] = "局部复制按钮"
	defer delete(chatTextCache, "words")
	chatParts = nil
	name := wp("LinDropBufferTest")
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(bufferTestProc), Name: name}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	parent := call(createWindow, 0, uintptr(unsafe.Pointer(name)), 0, 0x80000000, 0, 0, 700, 400, 0, 0, 0, 0)
	if parent == 0 {
		t.Fatal("parent")
	}
	defer destroyWindow.Call(parent)
	h := call(createWindow, 0, uintptr(unsafe.Pointer(wp("LISTBOX"))), 0, 0x50002061, 0, 0, 650, 300, parent, 126, 0, 0)
	if h == 0 {
		t.Fatal("list")
	}
	guiControls[126] = h
	guiListProc[h] = call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffffc), syscall.NewCallback(listProc))
	defer delete(guiListProc, h)
	for i := range guiTasks {
		msg(h, 0x180, 0, uintptr(unsafe.Pointer(wp("row"))))
		msg(h, 0x1a0, uintptr(i), 116)
	}
	showWindow.Call(parent, 4)
	call(user.NewProc("UpdateWindow"), parent)
	dc := call(user.NewProc("GetDC"), h)
	defer call(user.NewProc("ReleaseDC"), h, dc)
	bufferTestDraws = 0
	bufferDC(dc, 650, 300, 0, 0, func(mem uintptr) {
		fill(mem, rect{0, 0, 650, 300}, canvas)
		call(user.NewProc("CallWindowProcW"), guiListProc[h], h, 0x318, mem, 0xc)
		if pixel := call(gdi.NewProc("GetPixel"), mem, 80, 30); pixel == canvas || pixel == 0xffffffff {
			t.Fatalf("buffered LISTBOX is blank: %x", pixel)
		}
	})
	if bufferTestDraws < 2 {
		t.Fatal("WM_PRINTCLIENT omitted owner drawn rows", bufferTestDraws)
	}
	call(user.NewProc("ValidateRect"), h, 0)
	trackChatHover(h, uintptr(80|(30<<16)))
	var update rect
	if chatHoverID != "" || call(user.NewProc("GetUpdateRect"), h, uintptr(unsafe.Pointer(&update)), 0) != 0 {
		t.Fatal("image hover invalidated list")
	}
	trackChatHover(h, uintptr(80|(145<<16)))
	if chatHoverID != "words" || call(user.NewProc("GetUpdateRect"), h, uintptr(unsafe.Pointer(&update)), 0) == 0 || update.T < 116 || update.B > 232 {
		t.Fatal("text hover did not stay in its own row", chatHoverID, update)
	}
	// A media draw with a negative offset cannot write beyond its row bitmap.
	bufferDC(dc, 650, 300, 0, 0, func(outer uintptr) {
		fill(outer, rect{0, 0, 650, 300}, warning)
		bufferDC(outer, 100, 30, 0, 100, func(mem uintptr) { fill(mem, rect{-500, -500, 1000, 1000}, paper) })
		if call(gdi.NewProc("GetPixel"), outer, 50, 99) != warning || call(gdi.NewProc("GetPixel"), outer, 50, 130) != warning || call(gdi.NewProc("GetPixel"), outer, 50, 110) != paper {
			t.Fatal("row buffer escaped its bounds")
		}
	})
}
