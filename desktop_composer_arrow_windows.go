package main

import (
	"syscall"
	"unsafe"
)

const composerDownArrowID = 483

var composerArrowTheme = syscall.NewLazyDLL("uxtheme.dll")
var composerArrowPressed, composerArrowInside, composerArrowHot bool

func createComposerDownArrow() {
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(composerArrowProc),
		Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Name: wp("LinDropComposerDownArrow")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	h := themedControl("LinDropComposerDownArrow", "向下滚动", composerDownArrowID, 0x04000000)
	style := call(user.NewProc("GetWindowLongW"), h, uintptr(0xfffffff0))
	call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffff0), style&^0x10000)
	// A sibling of the resizing pane, above it in z-order. Its rectangle does
	// not change when the splitter changes the input height.
	call(user.NewProc("SetWindowPos"), h, 0, 0, 0, 0, 0, 0x1|0x2|0x10)
}

func paintComposerArrow(h, dc uintptr) {
	r := chatClient(h)
	fill(dc, r, paper)
	on := call(user.NewProc("IsWindowEnabled"), h) != 0
	state := uintptr(5) // ABS_DOWNNORMAL
	if !on {
		state = 8
	} else if composerArrowPressed && composerArrowInside {
		state = 7
	} else if composerArrowHot {
		state = 6
	}
	theme := call(composerArrowTheme.NewProc("OpenThemeData"), h, uintptr(unsafe.Pointer(wp("SCROLLBAR"))))
	if theme != 0 {
		result := call(composerArrowTheme.NewProc("DrawThemeBackground"), theme, dc, 1, state, uintptr(unsafe.Pointer(&r)), 0)
		call(composerArrowTheme.NewProc("CloseThemeData"), theme)
		if result == 0 {
			return
		}
	}
	flags := uintptr(1) // DFCS_SCROLLDOWN
	if !on {
		flags |= 0x100
	} else if composerArrowPressed && composerArrowInside {
		flags |= 0x200
	}
	call(user.NewProc("DrawFrameControl"), dc, uintptr(unsafe.Pointer(&r)), 3, flags)
}

func stopComposerArrow(h uintptr) {
	composerArrowPressed, composerArrowInside = false, false
	call(user.NewProc("KillTimer"), h, 1)
	if call(user.NewProc("GetCapture")) == h {
		call(user.NewProc("ReleaseCapture"))
	}
	invalidate(h)
}

func composerArrowProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x14:
		return 1
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), h, uintptr(unsafe.Pointer(&ps)))
		paintComposerArrow(h, dc)
		call(user.NewProc("EndPaint"), h, uintptr(unsafe.Pointer(&ps)))
		return 0
	case 0x201:
		if call(user.NewProc("IsWindowEnabled"), h) != 0 {
			composerArrowPressed, composerArrowInside = true, true
			call(user.NewProc("SetCapture"), h)
			scrollComposerInput(1)
			call(user.NewProc("SetTimer"), h, 1, 350, 0)
			invalidate(h)
		}
		return 0
	case 0x200:
		r := chatClient(h)
		x, y := int32(int16(l)), int32(int16(l>>16))
		inside := x >= 0 && y >= 0 && x < r.R && y < r.B
		if composerArrowInside != inside || !composerArrowHot {
			composerArrowInside, composerArrowHot = inside, inside
			invalidate(h)
		}
		track := struct {
			Size, Flags uint32
			Hwnd        uintptr
			Hover       uint32
		}{Flags: 2, Hwnd: h}
		track.Size = uint32(unsafe.Sizeof(track))
		call(user.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&track)))
		return 0
	case 0x2a3:
		composerArrowHot = false
		invalidate(h)
		return 0
	case 0x113:
		if composerArrowPressed && composerArrowInside {
			scrollComposerInput(1)
		}
		if composerArrowPressed {
			call(user.NewProc("SetTimer"), h, 1, 45, 0)
		}
		return 0
	case 0x202, 0x215, 0x1f:
		stopComposerArrow(h)
		return 0
	case 0xa:
		if w == 0 {
			stopComposerArrow(h)
		}
		invalidate(h)
	case 2:
		stopComposerArrow(h)
		composerArrowHot = false
	case 0x20a:
		scrollComposerWheel(w)
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
}
