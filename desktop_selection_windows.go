package main

import (
	"unicode"
	"unsafe"
)

type chatSelectableLine struct {
	Text  string
	Stops []int
}
type chatSelectionState struct {
	ID               string
	Anchor, Caret    int
	Dragging, Double bool
	X, Y             int
}

var chatSelection chatSelectionState

const chatSelectTimer = 0x724

// Stops map displayed caret positions back to original Unicode rune offsets.
// Wrapping introduces no copied newlines; CRLF and tabs retain their source.
func selectableChatLines(s string, width, limit int) ([]chatSelectableLine, bool) {
	raw := []rune(s)
	out := []chatSelectableLine{}
	line := []rune{}
	stops := []int{0}
	used := 0
	finish := func() {
		out = append(out, chatSelectableLine{string(line), append([]int(nil), stops...)})
		line = nil
		used = 0
		stops = []int{stops[len(stops)-1]}
	}
	for i := 0; i < len(raw); i++ {
		r := raw[i]
		if r == '\r' || r == '\n' {
			finish()
			if limit > 0 && len(out) >= limit {
				return out, true
			}
			if r == '\r' && i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			stops[0] = i + 1
			continue
		}
		count := 1
		if r == '\t' {
			count = 4
			r = ' '
		}
		for j := 0; j < count; j++ {
			n := chatLineWidth(r)
			if used+n > width && len(line) > 0 {
				finish()
				if limit > 0 && len(out) >= limit {
					return out, true
				}
			}
			line = append(line, r)
			used += n
			end := i + 1
			if count == 4 && j < 2 {
				end = i
			}
			stops = append(stops, end)
		}
	}
	finish()
	return out, false
}
func closeChatSelection() {
	chatSelection = chatSelectionState{}
	if h := guiControls[126]; h != 0 {
		call(user.NewProc("KillTimer"), h, chatSelectTimer)
		invalidate(h)
	}
}
func selectedChatRange(id string) (int, int, bool) {
	if chatSelection.ID != id {
		return 0, 0, false
	}
	a, b := chatSelection.Anchor, chatSelection.Caret
	if a > b {
		a, b = b, a
	}
	return a, b, a != b
}
func selectedChatText(t Task) string {
	r := []rune(chatBody(t))
	a, b, ok := selectedChatRange(t.ID)
	if ok && a >= 0 && b <= len(r) {
		return string(r[a:b])
	}
	return chatBody(t)
}
func showChatSelection() {
	if t, ok := currentTask(); ok && t.Kind == "text" {
		chatSelection = chatSelectionState{ID: t.ID, Caret: len([]rune(chatBody(t)))}
		invalidate(guiControls[126])
	}
}
func chatWordRange(s string, at int) (int, int) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, 0
	}
	if at >= len(r) {
		at = len(r) - 1
	}
	if at < 0 {
		at = 0
	}
	if unicode.IsSpace(r[at]) && at > 0 && !unicode.IsSpace(r[at-1]) {
		at--
	}
	word := func(c rune) bool {
		return !unicode.Is(unicode.Han, c) && (unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_')
	}
	a, b := at, at+1
	if word(r[at]) {
		for a > 0 && word(r[a-1]) {
			a--
		}
		for b < len(r) && word(r[b]) {
			b++
		}
	}
	for a > 0 && (unicode.IsMark(r[a]) || r[a] == 0xfe0f) {
		a--
	}
	for b < len(r) && (unicode.IsMark(r[b]) || r[b] == 0xfe0f) {
		b++
	}
	return a, b
}
func selectionLine(part *chatTextPart, index int) (chatSelectableLine, bool) {
	if part != nil && index < len(part.TextLines) {
		return part.TextLines[index], true
	}
	return chatSelectableLine{}, false
}
func paintSelectableLine(dc uintptr, t Task, part *chatTextPart, index int, text string, x, y, width int) {
	label(dc, text, x, y, width, 21, ink, guiFont)
	a, b, ok := selectedChatRange(t.ID)
	line, has := selectionLine(part, index)
	if !ok || !has {
		return
	}
	chars := []rune(line.Text)
	left, right, pos := -1, -1, 0
	for i, c := range chars {
		next := pos + chatLineWidth(c)
		if line.Stops[i] < b && line.Stops[i+1] > a {
			if left < 0 {
				left = pos
			}
			right = next
		}
		pos = next
	}
	if left < 0 || right <= left {
		return
	}
	r := rct(x+left, y, right-left, 21)
	fill(dc, r, color(45, 112, 202))
	saved := call(gdi.NewProc("SaveDC"), dc)
	call(gdi.NewProc("IntersectClipRect"), dc, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B))
	label(dc, text, x, y, width, 21, paper, guiFont)
	call(gdi.NewProc("RestoreDC"), dc, saved)
}
func chatTextPoint(h uintptr, index, x, y int) (int, bool) {
	if index < 0 || index >= len(guiTasks) {
		return 0, false
	}
	t := guiTasks[index]
	if t.Kind != "text" {
		return 0, false
	}
	p := chatPartAt(index)
	if p == nil || len(p.TextLines) == 0 {
		return 0, false
	}
	b := chatBubblePart(t, chatListWidth(), p)
	var row rect
	msg(h, 0x198, uintptr(index), uintptr(unsafe.Pointer(&row)))
	pad := 0
	if b.First {
		pad = 9
	}
	top := int(row.T) + sc(b.Y+pad)
	localY := y - top
	if x < sc(b.X) || x >= sc(b.X+b.W) || localY < 0 || localY >= sc(len(p.TextLines)*21) {
		return 0, false
	}
	line := p.TextLines[localY/sc(21)]
	px := x - sc(b.X+14)
	pos := 0
	for i, c := range []rune(line.Text) {
		next := pos + sc(chatLineWidth(c))
		if px < (pos+next)/2 {
			return line.Stops[i], true
		}
		pos = next
	}
	return line.Stops[len(line.Stops)-1], true
}
func selectionCaret(h uintptr, v *chatViewport, x, y int) int {
	v.index()
	first, last := -1, -1
	for i, t := range guiTasks {
		if t.ID == chatSelection.ID {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return chatSelection.Caret
	}
	world := y + int(v.Y)
	i := v.at(world)
	if i < first {
		p := chatPartAt(first)
		if p != nil && len(p.TextLines) > 0 {
			return p.TextLines[0].Stops[0]
		}
		return 0
	}
	if i > last {
		i = last
	}
	if i < first {
		i = first
	}
	p := chatPartAt(i)
	if p == nil || len(p.TextLines) == 0 {
		return chatSelection.Caret
	}
	b := chatBubblePart(guiTasks[i], chatListWidth(), p)
	pad := 0
	if b.First {
		pad = 9
	}
	lineIndex := (world - v.Tops[i] - sc(b.Y+pad)) / sc(21)
	if lineIndex < 0 {
		lineIndex = 0
	}
	if lineIndex >= len(p.TextLines) {
		lineIndex = len(p.TextLines) - 1
	}
	line := p.TextLines[lineIndex]
	pos := 0
	px := x - sc(b.X+14)
	for j, c := range []rune(line.Text) {
		next := pos + sc(chatLineWidth(c))
		if px < (pos+next)/2 {
			return line.Stops[j]
		}
		pos = next
	}
	return line.Stops[len(line.Stops)-1]
}
func tickSelectionScroll(h uintptr, v *chatViewport) {
	if !chatSelection.Dragging {
		call(user.NewProc("KillTimer"), h, chatSelectTimer)
		return
	}
	height := int(chatClient(h).B)
	delta := 0
	if chatSelection.Y < sc(20) {
		delta = -sc(10)
	} else if chatSelection.Y > height-sc(20) {
		delta = sc(10)
	}
	if delta != 0 {
		v.index()
		first, last := -1, -1
		for i, task := range guiTasks {
			if task.ID == chatSelection.ID {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			return
		}
		if delta > 0 && v.Tops[last+1] <= int(v.Y)+height-sc(20) {
			return
		}
		if delta < 0 && v.Tops[first] >= int(v.Y)+sc(20) {
			return
		}
		v.scroll(h, v.Y+float64(delta), false)
		chatSelection.Caret = selectionCaret(h, v, chatSelection.X, chatSelection.Y)
		invalidate(h)
	}
}
func chatViewportInput(h uintptr, v *chatViewport, m uint32, w, l uintptr) bool {
	x, y := int(int16(l)), int(int16(l>>16))
	hit := msg(h, 0x1a9, 0, l)
	index := int(hit & 0xffff)
	valid := hit>>16 == 0 && index < len(guiTasks)
	if m == 0x201 || m == 0x203 {
		guiKeyboardMode = false
		call(user.NewProc("SetFocus"), h)
		if !valid {
			closeChatSelection()
			return true
		}
		v.choose(h, index)
		if at, ok := chatTextPoint(h, index, x, y); ok {
			chatSelection = chatSelectionState{ID: guiTasks[index].ID, Anchor: at, Caret: at, Dragging: m == 0x201, X: x, Y: y}
			if m == 0x203 {
				chatSelection.Anchor, chatSelection.Caret = chatWordRange(chatBody(guiTasks[index]), at)
				chatSelection.Double = true
			} else {
				call(user.NewProc("SetCapture"), h)
			}
			invalidate(h)
			return true
		}
		closeChatSelection()
		if m == 0x201 {
			chatDragMessage(h, m, w, l)
			call(user.NewProc("SetCapture"), h)
		}
		return true
	}
	if m == 0x200 {
		if chatSelection.Dragging {
			chatSelection.X, chatSelection.Y = x, y
			chatSelection.Caret = selectionCaret(h, v, x, y)
			call(user.NewProc("SetTimer"), h, chatSelectTimer, 30, 0)
			invalidate(h)
			return true
		}
		if chatDragMessage(h, m, w, l) {
			return true
		}
		trackChatHover(h, l)
		return true
	}
	if m == 0x202 {
		selecting := chatSelection.Dragging
		chatSelection.Dragging = false
		call(user.NewProc("KillTimer"), h, chatSelectTimer)
		if call(user.NewProc("GetCapture")) == h {
			call(user.NewProc("ReleaseCapture"))
		}
		if selecting || chatSelection.Double {
			chatSelection.Double = false
			return true
		}
		if chatDragMessage(h, m, w, l) {
			return true
		}
		if valid {
			var row rect
			msg(h, 0x198, uintptr(index), uintptr(unsafe.Pointer(&row)))
			id := chatHitPart(guiTasks[index], chatListWidth(), x, y-int(row.T), chatPartAt(index))
			if id != 0 {
				doCommand(id, 0)
			}
		}
		return true
	}
	if m == 0x204 {
		if valid {
			if chatSelection.ID != guiTasks[index].ID {
				closeChatSelection()
			}
			v.choose(h, index)
		}
		return true
	}
	if m == 0x205 || m == 0x7b {
		taskContextMenu(guiMain)
		return true
	}
	if m == 0x2a3 {
		old := chatHoverID
		chatHoverID = ""
		invalidateChatCopy(old)
		return true
	}
	if m == 0x215 || m == 0x1f {
		chatSelection.Dragging = false
		call(user.NewProc("KillTimer"), h, chatSelectTimer)
		chatDragMessage(h, m, w, l)
		return true
	}
	return false
}
