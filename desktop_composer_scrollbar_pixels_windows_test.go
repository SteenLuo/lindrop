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
	var area rect
	if err := onUI(func() error {
		chatResizeActive = true
		resizeChatComposer(160)
		call(user.NewProc("SetFocus"), guiMain)
		return nil
	}); err != nil {
		return err
	}
	time.Sleep(75 * time.Millisecond)
	var baseline *image.NRGBA
	if err := onUI(func() error {
		scroll, ok := composerScrollbarBounds(guiControls[170])
		if !ok {
			return fmt.Errorf("native scrollbar unavailable")
		}
		var origin point
		call(user.NewProc("ClientToScreen"), guiMain, uintptr(unsafe.Pointer(&origin)))
		width := scroll.R - scroll.L
		area = rect{scroll.L - origin.X, scroll.B - origin.Y - width*3, scroll.R - origin.X, scroll.B - origin.Y + int32(sc(3))}
		var err error
		baseline, err = captureComposerRegion(area)
		if err == nil {
			err = saveComposerEvidence(baseline, "scrollbar-baseline")
		}
		return err
	}); err != nil {
		return err
	}
	changed := 0
	for step := 0; step < 180; step++ {
		height := 160 + step
		if step >= 90 {
			height = 250 - (step - 90)
		}
		if err := onUI(func() error { resizeChatComposer(height); return nil }); err != nil {
			return err
		}
		time.Sleep(16 * time.Millisecond)
		if err := onUI(func() error {
			frame, err := captureComposerRegion(area)
			if err != nil {
				return err
			}
			if !bytes.Equal(frame.Pix, baseline.Pix) {
				changed++
				if changed <= 4 {
					if err = saveComposerEvidence(frame, fmt.Sprintf("scrollbar-changed-%03d", step)); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if changed != 0 {
		return fmt.Errorf("fixed input scrollbar changed pixels during %d/180 resize frames", changed)
	}
	t.Log("SCROLLBAR_STABILITY: 180 height changes, identical presented bottom-arrow pixels")
	return onUI(func() error { chatResizeActive = false; return nil })
}
