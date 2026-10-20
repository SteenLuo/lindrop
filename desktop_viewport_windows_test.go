package main

import (
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func viewportTestWindow(t testing.TB) (uintptr, *chatViewport) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	oldNode, oldTasks, oldParts, oldList, oldScale, oldFont := guiNode, guiTasks, chatParts, guiControls[126], guiScale, guiFont
	oldSelection := chatSelection
	chatSelection = chatSelectionState{}
	guiScale = 1
	guiNode = &Node{id: "self"}
	guiFont = fontSize(13, 400)
	guiTasks = nil
	chatParts = nil
	for i := 0; i < 30; i++ {
		id := "viewport-" + string(rune('A'+i))
		task := Task{ID: id, Kind: "text", Direction: "in", Status: "done"}
		chatTextCache[id] = "你好😀 alpha beta\r\n第二行文字与文件传输。"
		p := chatMessageParts(task, 620)
		for _, part := range p {
			guiTasks = append(guiTasks, task)
			chatParts = append(chatParts, part)
		}
	}
	registerChatViewport()
	h := call(createWindow, 0, uintptr(unsafe.Pointer(wp("LinDropConversation"))), 0, 0x80200000, 0, 0, 620, 300, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal("viewport creation")
	}
	guiControls[126] = h
	t.Cleanup(func() {
		destroyWindow.Call(h)
		for _, v := range guiTasks {
			delete(chatTextCache, v.ID)
			delete(chatPartsCache, v.ID)
		}
		guiNode, guiTasks, chatParts, guiControls[126], guiScale, guiFont = oldNode, oldTasks, oldParts, oldList, oldScale, oldFont
		chatSelection = oldSelection
	})
	msg(h, 0xb, 0, 0)
	for range guiTasks {
		msg(h, 0x180, 0, 0)
	}
	msg(h, 0xb, 1, 0)
	return h, chatViews[h]
}
func TestPixelViewportGeometryWheelAndAnimation(t *testing.T) {
	h, v := viewportTestWindow(t)
	v.scroll(h, 57, false)
	var r rect
	msg(h, 0x198, 0, uintptr(unsafe.Pointer(&r)))
	if r.T != -57 {
		t.Fatal("viewport must retain sub-row pixel offset", r)
	}
	if msg(h, 0x1a9, 0, uintptr(2|(4<<16))) != 0 {
		t.Fatal("pixel hit test lost partially visible row")
	}
	v.scroll(h, 1e9, false)
	if v.Y != v.maximum(h) {
		t.Fatal("bottom overscroll")
	}
	v.scroll(h, 0, false)
	msg(h, 0x20a, uintptr(uint32(0xffc4)<<16), 0)
	half := v.Target
	msg(h, 0x20a, uintptr(uint32(0xffc4)<<16), 0)
	if math.Abs(v.Target-2*half) > .01 {
		t.Fatal("high resolution wheel deltas were dropped", half, v.Target)
	}
	v.scroll(h, 0, false)
	v.scroll(h, 117, true)
	v.Started = time.Now().Add(-40 * time.Millisecond)
	msg(h, 0x113, chatScrollTimer, 0)
	if v.Y <= 0 || v.Y >= 117 {
		t.Fatal("animation jumped to an entire row", v.Y)
	}
	v.Started = time.Now().Add(-200 * time.Millisecond)
	msg(h, 0x113, chatScrollTimer, 0)
	if v.Y != 117 || !v.Started.IsZero() {
		t.Fatal("animation did not settle")
	}
	if got := scrollInterpolation(100, 0, 50*time.Millisecond); got <= 0 || got >= 100 {
		t.Fatal("reverse animation overshoot", got)
	}
}
func TestDirectTextSelectionWithoutChildEdit(t *testing.T) {
	h, v := viewportTestWindow(t)
	task := guiTasks[0]
	part := chatPartAt(0)
	b := chatBubblePart(task, chatListWidth(), part)
	x, y := sc(b.X+14), sc(b.Y+10)
	msg(h, 0x201, 1, uintptr(uint16(x))|uintptr(uint16(y))<<16)
	if chatSelection.ID != task.ID || !chatSelection.Dragging {
		t.Fatal("press did not begin direct selection", chatSelection)
	}
	last := part.TextLines[len(part.TextLines)-1]
	endX := sc(b.X + b.W - 12)
	endY := sc(b.Y + 10 + (len(part.TextLines)-1)*21)
	msg(h, 0x200, 1, uintptr(uint16(endX))|uintptr(uint16(endY))<<16)
	msg(h, 0x202, 0, uintptr(uint16(endX))|uintptr(uint16(endY))<<16)
	if chatSelection.Anchor != 0 || chatSelection.Caret != last.Stops[len(last.Stops)-1] {
		t.Fatal("cross-line selection is wrong", chatSelection)
	}
	if got := selectedChatText(task); got != chatBody(task) {
		t.Fatalf("selection changed source text: %q", got)
	}
	if child := call(user.NewProc("GetWindow"), h, 5); child != 0 {
		t.Fatal("selection introduced an editor child")
	}
	before := chatSelection
	msg(h, 0x204, 0, uintptr(uint16(endX))|uintptr(uint16(endY))<<16)
	if chatSelection.Anchor != before.Anchor || chatSelection.Caret != before.Caret {
		t.Fatal("right press discarded selection")
	}
	if v.Selected != 0 {
		t.Fatal("message action selection lost")
	}
	chatSelection.Dragging = true
	chatSelection.Y = int(chatClient(h).B) - 1
	tickSelectionScroll(h, v)
	if v.Y != 0 {
		t.Fatal("single message selection scrolled into unrelated messages")
	}
	closeChatSelection()
	if _, _, ok := selectedChatRange(task.ID); ok {
		t.Fatal("escape did not clear selection")
	}
}
func TestSelectableLinesPreserveUnicodeAndWhitespace(t *testing.T) {
	old := guiScale
	guiScale = 1
	defer func() { guiScale = old }()
	for _, body := range []string{"你好😀 alpha\tbeta\r\n第二行", "one\n\nthree", "a\tb\tc", strings.Repeat("长文字😀", 60)} {
		lines, _ := selectableChatLines(body, 100, 0)
		plain, _ := wrapChatLines(body, 100, 0)
		if len(lines) != len(plain) {
			t.Fatal("selection changed wrapping")
		}
		last := 0
		for i, line := range lines {
			if line.Text != plain[i] {
				t.Fatal("selection changed displayed text")
			}
			if len(line.Stops) != len([]rune(line.Text))+1 {
				t.Fatal("missing rune caret")
			}
			for _, stop := range line.Stops {
				if stop < last || stop > len([]rune(body)) {
					t.Fatal("invalid source mapping", stop, last)
				}
				last = stop
			}
		}
	}
	a, b := chatWordRange("你好😀 alpha beta", 7)
	if got := string([]rune("你好😀 alpha beta")[a:b]); got != "alpha" {
		t.Fatal("word selection", got)
	}
	a, b = chatWordRange("你好😀", 2)
	if got := string([]rune("你好😀")[a:b]); got != "😀" {
		t.Fatal("emoji split", got)
	}
}
func TestViewportCachesRowsAndInvalidatesSelection(t *testing.T) {
	h, v := viewportTestWindow(t)
	vectorStart()
	if vectorToken == 0 {
		t.Fatal("GDI+")
	}
	defer func() { releaseVectorFonts(); call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	dc := call(user.NewProc("GetDC"), 0)
	defer call(user.NewProc("ReleaseDC"), 0, dc)
	bufferDC(dc, 620, 300, 0, 0, func(mem uintptr) {
		one := v.raster(mem, 0, 620, v.Heights[0])
		if one == nil {
			t.Fatal("row cache allocation")
		}
		builds := textRasterBuilds
		two := v.raster(mem, 0, 620, v.Heights[0])
		if one != two || textRasterBuilds != builds {
			t.Fatal("unchanged row rerasterized")
		}
		chatSelection = chatSelectionState{ID: guiTasks[0].ID, Anchor: 0, Caret: 2}
		three := v.raster(mem, 0, 620, v.Heights[0])
		if three == two {
			t.Fatal("selection did not invalidate row")
		}
		if pixel := call(gdi.NewProc("GetPixel"), three.DC, uintptr(sc(72)), uintptr(sc(20))); pixel != color(45, 112, 202) {
			t.Fatalf("direct highlight missing: %#x", pixel)
		}
		for i := 0; i < len(v.Heights); i++ {
			v.raster(mem, i, 2048, 255)
		}
		if v.Rows[0] != nil || v.Rows[len(v.Heights)-1] == nil {
			t.Fatal("row cache did not evict least recently used surfaces")
		}
		if v.Bytes > chatRowCacheLimit {
			t.Fatal("row cache exceeded memory bound")
		}
	})
	v.clearRows()
	if v.Bytes != 0 || len(v.Rows) != 0 {
		t.Fatal("cached GDI surfaces leaked")
	}
	_ = h
}
func TestTextRasterCacheRespectsMemoryAndStyle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vectorStart()
	defer func() { releaseVectorFonts(); call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	old := textRasterLimit
	textRasterLimit = 240000
	defer func() { textRasterLimit = old }()
	clearTextRasters()
	f := fontSize(13, 400)
	dc := call(user.NewProc("GetDC"), 0)
	defer call(user.NewProc("ReleaseDC"), 0, dc)
	bufferDC(dc, 600, 80, 0, 0, func(mem uintptr) {
		for _, c := range []uintptr{ink, paper, accent} {
			if !vectorText(mem, "中文😀 cache", rect{0, 0, 300, 21}, c, f, 0) {
				t.Fatal("text raster")
			}
		}
		if textRasterBytes > textRasterLimit || len(textRasters) == 0 {
			t.Fatal("text cache capacity")
		}
		before := textRasterBuilds
		vectorText(mem, "中文😀 cache", rect{10, 30, 310, 51}, accent, f, 0)
		if textRasterBuilds != before {
			t.Fatal("position incorrectly invalidates identical text")
		}
	})
}

func BenchmarkCachedViewportFrame(b *testing.B) {
	h, v := viewportTestWindow(b)
	vectorStart()
	defer func() { releaseVectorFonts(); call(vector.NewProc("GdiplusShutdown"), vectorToken); vectorToken = 0 }()
	dc := call(user.NewProc("GetDC"), 0)
	bufferDC(dc, 620, 300, 0, 0, func(mem uintptr) {
		for i := range v.Heights {
			v.raster(mem, i, 2048, 255)
		}
	})
	call(user.NewProc("ReleaseDC"), 0, dc)
	maximum := int(v.maximum(h))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.scroll(h, float64((i*7)%(maximum+1)), false)
		msg(h, 0xf, 0, 0)
	}
	b.StopTimer()
}

func TestViewportFileGestureDoesNotSelectText(t *testing.T) {
	h, v := viewportTestWindow(t)
	closeChatSelection()
	guiTasks[0].Kind = ""
	guiTasks[0].Media = "file"
	guiTasks[0].Title = "example.zip"
	chatParts[0] = chatTextPart{}
	b := chatBubblePart(guiTasks[0], chatListWidth(), nil)
	x, y := sc(b.X+20), sc(b.Y+20)
	pos := uintptr(uint16(x)) | uintptr(uint16(y))<<16
	msg(h, 0x201, 1, pos)
	if chatDragID != guiTasks[0].ID || chatSelection.Dragging {
		t.Fatal("file gesture became text selection")
	}
	msg(h, 0x1f, 0, 0)
	if chatDragID != "" {
		t.Fatal("file drag cancellation left stale state")
	}
	if v.Selected != 0 {
		t.Fatal("file message action is no longer selected")
	}
}

func TestSelectionAcrossExpandedMessageRows(t *testing.T) {
	h, v := viewportTestWindow(t)
	task := guiTasks[0]
	body := strings.Repeat("中文😀 alpha\tbeta\r\n", 36)
	chatTextCache[task.ID] = body
	chatExpanded[task.ID] = true
	defer delete(chatExpanded, task.ID)
	parts := chatMessageParts(task, chatListWidth())
	if len(parts) < 2 {
		t.Fatal("fixture needs multiple rows")
	}
	guiTasks = nil
	chatParts = nil
	for _, p := range parts {
		guiTasks = append(guiTasks, task)
		chatParts = append(chatParts, p)
	}
	msg(h, 0xb, 0, 0)
	msg(h, 0x184, 0, 0)
	for range parts {
		msg(h, 0x180, 0, 0)
	}
	msg(h, 0xb, 1, 0)
	chatSelection = chatSelectionState{ID: task.ID, Anchor: 0, Dragging: true}
	v.scroll(h, v.maximum(h), false)
	i := len(parts) - 1
	var row rect
	msg(h, 0x198, uintptr(i), uintptr(unsafe.Pointer(&row)))
	b := chatBubblePart(task, chatListWidth(), chatPartAt(i))
	y := int(row.T) + sc(b.Y+len(b.Lines)*21+15)
	chatSelection.Caret = selectionCaret(h, v, sc(b.X+b.W), y)
	if got := selectedChatText(task); got != body {
		t.Fatalf("expanded selection changed text or newlines: %q", got)
	}
	if call(user.NewProc("GetWindow"), h, 5) != 0 {
		t.Fatal("expanded selection created child editor")
	}
}

func TestProgressRefreshKeepsViewportAndUnchangedRows(t *testing.T) {
	h, v := viewportTestWindow(t)
	oldSelected, oldLast, oldDevice, oldLayout := guiSelected, guiLastTasks, chatLastDevice, chatLayoutWidth
	defer func() {
		guiSelected, guiLastTasks, chatLastDevice, chatLayoutWidth = oldSelected, oldLast, oldDevice, oldLayout
	}()
	guiSelected, guiLastTasks = "peer", ""
	all := append([]Task(nil), guiTasks...)
	for i := range all {
		all[i].Sender, all[i].Target = "peer", "self"
	}
	all[5].Kind, all[5].Media = "", "file"
	all[5].Status, all[5].Title, all[5].Total = "sending", "large.bin", 5*1024*1024*1024
	if !refreshChatList(all) {
		t.Fatal("initial structure was not built")
	}
	v.scroll(h, 73, false)
	initialWidth := chatClient(h).R
	initialHeight := &v.Heights[0]
	chatSelection = chatSelectionState{ID: all[0].ID, Anchor: 0, Caret: 2}
	// Zero-sized cache sentinels let us observe invalidation without drawing.
	keep, changed := &chatRowRaster{}, &chatRowRaster{}
	v.Rows[0], v.Rows[5] = keep, changed
	for i := 0; i < 60; i++ {
		all[5].Done += 1024 * 1024
		all[5].Rate = float64((i + 1) * 1024 * 1024)
		if refreshChatList(all) {
			t.Fatal("progress requested a layout rebuild")
		}
		if &v.Heights[0] != initialHeight || chatClient(h).R != initialWidth || v.Y != 73 {
			t.Fatal("progress changed viewport geometry", v.Y)
		}
		if v.Rows[0] != keep || v.Rows[5] != nil {
			t.Fatal("progress invalidated unrelated cache or kept stale row")
		}
		if chatSelection.ID != all[0].ID || chatSelection.Caret != 2 {
			t.Fatal("selection lost on progress")
		}
	}
	if guiTasks[5].Done != all[5].Done {
		t.Fatal("displayed task snapshot was not updated")
	}
	// Resetting content under WM_SETREDRAW must not hide and re-show the bar.
	msg(h, 0xb, 0, 0)
	msg(h, 0x184, 0, 0)
	msg(h, 0x197, 0, 0)
	if chatClient(h).R != initialWidth {
		t.Fatal("disabled redraw toggled scrollbar gutter")
	}
	for range guiTasks {
		msg(h, 0x180, 0, 0)
	}
	msg(h, 0xb, 1, 0)
	if chatClient(h).R != initialWidth {
		t.Fatal("rebuilt rows changed scrollbar gutter")
	}
}
