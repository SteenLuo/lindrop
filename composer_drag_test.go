package main

import "testing"

func TestComposerDragFrameKeepsOnlyLatestPointer(t *testing.T) {
	var frame composerDragFrame
	arms := 0
	for i := 0; i < 2000; i++ {
		if frame.request(64 + i) {
			arms++
		}
	}
	if arms != 1 {
		t.Fatal("motion restarted frame timer", arms)
	}
	if height, ok := frame.take(); !ok || height != 2063 {
		t.Fatal("frame did not consume latest pointer", height, ok)
	}
	if _, ok := frame.take(); ok {
		t.Fatal("frame was replayed")
	}
	if frame.request(120) {
		t.Fatal("active cadence was restarted")
	}
	frame.reset()
	if frame.Armed || frame.Pending {
		t.Fatal("finished drag left work pending")
	}
	if !frame.request(180) {
		t.Fatal("new drag did not arm a timer")
	}
}
