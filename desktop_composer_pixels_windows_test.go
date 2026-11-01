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
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var syscallDwmFlush = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")

func checkPresentedFooter(frame *image.NRGBA) error {
	var phone point
	call(user.NewProc("MapWindowPoints"), guiControls[103], guiMain, uintptr(unsafe.Pointer(&phone)), 1)
	// Moving the phone icon over the old settings position used to copy its
	// pixels into this owner-drawn button. The margins must remain plain rail.
	if guiHover != 103 {
		for y := sc(2); y < sc(36); y++ {
			for x := sc(2); x < sc(36); x++ {
				if x >= sc(8) && x <= sc(30) && y >= sc(8) && y <= sc(30) {
					continue
				}
				if got := frame.NRGBAAt(int(phone.X)+x, int(phone.Y)+y); got != (imagecolor.NRGBA{236, 240, 232, 255}) {
					return fmt.Errorf("phone icon retained moved control pixels: %v at %d,%d", got, x, y)
				}
			}
		}
	}
	// A correct footer must not conceal a stale/blank input toolbar after
	// maximizing. Inspect the already presented native file button as well.
	var tool point
	call(user.NewProc("MapWindowPoints"), guiControls[121], guiMain, uintptr(unsafe.Pointer(&tool)), 1)
	want := imagecolor.NRGBA{49, 95, 78, 255}
	if call(user.NewProc("IsWindowEnabled"), guiControls[121]) == 0 {
		want = imagecolor.NRGBA{173, 191, 177, 255}
	} else if guiHover == 121 {
		want = imagecolor.NRGBA{44, 86, 70, 255}
	}
	for y := int(tool.Y) + sc(12); y < int(tool.Y)+sc(15); y++ {
		for x := int(tool.X) + sc(7); x < int(tool.X)+sc(10); x++ {
			if frame.NRGBAAt(x, y) != want {
				return fmt.Errorf("input toolbar missing/stale at %d,%d: got %v want %v", x, y, frame.NRGBAAt(x, y), want)
			}
		}
	}
	footer := chatClient(guiComposerFooter)
	if int(footer.B) != sc(composerFooterHeight) {
		return fmt.Errorf("footer height changed: %d", footer.B)
	}
	var fp, bp point
	call(user.NewProc("MapWindowPoints"), guiComposerFooter, guiMain, uintptr(unsafe.Pointer(&fp)), 1)
	call(user.NewProc("MapWindowPoints"), guiControls[171], guiMain, uintptr(unsafe.Pointer(&bp)), 1)
	button := chatClient(guiControls[171])
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	if abs(int(fp.Y+footer.B)-frame.Bounds().Dy()) > 1 || abs(frame.Bounds().Dy()-int(bp.Y+button.B)-sc(7)) > 1 || int(button.B) != sc(29) {
		return fmt.Errorf("footer/button lost bottom anchor: footer=%v button=%v/%v frame=%v", fp, bp, button, frame.Bounds())
	}
	if abs(frame.Bounds().Dx()-int(bp.X+button.R)-sc(25)) > 1 {
		return fmt.Errorf("send button lost right anchor")
	}
	for y := int(bp.Y+button.B) + 1; y < frame.Bounds().Dy()-1; y++ {
		for x := int(bp.X) + sc(8); x < int(bp.X+button.R)-sc(8); x++ {
			if frame.NRGBAAt(x, y) != (imagecolor.NRGBA{255, 255, 255, 255}) {
				return fmt.Errorf("stale colored pixel below send button at %d,%d", x, y)
			}
		}
	}
	return nil
}

// Read the frame already presented on the disposable CI desktop, rather than
// printing controls into a fresh bitmap (which would conceal stale pixels).
func captureComposerFrame() (*image.NRGBA, error) {
	r := chatClient(guiMain)
	return captureComposerRegion(r)
}

func captureComposerRegion(r rect) (*image.NRGBA, error) {
	w, h := int(r.R-r.L), int(r.B-r.T)
	var origin point
	origin.X, origin.Y = r.L, r.T
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
		for _, scale := range []string{"1", "2"} {
			cmd := exec.Command(exe, "-test.run=^TestDesktopComposerPresentedPixelFrames$", "-test.v", "-test.timeout=60s")
			cmd.Env = append(os.Environ(), "LINDROP_PIXEL_CHILD=1", "LINDROP_PIXEL_SCALE="+scale,
				"LINDROP_RESIZE_EVIDENCE="+filepath.Join(os.Getenv("LINDROP_RESIZE_EVIDENCE"), "scale-"+scale))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("presented frame fixture scale %s: %v\n%s", scale, err, out)
			}
			t.Log(string(out))
		}
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
	sampleOnline := true
	guiReadSnapshot = func() map[string]interface{} {
		if !ready {
			ready = true
			close(created)
		}
		return map[string]interface{}{"saveDir": n.cfg.SaveDir, "devices": []Device{{ID: "sample-peer", Name: "示例设备", Online: sampleOnline}}, "tasks": []Task{
			{ID: "sample-file", Title: "示例文档.txt", Direction: "in", Sender: "sample-peer", Target: n.id, Status: "done", Total: 1024, Done: 1024, Created: 1},
			{ID: "sample-text", Kind: "text", Direction: "out", Sender: n.id, Target: "sample-peer", Status: "done", Created: 2},
		}}
	}
	result := make(chan error, 1)
	go func() {
		<-created
		time.Sleep(200 * time.Millisecond)
		onUI := func(fn func() error) error {
			done := make(chan error, 1)
			queue(func() { done <- fn() })
			return <-done
		}
		defer queue(func() { chatResizeActive = false; destroyWindow.Call(guiMain) })
		failure := onUI(func() error {
			if scale, err := strconv.ParseFloat(os.Getenv("LINDROP_PIXEL_SCALE"), 64); err == nil && scale > 1 {
				guiScale = scale
				guiPositions = map[int][4]int{}
				chatWidths = map[rune]int{}
				desktopFonts = map[[2]int]uintptr{}
				guiFont = fontSize(13, 400)
				guiSmall, guiTiny = fontSize(11, 400), fontSize(9, 400)
				guiMedium, guiTitle = fontSize(13, 600), fontSize(18, 600)
				for _, id := range []int{105, 170} {
					msg(guiControls[id], 0x30, guiFont, 1)
				}
				work := desktopWorkArea()
				call(user.NewProc("SetWindowPos"), guiMain, 0, uintptr(work.L+10), uintptr(work.T+10), uintptr(work.R-work.L-20), uintptr(work.B-work.T-20), 0x4|0x10)
				layout()
			}
			call(user.NewProc("SetForegroundWindow"), guiMain)
			call(user.NewProc("SetFocus"), guiMain)
			chatResizeActive = true
			call(user.NewProc("RedrawWindow"), guiMain, 0, 0, 0x1|0x4|0x80|0x100|0x400)
			return nil
		})
		if failure != nil {
			result <- failure
			return
		}
		var fixedButton rect
		onUI(func() error {
			call(user.NewProc("GetWindowRect"), guiControls[171], uintptr(unsafe.Pointer(&fixedButton)))
			return nil
		})
		for step, height := range []int{250, 64, 260, 96, 230, 64, 250, 120} {
			failure = onUI(func() error { resizeChatComposer(height); return nil })
			if failure != nil {
				result <- failure
				return
			}
			// Return to the real application message loop between motion and
			// capture. Printing/updating controls here would repair stale pixels;
			// blocking the UI for the whole loop would capture an old DWM frame.
			time.Sleep(45 * time.Millisecond)
			failure = onUI(func() error {
				frame, e := captureComposerFrame()
				if e != nil {
					return e
				}
				var button rect
				call(user.NewProc("GetWindowRect"), guiControls[171], uintptr(unsafe.Pointer(&button)))
				if button != fixedButton {
					saveComposerEvidence(frame, fmt.Sprintf("footer-moved-%02d", step))
					return fmt.Errorf("input resize moved send button")
				}
				if e = checkPresentedFooter(frame); e != nil {
					saveComposerEvidence(frame, fmt.Sprintf("footer-stale-%02d", step))
					return e
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
					return fmt.Errorf("step %d height %d: %d stale pixels in blank native input", step, height, bad)
				}
				if step == 0 || step == 7 {
					if e = saveComposerEvidence(frame, fmt.Sprintf("clean-frame-%02d", step)); e != nil {
						return e
					}
				}
				return nil
			})
			if failure != nil {
				result <- failure
				return
			}
		}
		failure = onUI(func() error { chatResizeActive = false; return nil })
		var original rect
		if failure == nil {
			failure = onUI(func() error {
				call(user.NewProc("GetWindowRect"), guiMain, uintptr(unsafe.Pointer(&original)))
				return nil
			})
		}
		if failure != nil {
			result <- failure
			return
		}
		for phase, name := range []string{"maximized", "restored", "smaller", "original"} {
			failure = onUI(func() error {
				switch phase {
				case 0:
					showWindow.Call(guiMain, 3)
				case 1:
					showWindow.Call(guiMain, 9)
				case 2:
					call(user.NewProc("SetWindowPos"), guiMain, 0, uintptr(original.L), uintptr(original.T), uintptr(original.R-original.L-70), uintptr(original.B-original.T-35), 0x4|0x10)
				case 3:
					call(user.NewProc("SetWindowPos"), guiMain, 0, uintptr(original.L), uintptr(original.T), uintptr(original.R-original.L), uintptr(original.B-original.T), 0x4|0x10)
				}
				return nil
			})
			if failure != nil {
				result <- failure
				return
			}
			time.Sleep(75 * time.Millisecond)
			failure = onUI(func() error {
				frame, e := captureComposerFrame()
				if e != nil {
					return e
				}
				if e = checkPresentedFooter(frame); e != nil {
					saveComposerEvidence(frame, "footer-bad-"+name)
					return e
				}
				return saveComposerEvidence(frame, "footer-"+name)
			})
			if failure != nil {
				result <- failure
				return
			}
		}
		failure = onUI(func() error { sampleOnline = false; refreshGUI(); return nil })
		if failure != nil {
			result <- failure
			return
		}
		time.Sleep(75 * time.Millisecond)
		failure = onUI(func() error {
			frame, e := captureComposerFrame()
			if e != nil {
				return e
			}
			if e = checkPresentedFooter(frame); e != nil {
				saveComposerEvidence(frame, "footer-bad-offline")
				return e
			}
			return saveComposerEvidence(frame, "footer-offline")
		})
		if failure != nil {
			result <- failure
			return
		}
		failure = onUI(func() error { sampleOnline = true; refreshGUI(); return nil })
		if failure != nil {
			result <- failure
			return
		}
		if failure = runComposerScrollbarProfile(t, onUI); failure != nil {
			result <- failure
			return
		}
		result <- runComposerDragProfile(t, onUI)
	}()
	runDesktop(n)
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}
