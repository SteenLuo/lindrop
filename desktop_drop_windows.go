package main

import (
	"errors"
	"syscall"
	"unsafe"
)

const wmDropFiles = 0x233

func acceptDesktopDrops(hwnd uintptr) { call(shell.NewProc("DragAcceptFiles"), hwnd, 1) }

func desktopDropPaths(drop uintptr) ([]string, error) {
	count := int(call(shell.NewProc("DragQueryFileW"), drop, 0xffffffff, 0, 0))
	if count < 1 || count > 5000 {
		return nil, errors.New("一次可拖入 1 至 5000 个文件或文件夹")
	}
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		size := int(call(shell.NewProc("DragQueryFileW"), drop, uintptr(i), 0, 0))
		if size < 1 || size > 32767 {
			return nil, errors.New("拖入的文件路径无效或过长")
		}
		buf := make([]uint16, size+1)
		if got := call(shell.NewProc("DragQueryFileW"), drop, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); int(got) != size {
			return nil, errors.New("未能读取完整文件路径，请重新拖入")
		}
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	return paths, nil
}

func desktopSendTarget(d Device, ok bool, self string) error {
	if !ok || !d.Online || d.ID == "" || d.ID == self {
		return errors.New("请先在左侧选择另一台在线设备，再发送文件或文字")
	}
	return nil
}

func sendDesktopFiles(paths []string, target string) error {
	_, e := guiNode.Send(paths, target)
	if e == nil {
		queue(func() {
			guiPage, guiFilter = 0, 0
			guiLastTasks = ""
			refreshGUI()
			layout()
		})
	}
	return e
}

func desktopDrop(drop uintptr) {
	defer call(shell.NewProc("DragFinish"), drop)
	paths, e := desktopDropPaths(drop)
	if e != nil {
		showError(e.Error())
		return
	}
	d, ok := currentDevice()
	if e = desktopSendTarget(d, ok, guiNode.id); e != nil {
		showError(e.Error())
		return
	}
	hideQR()
	set(guiControls[190], "正在准备拖入的文件，发送给 "+deviceName(d))
	work(func() error { return sendDesktopFiles(paths, d.ID) })
}
