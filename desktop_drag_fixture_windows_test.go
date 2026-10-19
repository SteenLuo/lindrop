package main

// Opt-in GUI fixture: all files and the OLE drop target belong to the test.
// No messages are sent to a real device or third-party chat application.
import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

var fixtureDropRoot string
var fixtureTarget struct{ Vtable *uintptr }
var fixtureTargetVtable []uintptr

func fixtureQuery(this, iid, out uintptr) uintptr {
	if out == 0 || iid == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	g := *(*syscall.GUID)(unsafe.Pointer(iid))
	base := syscall.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	if g != base {
		base.Data1 = 0x122
		if g != base {
			return 0x80004002
		}
	}
	*(*uintptr)(unsafe.Pointer(out)) = this
	return 0
}
func fixtureRef(this uintptr) uintptr { return 1 }
func fixtureEnter(data, effect uintptr) uintptr {
	f := dragFormats()[0]
	if comTestCall(data, 5, uintptr(unsafe.Pointer(&f))) == 0 {
		*(*uint32)(unsafe.Pointer(effect)) &= 1
	} else {
		*(*uint32)(unsafe.Pointer(effect)) = 0
	}
	return 0
}
func fixtureOver(effect uintptr) uintptr { *(*uint32)(unsafe.Pointer(effect)) &= 1; return 0 }
func fixtureLeave(this uintptr) uintptr  { return 0 }
func fixtureCopy(source, dest string) error {
	return filepath.Walk(source, func(p string, st os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, p)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, rel)
		if st.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.Create(target)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
}
func fixtureDrop(data, effect uintptr) uintptr {
	f := dragFormats()[0]
	var medium dragMedium
	*(*uint32)(unsafe.Pointer(effect)) = 0
	if comTestCall(data, 3, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&medium))) != 0 {
		return 0x80004005
	}
	defer call(oleDrag.NewProc("ReleaseStgMedium"), uintptr(unsafe.Pointer(&medium)))
	paths, e := desktopDropPaths(medium.Handle)
	if e != nil {
		return 0x80004005
	}
	for _, p := range paths {
		if e = fixtureCopy(p, filepath.Join(fixtureDropRoot, "dropped", filepath.Base(p))); e != nil {
			return 0x80004005
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"protocol": "DoDragDrop/CF_HDROP", "paths": paths, "effect": "copy"})
	os.WriteFile(filepath.Join(fixtureDropRoot, "drop-result.json"), raw, 0600)
	*(*uint32)(unsafe.Pointer(effect)) = 1
	return 0
}
func registerFixtureTarget(h uintptr) uintptr {
	fixtureTargetVtable = []uintptr{syscall.NewCallback(fixtureQuery), syscall.NewCallback(fixtureRef), syscall.NewCallback(fixtureRef)}
	if runtime.GOARCH == "386" {
		fixtureTargetVtable = append(fixtureTargetVtable,
			syscall.NewCallback(func(this, data, keys, x, y, effect uintptr) uintptr { return fixtureEnter(data, effect) }),
			syscall.NewCallback(func(this, keys, x, y, effect uintptr) uintptr { return fixtureOver(effect) }),
			syscall.NewCallback(fixtureLeave),
			syscall.NewCallback(func(this, data, keys, x, y, effect uintptr) uintptr { return fixtureDrop(data, effect) }))
	} else {
		fixtureTargetVtable = append(fixtureTargetVtable,
			syscall.NewCallback(func(this, data, keys, pt, effect uintptr) uintptr { return fixtureEnter(data, effect) }),
			syscall.NewCallback(func(this, keys, pt, effect uintptr) uintptr { return fixtureOver(effect) }),
			syscall.NewCallback(fixtureLeave),
			syscall.NewCallback(func(this, data, keys, pt, effect uintptr) uintptr { return fixtureDrop(data, effect) }))
	}
	fixtureTarget.Vtable = &fixtureTargetVtable[0]
	return call(oleDrag.NewProc("RegisterDragDrop"), h, uintptr(unsafe.Pointer(&fixtureTarget)))
}
func TestDesktopDragOutFixture(t *testing.T) {
	dir := os.Getenv("LINNODE_DRAG_QA_DIR")
	if dir == "" {
		t.Skip("opt-in native drag/paint fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if int32(call(oleDrag.NewProc("OleInitialize"), 0)) < 0 {
		t.Fatal("OLE")
	}
	defer call(oleDrag.NewProc("OleUninitialize"))
	fixtureDropRoot = dir
	os.MkdirAll(filepath.Join(dir, "dropped"), 0700)
	n, e := NewNode(filepath.Join(dir, "state"), "绘制拖放测试", filepath.Join(dir, "receive"))
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	if e = n.Start("127.0.0.1", 0); e != nil {
		t.Fatal(e)
	}
	image := filepath.Join(dir, "图片样本.png")
	plain := filepath.Join(dir, "拖出说明😀.txt")
	os.WriteFile(plain, []byte("拖出的文件必须保持字节一致。"), 0600)
	paths := []string{plain, image}
	for i, p := range paths {
		files, folder, title, e := collect([]string{p})
		if e != nil {
			t.Fatal(e)
		}
		id := []string{"plain", "image"}[i]
		media := ""
		if i == 1 {
			media = "image"
		}
		n.cfg.Tasks[id] = &Task{ID: id, Direction: "out", Sender: n.id, Target: "test", Title: title, Status: "done", Folder: folder, Files: files, Media: media, Created: int64(i + 30)}
	}
	for i := 0; i < 14; i++ {
		id := string(rune('a' + i))
		t := &Task{ID: id, Direction: "in", Sender: "test", Target: n.id, Kind: "text", Status: "done", Created: int64(i + 1)}
		n.cfg.Tasks[id] = t
		chatTextCache[id] = "滚动检查：消息应保持稳定，复制按钮只在悬浮时出现。"
	}
	guiSelected = "test"
	guiWindowTitle = "邻传 · 拖出与绘制测试"
	registered := false
	var target uintptr
	guiReadSnapshot = func() map[string]interface{} {
		if !registered {
			target = guiControls[170]
			if hr := registerFixtureTarget(target); hr != 0 {
				panic(hr)
			}
			registered = true
			cue(170, "测试接收区：把上方文件/图片拖到这里")
		}
		s := n.snapshot(true, WebClient{})
		s["devices"] = []Device{{ID: "test", Name: "隔离测试设备", Online: true, Paired: true}}
		return s
	}
	defer func() {
		if target != 0 {
			call(oleDrag.NewProc("RevokeDragDrop"), target)
		}
	}()
	raw, _ := json.Marshal(map[string]string{"local": n.localAddr, "admin": n.admin, "kind": "isolated-drag-fixture"})
	os.WriteFile(filepath.Join(dir, "control.json"), raw, 0600)
	runDesktop(n)
}
