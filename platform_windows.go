package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var moveFileEx = kernel.NewProc("MoveFileExW")
var getDiskFree = kernel.NewProc("GetDiskFreeSpaceExW")
var dialogs = syscall.NewLazyDLL("comdlg32.dll")
var getOpenFileName = dialogs.NewProc("GetOpenFileNameW")
var dialogError = dialogs.NewProc("CommDlgExtendedError")
var shell = syscall.NewLazyDLL("shell32.dll")
var browseFolder = shell.NewProc("SHBrowseForFolderW")
var pathFromIDList = shell.NewProc("SHGetPathFromIDListW")
var ole = syscall.NewLazyDLL("ole32.dll")
var coInitialize = ole.NewProc("CoInitializeEx")
var coUninitialize = ole.NewProc("CoUninitialize")
var taskMemFree = ole.NewProc("CoTaskMemFree")

func moveNoReplace(old, new string) error {
	a, e := syscall.UTF16PtrFromString(old)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(new)
	if e != nil {
		return e
	}
	ok, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 8)
	if ok == 0 {
		return &os.LinkError{Op: "rename", Old: old, New: new, Err: err}
	}
	return nil
}
func freeSpace(path string) (uint64, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var free, total, all uint64
	ok, _, err := getDiskFree.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&all)))
	if ok == 0 {
		return 0, err
	}
	return free, nil
}

// OPENFILENAMEW uses pointer-sized fields for both Win32 and Win64.
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExt                   *uint16
	CustomData, Hook             uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	ReservedFlags, FlagsEx       uint32
}

func pickFiles() ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buf := make([]uint16, 65536)
	title, _ := syscall.UTF16PtrFromString("邻传 - 选择要发送的文件")
	filter := utf16.Encode([]rune("所有文件\x00*.*\x00\x00"))
	ofn := openFileName{Owner: guiMain, File: &buf[0], MaxFile: uint32(len(buf)), Title: title, Filter: &filter[0], Flags: 0x00080000 | 0x00000200 | 0x00000800 | 0x00001000 | 0x00000008 | 0x02000000}
	ofn.Size = uint32(unsafe.Sizeof(ofn))
	ok, _, _ := getOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		code, _, _ := dialogError.Call()
		if code == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("文件选择窗口错误: 0x%x", code)
	}
	var parts []string
	start := 0
	for i, ch := range buf {
		if ch == 0 {
			if i == start {
				break
			}
			parts = append(parts, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts, nil
	}
	var paths []string
	for _, name := range parts[1:] {
		paths = append(paths, filepath.Join(parts[0], name))
	}
	return paths, nil
}

type browseInfo struct {
	Owner, Root        uintptr
	DisplayName, Title *uint16
	Flags              uint32
	Callback, Param    uintptr
	Image              int32
}

func pickFolder() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitialize.Call(0, 2)
	if int32(hr) < 0 {
		return "", fmt.Errorf("无法初始化目录窗口: 0x%x", hr)
	}
	defer coUninitialize.Call()
	title, _ := syscall.UTF16PtrFromString("邻传 - 选择接收文件的保存位置")
	display := make([]uint16, 260)
	bi := browseInfo{Owner: guiMain, DisplayName: &display[0], Title: title, Flags: 0x1 | 0x40}
	pidl, _, _ := browseFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil
	}
	defer taskMemFree.Call(pidl)
	path := make([]uint16, 32768)
	ok, _, _ := pathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if ok == 0 {
		return "", fmt.Errorf("请选择本地文件夹")
	}
	return syscall.UTF16ToString(path), nil
}
