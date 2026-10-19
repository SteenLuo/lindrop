package main

import (
	"strings"
	"syscall"
	"unsafe"
)

func requestTitle(t Task) string {
	if t.Kind == "text" {
		return t.SenderName + "想发送一段文字"
	}
	s := t.SenderName + "想发送文件"
	if t.Folder {
		return t.SenderName + "想发送文件夹"
	}
	if strings.Contains(t.Title, ".") {
		return t.SenderName + "想发送 1 个文件"
	}
	return s
}
func requestRejectOffset(t Task, width int) int {
	dc := call(user.NewProc("GetDC"), guiMain)
	defer call(user.NewProc("ReleaseDC"), guiMain, dc)
	old := call(gdi.NewProc("SelectObject"), dc, fontSize(11, 500))
	defer call(gdi.NewProc("SelectObject"), dc, old)
	words, _ := syscall.UTF16FromString(strings.ReplaceAll(requestTitle(t), "\x00", ""))
	if len(words) == 0 {
		return 220
	}
	var size point
	call(gdi.NewProc("GetTextExtentPoint32W"), dc, uintptr(unsafe.Pointer(&words[0])), uintptr(len(words)-1), uintptr(unsafe.Pointer(&size)))
	x := 71 + int(float64(size.X)/guiScale)
	if x < 220 {
		x = 220
	}
	if x > width-120 {
		x = width - 120
	}
	return x
}

var guiKeyboardMode bool
var guiFontFamily = "Microsoft YaHei UI"

func selectDesktopFontFace() {
	dc := call(user.NewProc("GetDC"), 0)
	f := newFont(12, 400)
	old := call(gdi.NewProc("SelectObject"), dc, f)
	var face [96]uint16
	call(gdi.NewProc("GetTextFaceW"), dc, uintptr(len(face)), uintptr(unsafe.Pointer(&face[0])))
	call(gdi.NewProc("SelectObject"), dc, old)
	call(gdi.NewProc("DeleteObject"), f)
	call(user.NewProc("ReleaseDC"), 0, dc)
	name := stringFromUTF16(face[:])
	if !strings.EqualFold(name, "Microsoft YaHei UI") {
		guiFontFamily = "Microsoft YaHei"
	}
}
func stringFromUTF16(v []uint16) string {
	var runes []rune
	for _, c := range v {
		if c == 0 {
			break
		}
		runes = append(runes, rune(c))
	}
	return string(runes)
}
func rightLabel(dc uintptr, s string, r rect, c, font uintptr) {
	if vectorText(dc, s, r, c, font, 2) {
		return
	}
	call(gdi.NewProc("SetTextColor"), dc, c)
	call(gdi.NewProc("SetBkMode"), dc, 1)
	call(gdi.NewProc("SelectObject"), dc, font)
	call(user.NewProc("DrawTextW"), dc, uintptr(unsafe.Pointer(wp(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x20|0x800|0x4|2)
}
func taskRowHeight(t Task) int {
	return chatRowHeight(t)
}

func taskContextMenu(hwnd uintptr) {
	t, ok := currentTask()
	if !ok {
		return
	}
	menu := call(user.NewProc("CreatePopupMenu"))
	defer call(user.NewProc("DestroyMenu"), menu)
	if t.Kind != "text" && (t.Direction == "out" || t.Status == "done") {
		call(user.NewProc("AppendMenuW"), menu, 0, 172, uintptr(unsafe.Pointer(wp("复制文件  Ctrl+C"))))
	}
	if t.Kind == "text" && chatBody(t) != "" {
		for _, v := range []struct {
			id   int
			name string
		}{{172, "复制文字  Ctrl+C"}, {174, "选择文字"}, {173, "展开 / 收起"}} {
			call(user.NewProc("AppendMenuW"), menu, 0, uintptr(v.id), uintptr(unsafe.Pointer(wp(v.name))))
		}
	} else if t.Status == "waiting" {
		for _, v := range []struct {
			id   int
			name string
		}{{127, "接收"}, {128, "接收并信任此设备"}, {129, "拒绝"}} {
			call(user.NewProc("AppendMenuW"), menu, 0, uintptr(v.id), uintptr(unsafe.Pointer(wp(v.name))))
		}
	} else if id, name := taskAction(t); id != 0 {
		call(user.NewProc("AppendMenuW"), menu, 0, uintptr(id), uintptr(unsafe.Pointer(wp(name))))
	} else if t.Kind == "text" || (t.Direction != "out" && t.Status != "done") {
		return
	}
	var p point
	call(user.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&p)))
	selected := call(user.NewProc("TrackPopupMenu"), menu, 0x102, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	if selected != 0 {
		doCommand(int(selected), 0)
	}
}
