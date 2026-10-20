package main

import (
	"math"
	"syscall"
	"time"
	"unsafe"
)

// Only the conversation is pixel based. The device and share lists remain
// native LISTBOX controls. The small LB_* adapter keeps existing task actions.
type chatViewport struct {
	Heights, Tops   []int
	Dirty, Redraw   bool
	Selected        int
	Y, Target, From float64
	Started         time.Time
	Rows            map[int]*chatRowRaster
	Bytes           int
	Clock           uint64
}
type chatRowRaster struct {
	DC, Bitmap, Old uintptr
	W, H            int
	Hover, Focus    bool
	A, B            int
	Used            uint64
	Scale           float64
}

var chatViews = map[uintptr]*chatViewport{}

const chatScrollTimer = 0x723
const chatRowCacheLimit = 32 * 1024 * 1024

func registerChatViewport() {
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Style: 8, Proc: syscall.NewCallback(chatViewportProc), Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Name: wp("LinDropConversation")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
}
func (v *chatViewport) index() {
	if !v.Dirty && len(v.Tops) == len(v.Heights)+1 {
		return
	}
	v.Tops = make([]int, len(v.Heights)+1)
	for i, h := range v.Heights {
		v.Tops[i+1] = v.Tops[i] + h
	}
	v.Dirty = false
}
func (v *chatViewport) at(y int) int {
	v.index()
	lo, hi := 0, len(v.Heights)
	for lo < hi {
		m := (lo + hi) / 2
		if v.Tops[m+1] <= y {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}
func chatClient(h uintptr) rect {
	var r rect
	call(user.NewProc("GetClientRect"), h, uintptr(unsafe.Pointer(&r)))
	return r
}
func (v *chatViewport) maximum(h uintptr) float64 {
	v.index()
	n := v.Tops[len(v.Heights)] + sc(12) - int(chatClient(h).B)
	if n < 0 {
		n = 0
	}
	return float64(n)
}
func (v *chatViewport) clamp(h uintptr, y float64) float64 {
	return math.Max(0, math.Min(y, v.maximum(h)))
}
func scrollInterpolation(from, to float64, elapsed time.Duration) float64 {
	t := float64(elapsed) / float64(100*time.Millisecond)
	if t >= 1 {
		return to
	}
	if t <= 0 {
		return from
	}
	t = 1 - math.Pow(1-t, 3)
	return from + (to-from)*t
}
func (v *chatViewport) scrollbar(h uintptr) {
	v.index()
	r := chatClient(h)
	si := struct {
		Size, Mask uint32
		Min, Max   int32
		Page       uint32
		Pos, Track int32
	}{Size: 28, Mask: 7, Max: int32(v.Tops[len(v.Heights)] + sc(12) - 1), Page: uint32(r.B), Pos: int32(math.Round(v.Y))}
	call(user.NewProc("SetScrollInfo"), h, 1, uintptr(unsafe.Pointer(&si)), 1)
}
func (v *chatViewport) scroll(h uintptr, y float64, animated bool) {
	now := time.Now()
	if !v.Started.IsZero() {
		v.Y = scrollInterpolation(v.From, v.Target, now.Sub(v.Started))
	}
	v.Target = v.clamp(h, y)
	if animated && math.Abs(v.Y-v.Target) > .3 {
		v.From = v.Y
		v.Started = now
		call(user.NewProc("SetTimer"), h, chatScrollTimer, 15, 0)
	} else {
		v.Y = v.Target
		v.Started = time.Time{}
		call(user.NewProc("KillTimer"), h, chatScrollTimer)
	}
	v.scrollbar(h)
	if v.Redraw {
		invalidate(h)
	}
}
func (v *chatViewport) clearRows() {
	for i := range v.Rows {
		v.dropRow(i)
	}
}
func (v *chatViewport) dropRow(i int) {
	r := v.Rows[i]
	if r == nil {
		return
	}
	call(gdi.NewProc("SelectObject"), r.DC, r.Old)
	call(gdi.NewProc("DeleteObject"), r.Bitmap)
	call(gdi.NewProc("DeleteDC"), r.DC)
	v.Bytes -= r.W * r.H * 4
	delete(v.Rows, i)
}
func (v *chatViewport) raster(dc uintptr, index, width, height int) *chatRowRaster {
	if index >= len(guiTasks) {
		return nil
	}
	task := guiTasks[index]
	a, b := -1, -1
	if chatSelection.ID == task.ID {
		a, b = chatSelection.Anchor, chatSelection.Caret
	}
	hover, focus := chatCopyVisible(task), guiKeyboardMode && v.Selected == index && call(user.NewProc("GetFocus")) == guiControls[126]
	if r := v.Rows[index]; r != nil && r.W == width && r.H == height && r.Hover == hover && r.Focus == focus && r.A == a && r.B == b && r.Scale == guiScale {
		v.Clock++
		r.Used = v.Clock
		return r
	}
	v.dropRow(index)
	size := width * height * 4
	for v.Bytes+size > chatRowCacheLimit && len(v.Rows) > 0 {
		oldest := -1
		used := ^uint64(0)
		for i, r := range v.Rows {
			if r.Used < used {
				oldest, used = i, r.Used
			}
		}
		v.dropRow(oldest)
	}
	if width <= 0 || height <= 0 || size > chatRowCacheLimit {
		return nil
	}
	r := &chatRowRaster{W: width, H: height, Hover: hover, Focus: focus, A: a, B: b, Scale: guiScale}
	r.DC = call(gdi.NewProc("CreateCompatibleDC"), dc)
	r.Bitmap = call(gdi.NewProc("CreateCompatibleBitmap"), dc, uintptr(width), uintptr(height))
	if r.DC == 0 || r.Bitmap == 0 {
		if r.DC != 0 {
			call(gdi.NewProc("DeleteDC"), r.DC)
		}
		if r.Bitmap != 0 {
			call(gdi.NewProc("DeleteObject"), r.Bitmap)
		}
		return nil
	}
	r.Old = call(gdi.NewProc("SelectObject"), r.DC, r.Bitmap)
	state := uint32(0)
	if focus {
		state = 0x10
	}
	paintItemDirect(&drawItem{ID: 126, ItemID: uint32(index), State: state, DC: r.DC, Rect: rect{0, 0, int32(width), int32(height)}})
	v.Clock++
	r.Used = v.Clock
	v.Rows[index] = r
	v.Bytes += size
	return r
}
func paintChatViewport(h uintptr, v *chatViewport) {
	var ps paintStruct
	dc := call(user.NewProc("BeginPaint"), h, uintptr(unsafe.Pointer(&ps)))
	defer call(user.NewProc("EndPaint"), h, uintptr(unsafe.Pointer(&ps)))
	r := chatClient(h)
	if r.R <= 0 || r.B <= 0 {
		return
	}
	v.index()
	bufferDC(dc, int(r.R), int(r.B), 0, 0, func(mem uintptr) {
		fill(mem, r, canvas)
		offset := int(math.Round(v.Y))
		for i := v.at(offset); i < len(v.Heights) && v.Tops[i]-offset < int(r.B); i++ {
			y := v.Tops[i] - offset
			if row := v.raster(mem, i, int(r.R), v.Heights[i]); row != nil {
				call(gdi.NewProc("BitBlt"), mem, 0, uintptr(y), uintptr(row.W), uintptr(row.H), row.DC, 0, 0, 0xcc0020)
			} else if i < len(guiTasks) {
				paintItem(&drawItem{ID: 126, ItemID: uint32(i), DC: mem, Rect: rect{0, int32(y), r.R, int32(y + v.Heights[i])}})
			}
		}
	})
	v.scrollbar(h)
}
func (v *chatViewport) choose(h uintptr, index int) {
	if index < 0 || index >= len(v.Heights) {
		return
	}
	v.Selected = index
	call(sendMessage, call(user.NewProc("GetParent"), h), 0x111, 126|(1<<16), h)
	invalidate(h)
}
func (v *chatViewport) reveal(h uintptr, index int) {
	v.index()
	height := int(chatClient(h).B)
	y := int(v.Y)
	if v.Tops[index] < y {
		v.scroll(h, float64(v.Tops[index]), false)
	} else if v.Tops[index+1] > y+height {
		v.scroll(h, float64(v.Tops[index+1]-height), false)
	}
}
func chatViewportProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if m == 1 {
		chatViews[h] = &chatViewport{Selected: -1, Redraw: true, Rows: map[int]*chatRowRaster{}}
	}
	v := chatViews[h]
	if v == nil {
		return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
	}
	switch m {
	case 2:
		call(user.NewProc("KillTimer"), h, chatScrollTimer)
		v.clearRows()
		delete(chatViews, h)
		return 0
	case 0xb:
		v.Redraw = w != 0
		if v.Redraw {
			v.scroll(h, v.Y, false)
		}
		return 0
	case 5:
		v.clearRows()
		v.scroll(h, v.Y, false)
		return 0
	case 0x14:
		fillListBackground(h, w)
		return 1
	case 0xf:
		paintChatViewport(h, v)
		return 0
	case 0x180:
		i := len(v.Heights)
		height := sc(116)
		if i < len(guiTasks) {
			height = sc(chatRowHeightAt(guiTasks[i], i))
		}
		v.Heights = append(v.Heights, height)
		v.Dirty = true
		return uintptr(i)
	case 0x184:
		v.clearRows()
		v.Heights = nil
		v.Dirty = true
		v.Selected = -1
		return 0
	case 0x18b:
		return uintptr(len(v.Heights))
	case 0x188:
		return uintptr(int32(v.Selected))
	case 0x186:
		v.Selected = int(int32(w))
		invalidate(h)
		return 0
	case 0x18e:
		return uintptr(v.at(int(v.Y)))
	case 0x197:
		v.index()
		if int(w) < len(v.Heights) {
			v.scroll(h, float64(v.Tops[w]), false)
		}
		return 0
	case 0x1a1:
		if int(w) < len(v.Heights) {
			return uintptr(v.Heights[w])
		}
		return ^uintptr(0)
	case 0x1a0:
		if int(w) < len(v.Heights) {
			v.Heights[w] = int(l)
			v.Dirty = true
			v.dropRow(int(w))
			if v.Redraw {
				invalidate(h)
			}
		}
		return 0
	case 0x198:
		if int(w) >= len(v.Heights) || l == 0 {
			return ^uintptr(0)
		}
		v.index()
		r := chatClient(h)
		y := v.Tops[w] - int(math.Round(v.Y))
		*(*rect)(unsafe.Pointer(l)) = rect{0, int32(y), r.R, int32(y + v.Heights[w])}
		return 0
	case 0x1a9:
		x, y := int(int16(l)), int(int16(l>>16))
		r := chatClient(h)
		i := v.at(y + int(math.Round(v.Y)))
		outside := x < 0 || x >= int(r.R) || y < 0 || y >= int(r.B) || i >= len(v.Heights)
		if outside {
			return uintptr(i&0xffff) | 0x10000
		}
		return uintptr(i)
	case 0x20a:
		chatHoverID = ""
		var lines uint32 = 3
		call(user.NewProc("SystemParametersInfoW"), 0x68, 0, uintptr(unsafe.Pointer(&lines)), 0)
		step := float64(sc(18)) * float64(lines)
		if lines == 0xffffffff {
			step = float64(chatClient(h).B) * .9
		}
		v.scroll(h, v.Target-float64(int16(w>>16))*step/120, true)
		return 0
	case 0x115:
		chatHoverID = ""
		y := v.Target
		page := float64(chatClient(h).B) * .9
		switch w & 0xffff {
		case 0:
			y -= float64(sc(24))
		case 1:
			y += float64(sc(24))
		case 2:
			y -= page
		case 3:
			y += page
		case 4, 5:
			si := struct {
				Size, Mask uint32
				Min, Max   int32
				Page       uint32
				Pos, Track int32
			}{Size: 28, Mask: 0x10}
			call(user.NewProc("GetScrollInfo"), h, 1, uintptr(unsafe.Pointer(&si)))
			y = float64(si.Track)
		case 6:
			y = 0
		case 7:
			y = v.maximum(h)
		}
		v.scroll(h, y, false)
		return 0
	case 0x113:
		if w == chatSelectTimer {
			tickSelectionScroll(h, v)
			return 0
		}
		if w == chatScrollTimer {
			v.Y = v.clamp(h, scrollInterpolation(v.From, v.Target, time.Since(v.Started)))
			if time.Since(v.Started) >= 100*time.Millisecond {
				v.Y = v.Target
				v.Started = time.Time{}
				call(user.NewProc("KillTimer"), h, chatScrollTimer)
			}
			v.scrollbar(h)
			invalidate(h)
			return 0
		}
	case 0x87:
		return 0x81
	case 0x20:
		if l&0xffff == 1 {
			var p point
			call(user.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&p)))
			call(user.NewProc("ScreenToClient"), h, uintptr(unsafe.Pointer(&p)))
			i := v.at(int(p.Y) + int(v.Y))
			cursor := uintptr(32512)
			if _, ok := chatTextPoint(h, i, int(p.X), int(p.Y)); ok {
				cursor = 32513
			}
			call(user.NewProc("SetCursor"), call(user.NewProc("LoadCursorW"), 0, cursor))
			return 1
		}
	case 0x100:
		ctrl := call(user.NewProc("GetKeyState"), 17)&0x8000 != 0
		if ctrl && w == 67 {
			doCommand(172, 0)
			return 0
		}
		if ctrl && w == 65 {
			showChatSelection()
			return 0
		}
		if w == 27 {
			closeChatSelection()
			invalidate(h)
			return 0
		}
		if w == 13 {
			if t, ok := currentTask(); ok {
				if t.Kind == "text" {
					toggleChatText()
				} else {
					openChatAttachment(t)
				}
			}
			return 0
		}
		i := v.Selected
		if i < 0 {
			i = 0
		}
		switch w {
		case 38:
			i--
		case 40:
			i++
		case 33:
			v.scroll(h, v.Target-float64(chatClient(h).B)*.9, true)
			return 0
		case 34:
			v.scroll(h, v.Target+float64(chatClient(h).B)*.9, true)
			return 0
		case 36:
			i = 0
		case 35:
			i = len(v.Heights) - 1
		default:
			return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
		}
		if i < 0 {
			i = 0
		}
		if i >= len(v.Heights) {
			i = len(v.Heights) - 1
		}
		if i >= 0 {
			guiKeyboardMode = true
			closeChatSelection()
			v.choose(h, i)
			v.reveal(h, i)
		}
		return 0
	case wmDropFiles:
		postMessage.Call(guiMain, wmDropFiles, w, 0)
		return 0
	case 0x201, 0x203, 0x200, 0x202, 0x204, 0x205, 0x7b, 0x2a3, 0x215, 0x1f:
		if chatViewportInput(h, v, m, w, l) {
			return 0
		}
	case 7, 8:
		invalidate(h)
	}
	return call(user.NewProc("DefWindowProcW"), h, uintptr(m), w, l)
}
