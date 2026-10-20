package main

import (
	"errors"
	"runtime"
	"unsafe"
)

func revealInFolder(folder, selected string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr := call(coInitialize, 0, 2)
	if int32(hr) >= 0 {
		defer call(coUninitialize)
	} else if uint32(hr) != 0x80010106 {
		return errors.New("无法初始化文件夹窗口")
	}
	var parent, item uintptr
	parse := shell.NewProc("SHParseDisplayName")
	if int32(call(parse, uintptr(unsafe.Pointer(wp(folder))), 0, uintptr(unsafe.Pointer(&parent)), 0, 0)) < 0 {
		return errors.New("无法定位所在文件夹")
	}
	defer call(taskMemFree, parent)
	var child uintptr
	if selected != "" && int32(call(parse, uintptr(unsafe.Pointer(wp(selected))), 0, uintptr(unsafe.Pointer(&item)), 0, 0)) >= 0 {
		defer call(taskMemFree, item)
		child = call(shell.NewProc("ILFindLastID"), item)
	}
	open := shell.NewProc("SHOpenFolderAndSelectItems")
	if child != 0 {
		hr = call(open, parent, 1, uintptr(unsafe.Pointer(&child)), 0)
	} else {
		hr = call(open, parent, 0, 0, 0)
	}
	if int32(hr) < 0 {
		return errors.New("无法打开所在文件夹")
	}
	return nil
}
