package main

import "testing"

func TestDesktopEmbeddedIcons(t *testing.T) {
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	// Exercise the actual embedded resource, without the system-icon fallback.
	for _, size := range []int{16, 20, 24, 32, 48, 64, 128, 256} {
		if h := loadDesktopIcon(instance, size, size); h == 0 {
			t.Fatalf("embedded icon group %d failed at %dx%d", desktopIconResource, size, size)
		}
	}
	if e := loadDesktopIcons(instance); e != nil {
		t.Fatal(e)
	}
}
