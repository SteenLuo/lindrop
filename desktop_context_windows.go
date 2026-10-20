package main

import (
	"syscall"
	"unsafe"
)

type contextMenuItem struct {
	ID              int
	Label, Shortcut string
}

var guiContextMenu uintptr
var guiContextItems []contextMenuItem
var guiContextTask string
var guiContextHover, guiContextPressed = -1, -1
var guiContextPreviousFocus uintptr
var contextMenuCallback = syscall.NewCallback(contextMenuProc)

const contextMenuWidth = 248
const contextMenuRow = 36
const contextMenuPadding = 6

func contextMenuRect(at point, width, height int32, work rect) rect {
	if width > work.R-work.L {
		width = work.R - work.L
	}
	if height > work.B-work.T {
		height = work.B - work.T
	}
	x, y := at.X, at.Y
	if x+width > work.R {
		x -= width
	}
	if y+height > work.B {
		y -= height
	}
	if x < work.L {
		x = work.L
	}
	if y < work.T {
		y = work.T
	}
	return rect{x, y, x + width, y + height}
}

func createContextMenuWindow(owner uintptr, r rect) uintptr {
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	name := wp("LinDropThemedContextMenu")
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Style: 0x20000, Proc: contextMenuCallback, Instance: instance, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Name: name}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	h := call(createWindow, 0x80, uintptr(unsafe.Pointer(name)), 0, 0x80000000, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), owner, 0, instance, 0)
	if h != 0 {
		region := call(gdi.NewProc("CreateRoundRectRgn"), 0, 0, uintptr(r.R-r.L+1), uintptr(r.B-r.T+1), uintptr(sc(20)), uintptr(sc(20)))
		if region != 0 && call(user.NewProc("SetWindowRgn"), h, region, 0) == 0 {
			call(gdi.NewProc("DeleteObject"), region)
		}
	}
	return h
}

func showThemedContextMenu(owner uintptr, at point, items []contextMenuItem, task string) {
	closeThemedContextMenu(false)
	if len(items) == 0 {
		return
	}
	guiContextPreviousFocus = call(user.NewProc("GetFocus"))
	guiContextItems, guiContextTask = items, task
	guiContextHover, guiContextPressed = -1, -1
	if guiKeyboardMode {
		guiContextHover = 0
	}
	near := rect{at.X, at.Y, at.X + 1, at.Y + 1}
	monitor := call(user.NewProc("MonitorFromRect"), uintptr(unsafe.Pointer(&near)), 2)
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{Size: 40}
	if call(user.NewProc("GetMonitorInfoW"), monitor, uintptr(unsafe.Pointer(&info))) == 0 {
		call(user.NewProc("SystemParametersInfoW"), 0x30, 0, uintptr(unsafe.Pointer(&info.Work)), 0)
	}
	r := contextMenuRect(at, int32(sc(contextMenuWidth)), int32(sc(2*contextMenuPadding+contextMenuRow*len(items))), info.Work)
	guiContextMenu = createContextMenuWindow(owner, r)
	if guiContextMenu == 0 {
		guiContextItems = nil
		return
	}
	showWindow.Call(guiContextMenu, 5)
	call(user.NewProc("SetForegroundWindow"), guiContextMenu)
	call(user.NewProc("SetFocus"), guiContextMenu)
}

func closeThemedContextMenu(restore bool) {
	h := guiContextMenu
	guiContextMenu = 0
	guiContextItems = nil
	guiContextTask = ""
	if h != 0 {
		destroyWindow.Call(h)
	}
	if restore && guiContextPreviousFocus != 0 && call(user.NewProc("IsWindowVisible"), guiMain) != 0 {
		call(user.NewProc("SetFocus"), guiContextPreviousFocus)
	}
}

func invokeContextMenuItem(index int) {
	if index < 0 || index >= len(guiContextItems) {
		return
	}
	id, task := guiContextItems[index].ID, guiContextTask
	closeThemedContextMenu(true)
	if task != "" {
		found := false
		for i, t := range guiTasks {
			if t.ID == task {
				msg(guiControls[126], 0x186, uintptr(i), 0)
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	doCommand(id, 0)
}

func contextMenuHit(x, y int) int {
	if x < sc(contextMenuPadding) || x >= sc(contextMenuWidth-contextMenuPadding) || y < sc(contextMenuPadding) {
		return -1
	}
	i := (y - sc(contextMenuPadding)) / sc(contextMenuRow)
	if i >= len(guiContextItems) {
		return -1
	}
	return i
}

func paintContextMenu(dc uintptr, client rect) {
	fill(dc, client, paper)
	rounded(dc, client, paper, lineColor, 10)
	for i, item := range guiContextItems {
		y := contextMenuPadding + i*contextMenuRow
		if i == guiContextHover {
			rounded(dc, rct(contextMenuPadding, y, contextMenuWidth-2*contextMenuPadding, contextMenuRow), tint, tint, 7)
		}
		label(dc, item.Label, 16, y+8, contextMenuWidth-100, 21, ink, guiFont)
		if item.Shortcut != "" {
			rightLabel(dc, item.Shortcut, rct(contextMenuWidth-88, y+8, 70, 21), muted, guiTiny)
		}
	}
}

func contextMenuProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x14:
		if w != 0 {
			fill(w, chatClient(h), paper)
		}
		return 1
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), h, uintptr(unsafe.Pointer(&ps)))
		r := chatClient(h)
		if r.R > 0 && r.B > 0 {
			bufferDC(dc, int(r.R), int(r.B), 0, 0, func(mem uintptr) { paintContextMenu(mem, r) })
		}
		call(user.NewProc("EndPaint"), h, uintptr(unsafe.Pointer(&ps)))
		return 0
	case 0x87:
		return 0x85
	case 0x200:
		i := contextMenuHit(int(int16(l)), int(int16(l>>16)))
		if i != guiContextHover {
			guiContextHover = i
			invalidate(h)
		}
		track := struct {
			Size, Flags uint32
			Hwnd        uintptr
			Hover       uint32
		}{Flags: 2, Hwnd: h}
		track.Size = uint32(unsafe.Sizeof(track))
		call(user.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&track)))
		return 0
	case 0x2a3:
		guiContextHover = -1
		invalidate(h)
		return 0
	case 0x201:
		guiContextPressed = contextMenuHit(int(int16(l)), int(int16(l>>16)))
		return 0
	case 0x202:
		i := contextMenuHit(int(int16(l)), int(int16(l>>16)))
		if i == guiContextPressed {
			invokeContextMenuItem(i)
		}
		return 0
	case 0x100:
		if len(guiContextItems) == 0 {
			return 0
		}
		switch w {
		case 27, 9:
			closeThemedContextMenu(true)
		case 13:
			invokeContextMenuItem(guiContextHover)
		case 38:
			if guiContextHover < 0 {
				guiContextHover = 0
			}
			guiContextHover = (guiContextHover + len(guiContextItems) - 1) % len(guiContextItems)
			invalidate(h)
		case 40:
			guiContextHover = (guiContextHover + 1) % len(guiContextItems)
			invalidate(h)
		case 36:
			guiContextHover = 0
			invalidate(h)
		case 35:
			guiContextHover = len(guiContextItems) - 1
			invalidate(h)
		case 67:
			if call(user.NewProc("GetKeyState"), 17)&0x8000 != 0 {
				for i, item := range guiContextItems {
					if item.ID == 172 {
						invokeContextMenuItem(i)
						break
					}
				}
			}
		}
		return 0
	case 6:
		if w&0xffff == 0 && guiContextMenu == h {
			closeThemedContextMenu(false)
		}
		return 0
	case 8:
		if guiContextMenu == h {
			closeThemedContextMenu(false)
		}
		return 0
	case 0x10, 0x204:
		closeThemedContextMenu(true)
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
}
