package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
	"time"
)

var dragProfileOriginal = map[uintptr]uintptr{}
var dragProfileArrivals chan time.Time
var dragProfileHandlers, dragProfileQueue []float64
var dragProfileLayouts, dragProfilePaints int

func dragProfileProc(h uintptr, m uint32, w, l uintptr) uintptr {
	input := h == guiMain && m == 0x200 && w&0x8000 != 0
	started := time.Now()
	if input {
		arrival := <-dragProfileArrivals
		dragProfileQueue = append(dragProfileQueue, float64(started.Sub(arrival))/float64(time.Millisecond))
	}
	if m == 0xf {
		dragProfilePaints++
	}
	if m == 0x47 && h == guiControls[126] {
		dragProfileLayouts++
	}
	result := call(user.NewProc("CallWindowProcW"), dragProfileOriginal[h], h, uintptr(m), w, l)
	if input {
		dragProfileHandlers = append(dragProfileHandlers, float64(time.Since(started))/float64(time.Millisecond))
	}
	return result
}

func profilePercentile(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	return copyValues[(len(copyValues)-1)*95/100]
}

// Same production mouse-message path before/after the optimization. This is
// deliberately run on the remote desktop only, as part of the pixel fixture.
func runComposerDragProfile(t *testing.T, onUI func(func() error) error) error {
	var root uintptr
	var startY, startHeight int
	if err := onUI(func() error {
		root = guiMain
		startY = currentComposerLayout().Top + 2
		startHeight = currentComposerLayout().TextHeight
		chatResizeActive = true
		chatResizeStartY = startY
		chatResizeStartHeight = startHeight
		dragProfileArrivals = make(chan time.Time, 128)
		dragProfileHandlers = nil
		dragProfileQueue = nil
		dragProfileLayouts = 0
		dragProfilePaints = 0
		for _, h := range []uintptr{guiMain, guiComposerWindow, guiControls[126]} {
			if h != 0 {
				dragProfileOriginal[h] = setWindowProcedure(h, syscall.NewCallback(dragProfileProc))
			}
		}
		return nil
	}); err != nil {
		return err
	}
	started := time.Now()
	lastHeight := 0
	for i := 0; i < 120; i++ {
		height := 96 + (i%40)*4
		lastHeight = height
		dragProfileArrivals <- time.Now()
		position := uintptr(uint16(sc(700))) | uintptr(uint16(sc(startY+startHeight-height)))<<16
		if call(postMessage, root, 0x200, 0x8001, position) == 0 {
			return fmt.Errorf("post drag sample")
		}
		time.Sleep(8 * time.Millisecond)
	}
	if err := onUI(func() error {
		position := uintptr(uint16(sc(700))) | uintptr(uint16(sc(startY+startHeight-lastHeight)))<<16
		msg(guiMain, 0x202, 0, position)
		wanted := composerLayout(guiHeight, lastHeight, len(chatPictures[chatDraftDevice])).TextHeight
		if currentComposerLayout().TextHeight != wanted {
			return fmt.Errorf("release did not use latest pointer position")
		}
		return nil
	}); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	return onUI(func() error {
		defer func() {
			for h, proc := range dragProfileOriginal {
				setWindowProcedure(h, proc)
			}
			dragProfileOriginal = map[uintptr]uintptr{}
		}()
		if len(dragProfileHandlers) != 120 {
			return fmt.Errorf("lost drag samples: %d", len(dragProfileHandlers))
		}
		report := map[string]interface{}{"input_events": len(dragProfileHandlers), "input_p95_ms": profilePercentile(dragProfileHandlers), "queue_p95_ms": profilePercentile(dragProfileQueue), "elapsed_ms": float64(time.Since(started)) / float64(time.Millisecond), "layout_commits": dragProfileLayouts, "paint_calls": dragProfilePaints}
		raw, _ := json.Marshal(report)
		t.Log("DRAG_PROFILE " + string(raw))
		if dir := os.Getenv("LINDROP_RESIZE_EVIDENCE"); dir != "" {
			dir = filepath.Join(dir, runtime.GOARCH)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "drag-profile.json"), raw, 0600)
		}
		return nil
	})
}
