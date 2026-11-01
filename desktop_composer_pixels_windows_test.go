package main

import (
	"fmt"
	"image"
	imagecolor "image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var syscallDwmFlush = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")

// Read the frame already presented on the disposable CI desktop, rather than
// printing controls into a fresh bitmap (which would conceal stale pixels).
func captureComposerFrame() (*image.NRGBA, error) {
	r := chatClient(guiMain)
	w, h := int(r.R), int(r.B)
	var origin point
	call(user.NewProc("ClientToScreen"), guiMain, uintptr(unsafe.Pointer(&origin)))
	screen := call(user.NewProc("GetDC"), 0)
	defer call(user.NewProc("ReleaseDC"), 0, screen)
	dc := call(gdi.NewProc("CreateCompatibleDC"), screen)
	defer call(gdi.NewProc("DeleteDC"), dc)
	header := struct {
		Size                   uint32
		W, H                   int32
		Planes, Bits           uint16
		Compression, ImageSize uint32
		X, Y                   int32
		Used, Important        uint32
	}{Size: 40, W: int32(w), H: -int32(h), Planes: 1, Bits: 32}
	var ptr uintptr
	bmp := call(gdi.NewProc("CreateDIBSection"), dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&ptr)), 0, 0)
	if bmp == 0 || ptr == 0 {
		return nil, fmt.Errorf("frame bitmap")
	}
	defer call(gdi.NewProc("DeleteObject"), bmp)
	old := call(gdi.NewProc("SelectObject"), dc, bmp)
	defer call(gdi.NewProc("SelectObject"), dc, old)
	call(syscallDwmFlush)
	if call(gdi.NewProc("BitBlt"), dc, 0, 0, uintptr(w), uintptr(h), screen, uintptr(origin.X), uintptr(origin.Y), 0xcc0020|0x40000000) == 0 {
		return nil, fmt.Errorf("capture presented frame")
	}
	call(gdi.NewProc("GdiFlush"))
	raw := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), w*h*4)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(raw); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = raw[i+2], raw[i+1], raw[i], 255
	}
	return img, nil
}

func saveComposerEvidence(img image.Image, name string) error {
	dir := os.Getenv("LINDROP_RESIZE_EVIDENCE")
	if dir == "" {
		return nil
	}
	dir = filepath.Join(dir, runtime.GOARCH)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func TestDesktopComposerPresentedPixelFrames(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("visible pixel fixture only on disposable remote runner")
	}
	if os.Getenv("LINDROP_PIXEL_CHILD") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestDesktopComposerPresentedPixelFrames$", "-test.v", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), "LINDROP_PIXEL_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("presented frame fixture: %v\n%s", err, out)
		}
		t.Log(string(out))
		return
	}
	n, err := NewNode(t.TempDir(), "示例电脑", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	guiClientWidth, guiClientHeight = 1050, 700
	guiSelected = "sample-peer"
	chatTextCache["sample-text"] = "一段用于绘制验证的示例文字"
	created := make(chan struct{})
	ready := false
	guiReadSnapshot = func() map[string]interface{} {
		if !ready {
			ready = true
			close(created)
		}
		return map[string]interface{}{"saveDir": n.cfg.SaveDir, "devices": []Device{{ID: "sample-peer", Name: "示例设备", Online: true}}, "tasks": []Task{
			{ID: "sample-file", Title: "示例文档.txt", Direction: "in", Sender: "sample-peer", Target: n.id, Status: "done", Total: 1024, Done: 1024, Created: 1},
			{ID: "sample-text", Kind: "text", Direction: "out", Sender: n.id, Target: "sample-peer", Status: "done", Created: 2},
		}}
	}
	result := make(chan error, 1)
	go func() {
		<-created
		time.Sleep(200 * time.Millisecond)
		queue(func() {
			defer destroyWindow.Call(guiMain)
			call(user.NewProc("SetForegroundWindow"), guiMain)
			call(user.NewProc("SetFocus"), guiControls[170])
			call(user.NewProc("HideCaret"), guiControls[170])
			call(user.NewProc("RedrawWindow"), guiMain, 0, 0, 0x1|0x4|0x80|0x100|0x400)
			for step, height := range []int{250, 64, 260, 96, 230, 64, 250, 120} {
				resizeChatComposer(height)
				// Honor only the update regions left by the real resize path.
				// Never request an additional erase that could repair the bug.
				call(user.NewProc("UpdateWindow"), guiMain)
				for _, id := range []int{126, 121, 122, 170, 171} {
					call(user.NewProc("UpdateWindow"), guiControls[id])
				}
				frame, e := captureComposerFrame()
				if e != nil {
					result <- e
					return
				}
				var p point
				call(user.NewProc("MapWindowPoints"), guiControls[170], guiMain, uintptr(unsafe.Pointer(&p)), 1)
				client := chatClient(guiControls[170])
				bad := 0
				for y := int(p.Y) + sc(30); y < int(p.Y+client.B)-sc(6); y++ {
					for x := int(p.X) + sc(8); x < int(p.X+client.R)-sc(35); x++ {
						if frame.NRGBAAt(x, y) != (imagecolor.NRGBA{255, 255, 255, 255}) {
							bad++
						}
					}
				}
				if bad > 0 {
					saveComposerEvidence(frame, fmt.Sprintf("stale-frame-%02d", step))
					result <- fmt.Errorf("step %d height %d: %d stale pixels in blank native input", step, height, bad)
					return
				}
				if step == 0 || step == 7 {
					if e = saveComposerEvidence(frame, fmt.Sprintf("clean-frame-%02d", step)); e != nil {
						result <- e
						return
					}
				}
			}
			result <- nil
		})
	}()
	runDesktop(n)
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}
