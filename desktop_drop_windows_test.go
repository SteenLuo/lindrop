package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"
)

func dropPayload(t *testing.T, paths []string) uintptr {
	t.Helper()
	raw := make([]byte, 20)
	binary.LittleEndian.PutUint32(raw, 20)
	binary.LittleEndian.PutUint32(raw[16:], 1)
	for _, c := range utf16.Encode([]rune(strings.Join(paths, string(rune(0))) + string([]rune{0, 0}))) {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], c)
		raw = append(raw, b[:]...)
	}
	h := call(kernel.NewProc("GlobalAlloc"), 2, uintptr(len(raw)))
	if h == 0 {
		t.Fatal("GlobalAlloc")
	}
	p := call(kernel.NewProc("GlobalLock"), h)
	if p == 0 {
		t.Fatal("GlobalLock")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(raw)), raw)
	call(kernel.NewProc("GlobalUnlock"), h)
	t.Cleanup(func() { call(shell.NewProc("DragFinish"), h) })
	return h
}

func TestDesktopUnicodeDropAndDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "中文😀.txt")
	folder := filepath.Join(dir, "素材")
	os.MkdirAll(filepath.Join(folder, "子目录"), 0700)
	os.WriteFile(file, []byte("单文件"), 0600)
	os.WriteFile(filepath.Join(folder, "子目录", "视频片段.txt"), []byte("目录内容"), 0600)
	paths := []string{file, folder}
	got, e := desktopDropPaths(dropPayload(t, paths))
	if e != nil || !reflect.DeepEqual(got, paths) {
		t.Fatalf("drop paths changed: %v", e)
	}
	metas, _, _, e := collect(got)
	if e != nil || len(metas) != 2 {
		t.Fatalf("mixed file/directory collection: %v, %d", e, len(metas))
	}
	long := []string{`C:\` + strings.Repeat("长目录\\", 90) + "文件.txt"}
	got, e = desktopDropPaths(dropPayload(t, long))
	if e != nil || !reflect.DeepEqual(got, long) {
		t.Fatal("long drop path truncated", e)
	}
	if _, e = desktopDropPaths(dropPayload(t, nil)); e == nil {
		t.Fatal("empty drop accepted")
	}
}

func TestDesktopSendTargetAndLongText(t *testing.T) {
	for _, d := range []Device{{}, {ID: "self", Online: true}, {ID: "other", Online: false}} {
		if desktopSendTarget(d, true, "self") == nil {
			t.Fatal("invalid drop target accepted")
		}
	}
	if e := desktopSendTarget(Device{ID: "other", Online: true}, true, "self"); e != nil {
		t.Fatal(e)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	h := call(createWindow, 0, uintptr(unsafe.Pointer(wp("EDIT"))), 0, 0x80000004, 0, 0, 100, 100, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal("create test edit")
	}
	defer destroyWindow.Call(h)
	s := strings.Repeat("文字😀\r\n", 1500)
	msg(h, 0xc5, maxTextBytes, 0)
	set(h, s)
	if got := text(h); got != s {
		t.Fatalf("native long text truncated: %d/%d", len(got), len(s))
	}
	if id, _ := taskAction(Task{Kind: "text", Direction: "in", Status: "done"}); id != 164 {
		t.Fatal("completed text lacks viewer action")
	}
}
