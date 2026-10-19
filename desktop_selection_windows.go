package main

import (
	"strings"
	"syscall"
	"unsafe"
)

var chatSelectEdit, chatSelectProc uintptr

func closeChatSelection() {
	if chatSelectEdit != 0 {
		h := chatSelectEdit
		chatSelectEdit = 0
		destroyWindow.Call(h)
	}
}
func showChatSelection() {
	t, ok := currentTask()
	if !ok || t.Kind != "text" || chatBody(t) == "" {
		return
	}
	closeChatSelection()
	h := guiControls[126]
	index := int(int32(msg(h, 0x188, 0, 0)))
	if index < 0 {
		return
	}
	var row, client rect
	msg(h, 0x198, uintptr(index), uintptr(unsafe.Pointer(&row)))
	call(user.NewProc("GetClientRect"), h, uintptr(unsafe.Pointer(&client)))
	b := chatBubblePart(t, chatListWidth(), chatPartAt(index))
	top := int(row.T) + sc(b.Y)
	if top < 0 {
		top = 0
	}
	lines, _ := wrapChatLines(chatBody(t), b.W-20, 0)
	height := sc(len(lines)*21 + 18)
	if height < sc(39) {
		height = sc(39)
	}
	if height > sc(270) {
		height = sc(270)
	}
	if top+height > int(client.B) {
		top = int(client.B) - height
	}
	if top < 0 {
		top = 0
	}
	body := strings.ReplaceAll(strings.ReplaceAll(chatBody(t), "\r\n", "\n"), "\n", "\r\n")
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	style := uintptr(0x50010000 | 0x4 | 0x40 | 0x800)
	if len(lines) > 10 {
		style |= 0x00200000
	}
	chatSelectEdit = call(createWindow, 0, uintptr(unsafe.Pointer(wp("EDIT"))), uintptr(unsafe.Pointer(wp(body))), style, uintptr(sc(b.X)), uintptr(top), uintptr(sc(b.W)), uintptr(height), h, 175, instance, 0)
	if chatSelectEdit == 0 {
		return
	}
	msg(chatSelectEdit, 0x30, guiFont, 1)
	msg(chatSelectEdit, 0xc5, maxTextBytes, 0)
	chatSelectProc = call(user.NewProc("SetWindowLongW"), chatSelectEdit, uintptr(0xfffffffc), syscall.NewCallback(chatSelectionProc))
	call(user.NewProc("SetFocus"), chatSelectEdit)
	msg(chatSelectEdit, 0xb1, 0, ^uintptr(0))
}
func chatSelectionProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x100 && w == 27 {
		closeChatSelection()
		call(user.NewProc("SetFocus"), guiControls[126])
		return 0
	}
	if m == 0x100 && w == 65 && call(user.NewProc("GetKeyState"), 17)&0x8000 != 0 {
		msg(hwnd, 0xb1, 0, ^uintptr(0))
		return 0
	}
	if m == 0x8 {
		queue(func() {
			if chatSelectEdit == hwnd {
				closeChatSelection()
			}
		})
	}
	return call(user.NewProc("CallWindowProcW"), chatSelectProc, hwnd, uintptr(m), w, l)
}
