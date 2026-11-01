package main

import (
	"bytes"
	"fmt"
	"image"
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
	arrowFrame := func() (*image.NRGBA, error) {
		scroll, ok := composerScrollbarBounds(guiControls[170])
		if !ok {
			return nil, fmt.Errorf("native scrollbar unavailable")
		}
		var origin point
		call(user.NewProc("ClientToScreen"), guiMain, uintptr(unsafe.Pointer(&origin)))
		width := scroll.R - scroll.L
		area = rect{scroll.L - origin.X, scroll.B - origin.Y - width*3, scroll.R - origin.X, scroll.B - origin.Y + int32(sc(3))}
		return captureComposerRegion(area)
	}
	if err := onUI(func() error {
		var err error
		baseline, err = arrowFrame()
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
				frame, err := arrowFrame()
				if err != nil {
					return err
				}
				if !bytes.Equal(frame.Pix, baseline.Pix) {
					changed++
					if changed <= 4 {
						if err = saveComposerEvidence(frame, fmt.Sprintf("scrollbar-changed-%s-%03d", mode, step)); err != nil {
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
