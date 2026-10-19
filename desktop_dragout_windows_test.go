package main

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func comTestCall(p uintptr, index int, args ...uintptr) uintptr {
	v := *(*uintptr)(unsafe.Pointer(p))
	fn := *(*uintptr)(unsafe.Pointer(v + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{p}, args...)...)
	return r
}
func TestDragDataObjectUnicodeEnumerationOwnership(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if int32(call(oleDrag.NewProc("OleInitialize"), 0)) < 0 {
		t.Fatal("OLE initialization")
	}
	defer call(oleDrag.NewProc("OleUninitialize"))
	paths := []string{filepath.Join(t.TempDir(), "图片😀.png"), `C:\` + strings.Repeat("长路径\\", 90) + "视频.mp4"}
	o := newDragObject(paths, false)
	p := uintptr(unsafe.Pointer(o))
	defer dragRelease(p)
	var got uintptr
	iid := syscall.GUID{Data1: 0x10e, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	if comTestCall(p, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&got))) != 0 || got != p {
		t.Fatal("QueryInterface")
	}
	comTestCall(got, 2)
	f := dragFormats()[0]
	var medium dragMedium
	for i := 0; i < 3; i++ {
		if comTestCall(p, 3, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&medium))) != 0 {
			t.Fatal("GetData")
		}
		actual, e := desktopDropPaths(medium.Handle)
		if e != nil || !reflect.DeepEqual(actual, paths) {
			t.Fatal("Unicode/long path round trip", actual, e)
		}
		call(oleDrag.NewProc("ReleaseStgMedium"), uintptr(unsafe.Pointer(&medium)))
	}
	f.Tymed = 4
	if comTestCall(p, 5, uintptr(unsafe.Pointer(&f))) != 0x80040069 {
		t.Fatal("unsupported TYMED accepted")
	}
	var enum uintptr
	if comTestCall(p, 8, 1, uintptr(unsafe.Pointer(&enum))) != 0 {
		t.Fatal("EnumFormatEtc")
	}
	defer comTestCall(enum, 2)
	var forms [2]dragFormat
	var count uint32
	if comTestCall(enum, 3, 2, uintptr(unsafe.Pointer(&forms[0])), uintptr(unsafe.Pointer(&count))) != 0 || count != 2 || forms[0].Format != 15 || forms[1].Format != dragEffectFormat {
		t.Fatal("enum formats")
	}
	comTestCall(enum, 5)
	var clone uintptr
	if comTestCall(enum, 6, uintptr(unsafe.Pointer(&clone))) != 0 {
		t.Fatal("enum Clone")
	}
	defer comTestCall(clone, 2)
	if comTestCall(clone, 3, 1, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&count))) != 0 || f.Format != 15 {
		t.Fatal("clone position")
	}
	src := newDragObject(nil, true)
	sp := uintptr(unsafe.Pointer(src))
	if comTestCall(sp, 3, 1, 1) != 0x40101 || comTestCall(sp, 3, 0, 0) != 0x40100 || comTestCall(sp, 3, 0, 1) != 0 || comTestCall(sp, 4, 1) != 0x40102 {
		t.Fatal("drop/cancel/cursor semantics")
	}
	if dragRelease(sp) != 0 {
		t.Fatal("source leaked")
	}
	dragRoots.Lock()
	_, exists := dragRoots.objects[sp]
	dragRoots.Unlock()
	if exists {
		t.Fatal("source still rooted after Release")
	}
	runtime.KeepAlive(o)
	runtime.KeepAlive(src)
}
