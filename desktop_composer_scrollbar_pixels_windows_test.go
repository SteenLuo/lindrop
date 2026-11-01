package main

import (
	"bytes"
	"fmt"
	"image"
	imagecolor "image/color"
	"image/draw"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// Only called by the GITHUB_ACTIONS-gated disposable desktop fixture. A blank
// input has no scroll-state change; its fixed bottom arrow must have identical
// presented pixels throughout height changes, including intermediate frames.
func runComposerScrollbarProfile(t *testing.T, onUI func(func() error) error) error {
	t.Helper()
	var area, original rect
	if err := onUI(func() error {
		chatResizeActive = true
		resizeChatComposer(160)
		call(user.NewProc("SetFocus"), guiMain)
		call(user.NewProc("GetWindowRect"), guiMain, uintptr(unsafe.Pointer(&original)))
		return nil
	}); err != nil {
		return err
	}
	time.Sleep(75 * time.Millisecond)
	var baseline *image.NRGBA
	var lastContext *image.NRGBA
	var trackBackground []imagecolor.NRGBA
	buttonInset, insetKnown := 0, false
	arrowFrame := func(windowResize bool) (*image.NRGBA, error) {
		scroll, ok := composerScrollbarBounds(guiControls[170])
		if !ok {
			return nil, fmt.Errorf("native scrollbar unavailable")
		}
		var origin point
		call(user.NewProc("ClientToScreen"), guiMain, uintptr(unsafe.Pointer(&origin)))
		width := scroll.R - scroll.L
		area = rect{scroll.L - origin.X, scroll.B - origin.Y - width*3, scroll.R - origin.X, scroll.B - origin.Y + int32(sc(3))}
		var button point
		call(user.NewProc("MapWindowPoints"), guiControls[171], guiMain, uintptr(unsafe.Pointer(&button)), 1)
		buttonBottom := int(button.Y + chatClient(guiControls[171]).B)
		context := rect{button.X + int32(sc(8)), scroll.T - origin.Y, area.R, int32(buttonBottom + sc(4))}
		frame, err := captureComposerRegion(context)
		if err != nil {
			return nil, err
		}
		lastContext = frame
		lastGreen := -1
		for y := 0; y < frame.Bounds().Dy(); y++ {
			p := frame.NRGBAAt(0, y)
			if p.R == 49 && p.G == 95 && p.B == 78 {
				lastGreen = y + int(context.T)
			}
		}
		if lastGreen < 0 {
			return nil, fmt.Errorf("presented send-button anchor unavailable")
		}
		if !insetKnown {
			buttonInset, insetKnown = lastGreen-buttonBottom, true
		}
		shift := 0
		if windowResize {
			// DWM can present a previous WHOLE window frame. Align to the send
			// button in this same captured frame; still reject arrow-only jumps
			// or double arrows. Splitter checks keep absolute screen anchoring.
			shift = lastGreen - buttonBottom - buttonInset
		}
		left := int(scroll.L - origin.X - context.L)
		top := int(scroll.T-origin.Y-context.T) + shift
		bottom := int(scroll.B-origin.Y-context.T) + shift
		if trackBackground == nil {
			trackBackground = make([]imagecolor.NRGBA, int(width))
			for x := int(width) / 3; x < int(width)*2/3; x++ {
				trackBackground[x] = frame.NRGBAAt(left+x, (top+bottom)/2)
			}
		}
		for y := top + sc(composerArrowHeight()) + 3; y < bottom-sc(composerArrowHeight())-3; y++ {
			for x := int(width) / 3; x < int(width)*2/3; x++ {
				if frame.NRGBAAt(left+x, y) != trackBackground[x] {
					saveComposerEvidence(frame, "scrollbar-track-ghost")
					return nil, fmt.Errorf("blank scrollbar track contains stale glyph pixels at %d,%d", x, y)
				}
			}
		}
		crop := image.Rect(int(area.L-context.L), int(area.T-context.T)+shift, int(area.R-context.L), int(area.B-context.T)+shift)
		if !crop.In(frame.Bounds()) {
			return nil, fmt.Errorf("presented arrow outside capture: %v", crop)
		}
		out := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(out, out.Bounds(), frame, crop.Min, draw.Src)
		return out, nil
	}
	if err := onUI(func() error {
		var err error
		baseline, err = arrowFrame(false)
		if err == nil {
			err = saveComposerEvidence(baseline, "scrollbar-baseline")
		}
		return err
	}); err != nil {
		return err
	}
	changed, total := 0, 0
	for _, mode := range []string{"input", "window"} {
		for step := 0; step < 120; step++ {
			height := 96 + step
			if step >= 60 {
				height = 156 - (step - 60)
			}
			if mode == "input" && step < 20 {
				height = []int{64, 250, 96, 240, 64, 220, 80, 200, 96, 260}[step%10]
			}
			if err := onUI(func() error {
				if mode == "input" {
					resizeChatComposer(height)
				} else {
					delta := int32(120 - step)
					call(user.NewProc("SetWindowPos"), guiMain, 0, uintptr(original.L), uintptr(original.T), uintptr(original.R-original.L), uintptr(original.B-original.T-delta), 0x4|0x10)
				}
				return nil
			}); err != nil {
				return err
			}
			time.Sleep(16 * time.Millisecond)
			if err := onUI(func() error {
				frame, err := arrowFrame(mode == "window")
				if err != nil {
					return err
				}
				if !bytes.Equal(frame.Pix, baseline.Pix) {
					changed++
					if changed <= 4 {
						if err = saveComposerEvidence(frame, fmt.Sprintf("scrollbar-changed-%s-%03d", mode, step)); err != nil {
							return err
						}
						if err = saveComposerEvidence(lastContext, fmt.Sprintf("scrollbar-context-%s-%03d", mode, step)); err != nil {
							return err
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
			total++
		}
	}
	if changed != 0 {
		return fmt.Errorf("fixed input scrollbar changed pixels during %d/%d resize frames", changed, total)
	}
	t.Logf("SCROLLBAR_STABILITY: %d input/window height changes, identical presented bottom-arrow pixels", total)
	return onUI(func() error {
		call(user.NewProc("SetWindowPos"), guiMain, 0, uintptr(original.L), uintptr(original.T), uintptr(original.R-original.L), uintptr(original.B-original.T), 0x4|0x10)
		chatResizeActive = false
		return nil
	})
}

func runComposerNativeThumbDrag(onUI func(func() error) error) error {
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("LINDROP_PIXEL_CHILD") != "1" {
		return fmt.Errorf("native input interaction is restricted to the disposable remote fixture")
	}
	var start, end, down point
	var selection uintptr
	if err := onUI(func() error {
		set(guiControls[170], strings.Repeat("a\r\n", 70000))
		call(user.NewProc("SetFocus"), guiControls[170])
		msg(guiControls[170], 0xb1, 2, 7)
		selection = msg(guiControls[170], 0xb0, 0, 0)
		var downRect rect
		call(user.NewProc("GetWindowRect"), guiControls[composerDownArrowID], uintptr(unsafe.Pointer(&downRect)))
		down = point{(downRect.L + downRect.R) / 2, (downRect.T + downRect.B) / 2}
		s := composerScrollbarInfo{Size: uint32(unsafe.Sizeof(composerScrollbarInfo{}))}
		object := int32(-4)
		if call(user.NewProc("GetScrollBarInfo"), guiControls[composerScrollbarID], uintptr(object), uintptr(unsafe.Pointer(&s))) == 0 {
			return fmt.Errorf("thumb geometry unavailable")
		}
		start = point{(s.Rect.L + s.Rect.R) / 2, s.Rect.T + (s.ThumbTop+s.ThumbBottom)/2}
		end = point{start.X, s.Rect.B - s.LineButton - 2}
		return nil
	}); err != nil {
		return err
	}
	time.Sleep(80 * time.Millisecond)
	call(user.NewProc("SetCursorPos"), uintptr(down.X), uintptr(down.Y))
	call(user.NewProc("mouse_event"), 2, 0, 0, 0, 0)
	defer call(user.NewProc("mouse_event"), 4, 0, 0, 0, 0)
	time.Sleep(450 * time.Millisecond)
	call(user.NewProc("mouse_event"), 4, 0, 0, 0, 0)
	time.Sleep(60 * time.Millisecond)
	if err := onUI(func() error {
		if msg(guiControls[170], 0xce, 0, 0) < 2 {
			return fmt.Errorf("fixed down arrow did not repeat while held")
		}
		if msg(guiControls[170], 0xb0, 0, 0) != selection || call(user.NewProc("GetFocus")) != guiControls[170] {
			return fmt.Errorf("fixed down arrow changed selection or focus")
		}
		scrollComposerInput(6)
		return nil
	}); err != nil {
		return err
	}
	call(user.NewProc("SetCursorPos"), uintptr(start.X), uintptr(start.Y))
	call(user.NewProc("mouse_event"), 2, 0, 0, 0, 0)
	defer call(user.NewProc("mouse_event"), 4, 0, 0, 0, 0)
	for i := int32(1); i <= 12; i++ {
		call(user.NewProc("SetCursorPos"), uintptr(end.X), uintptr(start.Y+(end.Y-start.Y)*i/12))
		time.Sleep(20 * time.Millisecond)
	}
	call(user.NewProc("mouse_event"), 4, 0, 0, 0, 0)
	time.Sleep(80 * time.Millisecond)
	err := onUI(func() error {
		first := int32(msg(guiControls[170], 0xce, 0, 0))
		if first <= 65535 {
			return fmt.Errorf("native thumb drag lost 32-bit position: %d", first)
		}
		if msg(guiControls[170], 0xb0, 0, 0) != selection {
			return fmt.Errorf("native scrollbar drag changed text selection")
		}
		if call(user.NewProc("GetFocus")) != guiControls[170] {
			return fmt.Errorf("scrollbar stole text input focus")
		}
		set(guiControls[170], "")
		call(user.NewProc("SetFocus"), guiMain)
		return nil
	})
	// Drain the disposable desktop's real mouse motion before the following
	// synthetic splitter profile arms its independent drag state.
	call(user.NewProc("SetCursorPos"), 0, 0)
	time.Sleep(150 * time.Millisecond)
	return err
}
