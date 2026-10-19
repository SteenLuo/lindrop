package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var oleDrag = syscall.NewLazyDLL("ole32.dll")
var guiDragging, chatDragged bool
var chatDragID string
var chatDragStart point

type dragFormat struct {
	Format uint16
	Target uintptr
	Aspect uint32
	Index  int32
	Tymed  uint32
}
type dragMedium struct {
	Tymed           uint32
	Handle, Release uintptr
}
type dragObject struct {
	Vtable *uintptr
	Refs   int32
	Source bool
	Paths  []string
}

var dragRoots = struct {
	sync.Mutex
	objects map[uintptr]*dragObject
}{objects: map[uintptr]*dragObject{}}
var dragDataVtable = [...]uintptr{
	syscall.NewCallback(dragQuery), syscall.NewCallback(dragAddRef), syscall.NewCallback(dragRelease),
	syscall.NewCallback(dragGetData), syscall.NewCallback(dragGetDataHere), syscall.NewCallback(dragQueryData),
	syscall.NewCallback(dragCanonical), syscall.NewCallback(dragSetData), syscall.NewCallback(dragEnum),
	syscall.NewCallback(dragAdvise), syscall.NewCallback(dragUnadvise), syscall.NewCallback(dragEnumAdvise),
}
var dragSourceVtable = [...]uintptr{
	syscall.NewCallback(dragQuery), syscall.NewCallback(dragAddRef), syscall.NewCallback(dragRelease),
	syscall.NewCallback(dragContinue), syscall.NewCallback(dragFeedback),
}
var dragEffectFormat = uint16(call(user.NewProc("RegisterClipboardFormatW"), uintptr(unsafe.Pointer(wp("Preferred DropEffect")))))

func newDragObject(paths []string, source bool) *dragObject {
	o := &dragObject{Refs: 1, Source: source, Paths: append([]string(nil), paths...)}
	o.Vtable = &dragDataVtable[0]
	if source {
		o.Vtable = &dragSourceVtable[0]
	}
	dragRoots.Lock()
	dragRoots.objects[uintptr(unsafe.Pointer(o))] = o
	dragRoots.Unlock()
	return o
}
func dragQuery(this, iid, out uintptr) uintptr {
	if out == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if iid == 0 {
		return 0x80004003
	}
	o := (*dragObject)(unsafe.Pointer(this))
	g := *(*syscall.GUID)(unsafe.Pointer(iid))
	base := syscall.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	match := g == base
	if o.Source {
		base.Data1 = 0x121
	} else {
		base.Data1 = 0x10e
	}
	if !match && g != base {
		return 0x80004002
	}
	*(*uintptr)(unsafe.Pointer(out)) = this
	dragAddRef(this)
	return 0
}
func dragAddRef(this uintptr) uintptr {
	return uintptr(atomic.AddInt32(&(*dragObject)(unsafe.Pointer(this)).Refs, 1))
}
func dragRelease(this uintptr) uintptr {
	n := atomic.AddInt32(&(*dragObject)(unsafe.Pointer(this)).Refs, -1)
	if n == 0 {
		dragRoots.Lock()
		delete(dragRoots.objects, this)
		dragRoots.Unlock()
	}
	return uintptr(n)
}
func dragQueryData(this, format uintptr) uintptr {
	if format == 0 {
		return 0x80004003
	}
	f := (*dragFormat)(unsafe.Pointer(format))
	if f.Format != 15 && f.Format != dragEffectFormat {
		return 0x80040064
	}
	if f.Aspect != 1 {
		return 0x8004006b
	}
	if f.Index != -1 {
		return 0x80040068
	}
	if f.Tymed&1 == 0 {
		return 0x80040069
	}
	return 0
}
func fileDropBytes(paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("没有可拖出的文件")
	}
	for _, p := range paths {
		if p == "" || strings.ContainsRune(p, 0) {
			return nil, errors.New("文件路径无效")
		}
	}
	raw := make([]byte, 20)
	binary.LittleEndian.PutUint32(raw, 20)
	binary.LittleEndian.PutUint32(raw[16:], 1)
	for _, c := range utf16.Encode([]rune(strings.Join(paths, "\x00") + "\x00\x00")) {
		raw = append(raw, byte(c), byte(c>>8))
	}
	return raw, nil
}
func dragGlobal(raw []byte) uintptr {
	h := call(kernel.NewProc("GlobalAlloc"), 2, uintptr(len(raw)))
	if h == 0 {
		return 0
	}
	p := call(kernel.NewProc("GlobalLock"), h)
	if p == 0 {
		call(kernel.NewProc("GlobalFree"), h)
		return 0
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(raw)), raw)
	call(kernel.NewProc("GlobalUnlock"), h)
	return h
}
func dragGetData(this, format, medium uintptr) uintptr {
	if medium == 0 {
		return 0x80004003
	}
	m := (*dragMedium)(unsafe.Pointer(medium))
	*m = dragMedium{}
	if hr := dragQueryData(this, format); hr != 0 {
		return hr
	}
	raw := []byte{1, 0, 0, 0}
	if (*dragFormat)(unsafe.Pointer(format)).Format == 15 {
		var e error
		raw, e = fileDropBytes((*dragObject)(unsafe.Pointer(this)).Paths)
		if e != nil {
			return 0x80040064
		}
	}
	h := dragGlobal(raw)
	if h == 0 {
		return 0x8007000e
	}
	*m = dragMedium{Tymed: 1, Handle: h}
	return 0
}
func dragGetDataHere(this, format, medium uintptr) uintptr { return 0x80040069 }
func dragCanonical(this, in, out uintptr) uintptr {
	if out == 0 {
		return 0x80004003
	}
	*(*dragFormat)(unsafe.Pointer(out)) = dragFormat{}
	return 0x80004001
}
func dragSetData(this, format, medium, release uintptr) uintptr { return 0x80004001 }
func dragFormats() []dragFormat {
	return []dragFormat{{Format: 15, Aspect: 1, Index: -1, Tymed: 1}, {Format: dragEffectFormat, Aspect: 1, Index: -1, Tymed: 1}}
}
func dragEnum(this, direction, out uintptr) uintptr {
	if out == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if direction != 1 {
		return 0x80004001
	}
	f := dragFormats()
	hr := call(shell.NewProc("SHCreateStdEnumFmtEtc"), uintptr(len(f)), uintptr(unsafe.Pointer(&f[0])), out)
	runtime.KeepAlive(f)
	return hr
}
func dragAdvise(this, format, flags, sink, connection uintptr) uintptr {
	if connection != 0 {
		*(*uint32)(unsafe.Pointer(connection)) = 0
	}
	return 0x80040003
}
func dragUnadvise(this, connection uintptr) uintptr { return 0x80040003 }
func dragEnumAdvise(this, out uintptr) uintptr {
	if out != 0 {
		*(*uintptr)(unsafe.Pointer(out)) = 0
	}
	return 0x80040003
}
func dragContinue(this, escape, keys uintptr) uintptr {
	if escape != 0 {
		return 0x40101
	}
	if keys&1 == 0 {
		return 0x40100
	}
	return 0
}
func dragFeedback(this, effect uintptr) uintptr { return 0x40102 }
func doFileDrag(paths []string) (bool, error) {
	data, source := newDragObject(paths, false), newDragObject(nil, true)
	defer dragRelease(uintptr(unsafe.Pointer(data)))
	defer dragRelease(uintptr(unsafe.Pointer(source)))
	var effect uint32
	hr := call(oleDrag.NewProc("DoDragDrop"), uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(source)), 1, uintptr(unsafe.Pointer(&effect)))
	runtime.KeepAlive(data)
	runtime.KeepAlive(source)
	if hr == 0x40101 {
		return false, nil
	}
	if int32(hr) < 0 {
		return false, fmt.Errorf("无法拖出文件（%08X）", uint32(hr))
	}
	return hr == 0x40100 && effect&1 != 0, nil
}
func copyChatFiles(id string) {
	paths, e := guiNode.exportTaskPaths(id)
	if e == nil {
		e = copyDesktopFiles(paths)
	}
	if e != nil {
		showError(e.Error())
		return
	}
	chatFeedback = "已复制文件，可到文件夹或聊天窗口粘贴"
	chatFeedbackUntil = time.Now().Add(3 * time.Second)
	invalidate(guiMain)
}
func copyDesktopFiles(paths []string) error {
	raw, e := fileDropBytes(paths)
	if e != nil {
		return e
	}
	h := dragGlobal(raw)
	if h == 0 {
		return errors.New("无法分配文件剪贴板")
	}
	defer func() {
		if h != 0 {
			call(kernel.NewProc("GlobalFree"), h)
		}
	}()
	if call(user.NewProc("OpenClipboard"), guiMain) == 0 {
		return errors.New("剪贴板正被占用，请重试")
	}
	defer call(user.NewProc("CloseClipboard"))
	if call(user.NewProc("EmptyClipboard")) == 0 || call(user.NewProc("SetClipboardData"), 15, h) == 0 {
		return errors.New("无法复制文件")
	}
	h = 0 // Clipboard now owns the allocation.
	if effect := dragGlobal([]byte{1, 0, 0, 0}); effect != 0 {
		if call(user.NewProc("SetClipboardData"), uintptr(dragEffectFormat), effect) == 0 {
			call(kernel.NewProc("GlobalFree"), effect)
		}
	}
	return nil
}

func chatDragMessage(hwnd uintptr, m uint32, w, l uintptr) bool {
	pos := point{int32(int16(l)), int32(int16(l >> 16))}
	if m == 0x201 {
		chatDragged = false
		chatDragID = ""
		hit := msg(hwnd, 0x1a9, 0, l)
		if hit>>16 == 0 && int(hit&0xffff) < len(guiTasks) {
			i := int(hit & 0xffff)
			t := guiTasks[i]
			var row rect
			msg(hwnd, 0x198, uintptr(i), uintptr(unsafe.Pointer(&row)))
			b := chatBubblePart(t, chatListWidth(), chatPartAt(i))
			if t.Kind != "text" && pos.X >= int32(sc(b.X)) && pos.X < int32(sc(b.X+b.W)) && pos.Y-row.T >= int32(sc(b.Y)) && pos.Y-row.T < int32(sc(b.Y+b.H)) {
				chatDragID = t.ID
				chatDragStart = pos
			}
		}
	}
	if m == 0x200 && w&1 != 0 && chatDragID != "" && !guiDragging {
		dx, dy := pos.X-chatDragStart.X, pos.Y-chatDragStart.Y
		cx, cy := int32(call(user.NewProc("GetSystemMetrics"), 68)), int32(call(user.NewProc("GetSystemMetrics"), 69))
		if dx < cx && dx > -cx && dy < cy && dy > -cy {
			return false
		}
		id := chatDragID
		chatDragID = ""
		chatDragged = true
		guiDragging = true
		call(user.NewProc("ReleaseCapture"))
		paths, e := guiNode.exportTaskPaths(id)
		if e == nil {
			_, e = doFileDrag(paths)
		}
		guiDragging = false
		if e != nil {
			showError(e.Error())
		}
		refreshGUI()
		return true
	}
	if m == 0x215 || m == 0x1f {
		chatDragID = ""
	}
	if m == 0x202 {
		chatDragID = ""
		if chatDragged {
			chatDragged = false
			return true
		}
	}
	return false
}
