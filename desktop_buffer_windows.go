package main

import "unsafe"

// Every GDI+ operation stays inside an offscreen bitmap. In particular, media
// rows can draw with negative offsets without spilling into adjacent windows.
func bufferDC(target uintptr, width, height, x, y int, paint func(uintptr)) {
	dc := call(gdi.NewProc("CreateCompatibleDC"), target)
	bmp := call(gdi.NewProc("CreateCompatibleBitmap"), target, uintptr(width), uintptr(height))
	if dc == 0 || bmp == 0 {
		if dc != 0 {
			call(gdi.NewProc("DeleteDC"), dc)
		}
		if bmp != 0 {
			call(gdi.NewProc("DeleteObject"), bmp)
		}
		saved := call(gdi.NewProc("SaveDC"), target)
		call(gdi.NewProc("SetViewportOrgEx"), target, uintptr(x), uintptr(y), 0)
		call(gdi.NewProc("IntersectClipRect"), target, 0, 0, uintptr(width), uintptr(height))
		paint(target)
		call(gdi.NewProc("RestoreDC"), target, saved)
		return
	}
	old := call(gdi.NewProc("SelectObject"), dc, bmp)
	paint(dc)
	call(gdi.NewProc("BitBlt"), target, uintptr(x), uintptr(y), uintptr(width), uintptr(height), dc, 0, 0, 0xcc0020)
	call(gdi.NewProc("SelectObject"), dc, old)
	call(gdi.NewProc("DeleteObject"), bmp)
	call(gdi.NewProc("DeleteDC"), dc)
}

func paintBufferedList(hwnd uintptr) {
	var ps paintStruct
	dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	defer call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	var client rect
	call(user.NewProc("GetClientRect"), hwnd, uintptr(unsafe.Pointer(&client)))
	if client.R <= 0 || client.B <= 0 {
		return
	}
	bufferDC(dc, int(client.R), int(client.B), 0, 0, func(mem uintptr) {
		bg := canvas
		if hwnd == guiControls[111] {
			bg = paper
		}
		fill(mem, client, bg)
		call(user.NewProc("CallWindowProcW"), guiListProc[hwnd], hwnd, 0x318, mem, 0xc)
	})
}

func invalidateChatCopy(id string) {
	if id == "" {
		return
	}
	h := guiControls[126]
	for i, t := range guiTasks {
		if t.ID != id || t.Kind != "text" {
			continue
		}
		if p := chatPartAt(i); p != nil && !p.Last {
			continue
		}
		var row rect
		if int32(msg(h, 0x198, uintptr(i), uintptr(unsafe.Pointer(&row)))) != -1 {
			call(user.NewProc("InvalidateRect"), h, uintptr(unsafe.Pointer(&row)), 0)
		}
	}
}
