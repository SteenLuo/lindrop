package main

import (
	"syscall"
	"time"
	"unsafe"
)

var guiQRVisible, guiQRKeyboard bool
var guiQROutside time.Time

func phonePopoverRect(anchor, work rect, width, height, gap int32) rect {
	if width > work.R-work.L {
		width = work.R - work.L
	}
	if height > work.B-work.T {
		height = work.B - work.T
	}
	x, y := anchor.R+gap, anchor.B-height
	if x+width > work.R {
		x = anchor.L - gap - width
	}
	if y+height > work.B {
		y = work.B - height
	}
	if x < work.L {
		x = work.L
	}
	if x+width > work.R {
		x = work.R - width
	}
	if y < work.T {
		y = work.T
	}
	return rect{x, y, x + width, y + height}
}

func phonePopoverContains(p point, anchor, panel rect, padding int32, keyboardFocus bool) bool {
	inside := func(r rect) bool {
		return p.X >= r.L-padding && p.X < r.R+padding && p.Y >= r.T-padding && p.Y < r.B+padding
	}
	return keyboardFocus || inside(anchor) || inside(panel)
}

func hideQR() {
	if qrWindow != 0 {
		call(user.NewProc("KillTimer"), qrWindow, 1)
		showWindow.Call(qrWindow, 0)
	}
	guiQRVisible, guiQRKeyboard = false, false
	guiQROutside = time.Time{}
}

func destroyQR() {
	if qrWindow != 0 {
		destroyWindow.Call(qrWindow)
		qrWindow = 0
	}
	if qrBitmap != 0 {
		call(gdi.NewProc("DeleteObject"), qrBitmap)
		qrBitmap = 0
	}
	guiQRVisible = false
}

func showQR() { showPhonePopover(guiKeyboardMode) }

func showPhonePopover(keyboard bool) {
	if guiConnect != 0 || guiControls[103] == 0 || !createQRBitmap() {
		return
	}
	if qrWindow == 0 {
		instance := call(kernel.NewProc("GetModuleHandleW"), 0)
		wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Style: 0x20000, Proc: syscall.NewCallback(qrProc), Instance: instance, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Background: guiWhiteBrush, Name: wp("LinNodePhonePopover")}
		call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
		// Tool window + no-activate popup: no taskbar item, title bar or focus theft.
		qrWindow = call(createWindow, 0x08000080, uintptr(unsafe.Pointer(wc.Name)), uintptr(unsafe.Pointer(wp("邻传连接信息"))), 0x80000000, 0, 0, uintptr(sc(320)), uintptr(sc(438)), guiMain, 0, instance, 0)
		if qrWindow == 0 {
			return
		}
		region := call(gdi.NewProc("CreateRoundRectRgn"), 0, 0, uintptr(sc(320)+1), uintptr(sc(438)+1), uintptr(sc(26)), uintptr(sc(26)))
		if region != 0 && call(user.NewProc("SetWindowRgn"), qrWindow, region, 0) == 0 {
			call(gdi.NewProc("DeleteObject"), region)
		}
	}
	var anchor rect
	call(user.NewProc("GetWindowRect"), guiControls[103], uintptr(unsafe.Pointer(&anchor)))
	monitor := call(user.NewProc("MonitorFromWindow"), guiControls[103], 2)
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{Size: 40}
	if call(user.NewProc("GetMonitorInfoW"), monitor, uintptr(unsafe.Pointer(&info))) == 0 {
		call(user.NewProc("SystemParametersInfoW"), 0x30, 0, uintptr(unsafe.Pointer(&info.Work)), 0)
	}
	r := phonePopoverRect(anchor, info.Work, int32(sc(320)), int32(sc(438)), int32(sc(8)))
	guiQRKeyboard = keyboard
	guiQROutside = time.Time{}
	guiQRVisible = true
	call(user.NewProc("SetWindowPos"), qrWindow, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x10|0x40)
	call(user.NewProc("SetTimer"), qrWindow, 1, 100, 0)
	invalidate(qrWindow)
}

func qrProc(hwnd uintptr, message uint32, w, l uintptr) uintptr {
	switch message {
	case 0x10:
		hideQR()
		return 0
	case 0x21:
		return 3 // MA_NOACTIVATE
	case 0x14:
		return 1
	case 0x113:
		var p point
		var anchor, panel rect
		call(user.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&p)))
		call(user.NewProc("GetWindowRect"), guiControls[103], uintptr(unsafe.Pointer(&anchor)))
		call(user.NewProc("GetWindowRect"), hwnd, uintptr(unsafe.Pointer(&panel)))
		focused := guiQRKeyboard && call(user.NewProc("GetFocus")) == guiControls[103]
		if phonePopoverContains(p, anchor, panel, int32(sc(8)), focused) {
			guiQROutside = time.Time{}
		} else if guiQROutside.IsZero() {
			guiQROutside = time.Now()
		} else if time.Since(guiQROutside) >= 250*time.Millisecond {
			hideQR()
		}
		return 0
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		mem := call(gdi.NewProc("CreateCompatibleDC"), dc)
		bmp := call(gdi.NewProc("CreateCompatibleBitmap"), dc, uintptr(sc(320)), uintptr(sc(438)))
		old := call(gdi.NewProc("SelectObject"), mem, bmp)
		fill(mem, rct(0, 0, 320, 438), paper)
		rounded(mem, rct(0, 0, 320, 438), paper, lineColor, 13)
		label(mem, "手机与电脑连接", 18, 14, 284, 28, ink, guiTitle)
		label(mem, "手机扫码进入 · 电脑仅需下方地址", 18, 46, 284, 20, muted, guiSmall)
		imageDC := call(gdi.NewProc("CreateCompatibleDC"), dc)
		oldImage := call(gdi.NewProc("SelectObject"), imageDC, qrBitmap)
		call(gdi.NewProc("BitBlt"), mem, uintptr(sc(50)), uintptr(sc(74)), uintptr(sc(220)), uintptr(sc(220)), imageDC, 0, 0, 0x00cc0020)
		call(gdi.NewProc("SelectObject"), imageDC, oldImage)
		call(gdi.NewProc("DeleteDC"), imageDC)
		stroke(mem, 18, 304, 302, 304, lineColor, 1)
		label(mem, "电脑连接地址", 18, 313, 284, 17, muted, guiSmall)
		label(mem, guiNode.addr, 18, 332, 284, 24, ink, fontSize(15, 600))
		label(mem, "手机手动进入备用码", 18, 371, 135, 23, muted, guiSmall)
		rightLabel(mem, guiNode.code, rct(160, 361, 142, 38), accent, fontSize(25, 600))
		label(mem, "鼠标移开自动收起 · Esc 可关闭", 18, 410, 284, 16, muted, guiTiny)
		call(gdi.NewProc("BitBlt"), dc, 0, 0, uintptr(sc(320)), uintptr(sc(438)), mem, 0, 0, 0x00cc0020)
		call(gdi.NewProc("SelectObject"), mem, old)
		call(gdi.NewProc("DeleteObject"), bmp)
		call(gdi.NewProc("DeleteDC"), mem)
		call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), hwnd, uintptr(message), w, l)
}
