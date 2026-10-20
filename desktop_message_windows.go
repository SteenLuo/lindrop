package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var guiTextWindow, guiTextEdit uintptr
var guiTextButtons []uintptr
var guiTextDraft, guiTextContent, guiTextError, guiTextTarget, guiTextTargetName string
var guiTextReadOnly bool

func taskBadge(t Task) string {
	if t.Kind == "text" {
		return "文字"
	}
	return fileBadge(t.Title, t.Folder)
}
func taskDisplayTitle(t Task) string {
	if t.Kind == "text" {
		return "文字消息 · " + time.Unix(t.Created, 0).Format("01-02 15:04")
	}
	return t.Title
}

func showTextComposer() {
	d, ok := currentDevice()
	if e := desktopSendTarget(d, ok, guiNode.id); e != nil {
		showError(e.Error())
		return
	}
	guiTextTarget, guiTextTargetName = d.ID, deviceName(d)
	openTextWindow(guiTextDraft, false)
}
func showTaskText(t Task) {
	s, e := guiNode.ReadTaskText(t.ID)
	if e != nil {
		showError(e.Error())
		return
	}
	guiTextTargetName = taskPeer(t)
	openTextWindow(s, true)
}

func openTextWindow(content string, readOnly bool) {
	if guiTextWindow != 0 {
		call(user.NewProc("SetForegroundWindow"), guiTextWindow)
		return
	}
	hideQR()
	guiTextReadOnly, guiTextContent, guiTextError = readOnly, content, ""
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(textWindowProc), Instance: instance, Icon: guiAppIcon, SmallIcon: guiSmallIcon, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Background: guiWhiteBrush, Name: wp("LinNodeText")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	var parent rect
	call(user.NewProc("GetWindowRect"), guiMain, uintptr(unsafe.Pointer(&parent)))
	x := parent.L + (parent.R-parent.L-int32(sc(580)))/2
	y := parent.T + (parent.B-parent.T-int32(sc(535)))/2
	title := "邻传 · 发送文字"
	if readOnly {
		title = "邻传 · 查看文字"
	}
	guiTextWindow = call(createWindow, 0, uintptr(unsafe.Pointer(wc.Name)), uintptr(unsafe.Pointer(wp(title))), 0x00ca0000, uintptr(x), uintptr(y), uintptr(sc(580)), uintptr(sc(535)), guiMain, 0, instance, 0)
	if guiTextWindow == 0 {
		showError("无法打开文字窗口")
		return
	}
	setDesktopWindowIcons(guiTextWindow)
	style := uintptr(0x50010000 | 0x00200000 | 4 | 0x40 | 0x1000)
	if readOnly {
		style |= 0x800
	}
	display := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")
	guiTextEdit = call(createWindow, 0, uintptr(unsafe.Pointer(wp("EDIT"))), uintptr(unsafe.Pointer(wp(display))), style, uintptr(sc(38)), uintptr(sc(114)), uintptr(sc(504)), uintptr(sc(252)), guiTextWindow, 160, instance, 0)
	msg(guiTextEdit, 0x30, guiFont, 1)
	msg(guiTextEdit, 0xc5, maxTextBytes, 0)
	for _, b := range []struct {
		id   int
		name string
		x    int
	}{{161, "取消", 310}, {162, "发送文字", 438}} {
		if readOnly {
			if b.id == 161 {
				b.name = "关闭"
			} else {
				b.id, b.name = 163, "复制全文"
			}
		}
		h := call(createWindow, 0, uintptr(unsafe.Pointer(wp("BUTTON"))), uintptr(unsafe.Pointer(wp(b.name))), 0x5001000b, uintptr(sc(b.x)), uintptr(sc(449)), uintptr(sc(116)), uintptr(sc(37)), guiTextWindow, uintptr(b.id), instance, 0)
		msg(h, 0x30, guiFont, 1)
		guiButtonProc[h] = setWindowProcedure(h, syscall.NewCallback(buttonProc))
		guiTextButtons = append(guiTextButtons, h)
	}
	call(user.NewProc("EnableWindow"), guiMain, 0)
	showWindow.Call(guiTextWindow, 1)
	call(user.NewProc("SetForegroundWindow"), guiTextWindow)
	call(user.NewProc("SetFocus"), guiTextEdit)
}

func sendTextFromWindow() {
	s := text(guiTextEdit)
	if e := validateText(s); e != nil {
		guiTextError = e.Error()
		invalidate(guiTextWindow)
		return
	}
	if _, e := guiNode.SendText(s, guiTextTarget); e != nil {
		guiTextError = e.Error()
		invalidate(guiTextWindow)
		return
	}
	guiTextDraft = ""
	guiTextReadOnly = true // Do not restore the sent message as an unsent draft.
	destroyWindow.Call(guiTextWindow)
	guiPage, guiFilter = 0, 0
	guiLastTasks = ""
	refreshGUI()
	layout()
}

func copyDesktopText(s string) error {
	words, e := syscall.UTF16FromString(s)
	if e != nil {
		return e
	}
	owner := guiTextWindow
	if owner == 0 {
		owner = guiMain
	}
	if call(user.NewProc("OpenClipboard"), owner) == 0 {
		return errors.New("剪贴板正忙，请稍后再试")
	}
	defer call(user.NewProc("CloseClipboard"))
	memory := call(kernel.NewProc("GlobalAlloc"), 2, uintptr(len(words)*2))
	if memory == 0 {
		return errors.New("无法分配剪贴板内存")
	}
	ptr := call(kernel.NewProc("GlobalLock"), memory)
	if ptr == 0 {
		call(kernel.NewProc("GlobalFree"), memory)
		return errors.New("无法写入剪贴板")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(words)), words)
	call(kernel.NewProc("GlobalUnlock"), memory)
	if call(user.NewProc("EmptyClipboard")) == 0 || call(user.NewProc("SetClipboardData"), 13, memory) == 0 {
		call(kernel.NewProc("GlobalFree"), memory)
		return errors.New("复制失败，请重试")
	}
	return nil // The clipboard owns memory after SetClipboardData succeeds.
}

func textWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x10:
		destroyWindow.Call(hwnd)
		return 0
	case 0x2:
		if !guiTextReadOnly {
			guiTextDraft = text(guiTextEdit)
		}
		for _, h := range guiTextButtons {
			delete(guiButtonProc, h)
		}
		guiTextButtons = nil
		guiTextWindow, guiTextEdit = 0, 0
		call(user.NewProc("EnableWindow"), guiMain, 1)
		call(user.NewProc("SetForegroundWindow"), guiMain)
		return 0
	case 0x111:
		id := int(w & 0xffff)
		switch id {
		case 2, 161:
			destroyWindow.Call(hwnd)
		case 162:
			sendTextFromWindow()
		case 163:
			if e := copyDesktopText(guiTextContent); e != nil {
				guiTextError = e.Error()
			} else {
				guiTextError = "已复制全文"
			}
			invalidate(hwnd)
		case 160:
			if (w>>16)&0xffff == 0x300 {
				guiTextError = ""
				invalidate(hwnd)
			}
		}
		return 0
	case 0x2b:
		paintButton((*drawItem)(unsafe.Pointer(l)))
		return 1
	case 0x133, 0x138:
		call(gdi.NewProc("SetTextColor"), w, ink)
		call(gdi.NewProc("SetBkColor"), w, paper)
		return guiWhiteBrush
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		title, sub := "发送文字", "发送给 "+guiTextTargetName
		if guiTextReadOnly {
			title, sub = "查看文字", guiTextTargetName
		}
		label(dc, title, 26, 22, 528, 30, ink, guiTitle)
		label(dc, sub, 26, 65, 528, 20, muted, guiSmall)
		rounded(dc, rct(26, 102, 528, 278), paper, lineColor, 10)
		hint := "保留换行 · 最多 64 KB · Ctrl+Enter 发送"
		if guiTextReadOnly {
			hint = "纯文本内容 · 点击复制全文，也可选择部分文字复制"
		}
		label(dc, hint, 26, 390, 528, 19, muted, guiSmall)
		if guiTextError != "" {
			label(dc, guiTextError, 26, 414, 528, 23, warning, guiSmall)
		} else if !guiTextReadOnly {
			label(dc, fmt.Sprintf("%s / 64 KB", formatSize(int64(len(text(guiTextEdit))))), 26, 414, 528, 23, muted, guiSmall)
		}
		call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), hwnd, uintptr(m), w, l)
}
