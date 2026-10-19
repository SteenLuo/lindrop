package main

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var chatDrafts = map[string]string{}
var chatDraftDevice string
var chatTextCache = map[string]string{}
var chatComposeProc uintptr
var chatComposing, chatSending bool
var chatFeedback string
var chatFeedbackUntil time.Time
var chatLastDevice string
var chatWidths = map[rune]int{}
var chatHoverID string
var chatExpanded = map[string]bool{}
var chatParts []chatTextPart
var chatPartsCache = map[string][]chatTextPart{}
var chatKeepAnchor string

type chatTextPart struct {
	ID                          string
	MediaOffset, MediaHeight    int
	Lines                       []string
	First, Last, More, Expanded bool
	Width                       int
}

// A pull is an incoming download, although the requester is stored as Sender.
func conversationPeer(t Task, self string) string {
	if t.Direction == "in" {
		if t.Pull {
			return t.Target
		}
		if t.Target == self {
			return t.Sender
		}
	}
	if t.Direction == "out" && t.Sender == self {
		return t.Target
	}
	return ""
}

func conversationTasks(all []Task, self, peer string) []Task {
	out := []Task{}
	if peer == "" || peer == self {
		return out
	}
	for _, t := range all {
		if conversationPeer(t, self) == peer {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].CreatedNS, out[j].CreatedNS
		if a == 0 {
			a = out[i].Created * 1e9
		}
		if b == 0 {
			b = out[j].Created * 1e9
		}
		if a == b {
			return out[i].ID < out[j].ID
		}
		return a < b
	})
	return out
}

func switchChatDraft(peer string) {
	if peer == chatDraftDevice {
		return
	}
	if chatDraftDevice != "" {
		chatDrafts[chatDraftDevice] = text(guiControls[170])
	}
	chatDraftDevice = peer
	set(guiControls[170], chatDrafts[peer])
}

func createChatComposer() {
	h := themedControl("EDIT", "", 170, 0x4|0x40|0x1000)
	msg(h, 0xc5, maxTextBytes, 0)
	cue(170, "输入文字，或将文件拖到此窗口")
	acceptDesktopDrops(h)
	chatComposeProc = call(user.NewProc("SetWindowLongW"), h, uintptr(0xfffffffc), syscall.NewCallback(chatEditProc))
	themedControl("BUTTON", "发送", 171, 0)
}

func chatEditProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if m == wmDropFiles {
		postMessage.Call(guiMain, wmDropFiles, w, 0)
		return 0
	}
	if m == 0x10d {
		chatComposing = true
	}
	if m == 0x10e {
		chatComposing = false
	}
	if m == 0x100 && w == 13 && !chatComposing && int16(call(user.NewProc("GetKeyState"), 0x11)) < 0 {
		sendChatText()
		return 0
	}
	if m == 0x102 && (w == 10 || (w == 13 && int16(call(user.NewProc("GetKeyState"), 0x11)) < 0 && !chatComposing)) {
		return 0
	}
	if m == 0x87 {
		return 4 | 0x80
	} // EDIT owns Enter; Chinese IME confirmation never sends.
	return call(user.NewProc("CallWindowProcW"), chatComposeProc, hwnd, uintptr(m), w, l)
}

func sendChatText() {
	if chatSending {
		return
	}
	d, ok := currentDevice()
	if e := desktopSendTarget(d, ok, guiNode.id); e != nil {
		showError(e.Error())
		return
	}
	body := text(guiControls[170])
	if e := validateText(body); e != nil {
		showError(e.Error())
		return
	}
	chatSending = true
	enabled(171, false)
	go func() {
		id, e := guiNode.SendText(body, d.ID)
		queue(func() {
			chatSending = false
			if e != nil {
				showError(e.Error())
			} else {
				chatTextCache[id] = body
				if chatDraftDevice == d.ID && text(guiControls[170]) == body {
					set(guiControls[170], "")
					chatDrafts[d.ID] = ""
				} else if chatDraftDevice != d.ID && chatDrafts[d.ID] == body {
					chatDrafts[d.ID] = ""
				}
				set(guiControls[190], "文字已发送")
			}
			refreshGUI()
			updateContext()
			invalidate(guiMain)
		})
	}()
}

func chatBody(t Task) string {
	if s, ok := chatTextCache[t.ID]; ok {
		return s
	}
	if t.Kind != "text" || (t.Direction != "out" && t.Status != "done") {
		return ""
	}
	s, e := guiNode.ReadTaskText(t.ID)
	if e == nil {
		chatTextCache[t.ID] = s
	}
	return s
}

func chatLineWidth(r rune) int {
	if n, ok := chatWidths[r]; ok {
		return n
	}
	dc := call(user.NewProc("GetDC"), guiMain)
	old := call(gdi.NewProc("SelectObject"), dc, guiFont)
	units := utf16.Encode([]rune{r})
	var size point
	call(gdi.NewProc("GetTextExtentPoint32W"), dc, uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)), uintptr(unsafe.Pointer(&size)))
	call(gdi.NewProc("SelectObject"), dc, old)
	call(user.NewProc("ReleaseDC"), guiMain, dc)
	n := int(float64(size.X)/guiScale + 0.999)
	if n < 1 {
		n = 7
	}
	chatWidths[r] = n
	return n
}

func wrapChatText(s string, width int) ([]string, bool) {
	return wrapChatLines(s, width, 5)
}

func wrapChatLines(s string, width, limit int) ([]string, bool) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	lines := []string{}
	line := []rune{}
	used := 0
	for _, r := range s {
		n := 0
		if r != '\n' {
			n = chatLineWidth(r)
		}
		if r == '\n' || used+n > width && len(line) > 0 {
			lines = append(lines, string(line))
			line = nil
			used = 0
			if limit > 0 && len(lines) >= limit {
				return lines, true
			}
			if r == '\n' {
				continue
			}
		}
		line = append(line, r)
		used += n
	}
	lines = append(lines, string(line))
	return lines, false
}

func chatTextWidth(width int) int {
	width -= 112
	if width > 500 {
		width = 500
	}
	if width < 180 {
		width = 180
	}
	return width
}

// Expanded messages are split into small owner-drawn rows. This keeps every
// LISTBOX item below the Win7 height limit while preserving the entire text.
func chatMessageParts(t Task, width int) []chatTextPart {
	if isChatMedia(t) && chatMediaReady(t) {
		return chatMediaParts(t)
	}
	body := chatBody(t)
	if t.Kind != "text" || body == "" {
		return nil
	}
	key := fmt.Sprintf("%s:%d:%g:%t", t.ID, width, guiScale, chatExpanded[t.ID])
	if parts, ok := chatPartsCache[key]; ok {
		return parts
	}
	limit := 5
	if chatExpanded[t.ID] {
		limit = 0
	}
	lines, more := wrapChatLines(body, chatTextWidth(width)-28, limit)
	longest := 0
	for _, line := range lines {
		n := 0
		for _, r := range line {
			n += chatLineWidth(r)
		}
		if n > longest {
			longest = n
		}
	}
	bw := longest + 28
	if bw < 48 {
		bw = 48
	}
	if bw > chatTextWidth(width) {
		bw = chatTextWidth(width)
	}
	expanded := chatExpanded[t.ID]
	if more || expanded {
		if bw < 100 {
			bw = 100
		}
	}
	// LB_SETITEMHEIGHT is limited in physical pixels, including at 125–200% DPI.
	linesPerPart := 8
	if guiScale > 0 {
		linesPerPart = (int(255/guiScale) - 79) / 21
		if linesPerPart > 8 {
			linesPerPart = 8
		}
		if linesPerPart < 1 {
			linesPerPart = 1
		}
	}
	parts := []chatTextPart{}
	for start := 0; start < len(lines); start += linesPerPart {
		end := start + linesPerPart
		if end > len(lines) {
			end = len(lines)
		}
		parts = append(parts, chatTextPart{ID: t.ID, Lines: lines[start:end], First: start == 0, Last: end == len(lines), More: more, Expanded: expanded, Width: bw})
	}
	chatPartsCache[key] = parts
	return parts
}

type chatBubble struct {
	X, Y, W, H                        int
	MediaOffset                       int
	Lines                             []string
	More, Text, First, Last, Expanded bool
}

func chatBubbleLayout(t Task, width int) chatBubble { return chatBubblePart(t, width, nil) }
func chatBubblePart(t Task, width int, part *chatTextPart) chatBubble {
	max := chatTextWidth(width)
	b := chatBubble{X: 58, Y: 10, W: max, H: 100, First: true, Last: true}
	if b.W > 320 {
		b.W = 320
	}
	if t.Status == "done" || t.Status == "rejected" {
		b.H = 88
	}
	if parts := chatMessageParts(t, width); t.Kind == "text" && len(parts) > 0 {
		p := parts[0]
		if part != nil && part.ID == t.ID {
			p = *part
		}
		b.Text = true
		b.Lines = p.Lines
		b.More = p.More
		b.First = p.First
		b.Last = p.Last
		b.Expanded = p.Expanded
		b.W = p.Width
		b.H = len(p.Lines) * 21
		if b.First {
			b.H += 9
		} else {
			b.Y = 0
		}
		if b.Last {
			b.H += 9
			if b.More || b.Expanded {
				b.H += 24
			}
		}
	}
	if !b.Text && isChatMedia(t) && chatMediaReady(t) {
		b.W = 260
		if b.W > max {
			b.W = max
		}
		parts := chatMediaParts(t)
		p := parts[0]
		if part != nil && part.ID == t.ID {
			p = *part
		}
		b.H = p.MediaHeight
		b.MediaOffset = p.MediaOffset
		b.First = p.First
		b.Last = p.Last
		if !b.First {
			b.Y = 0
		}
	}
	if t.Direction == "out" {
		b.X = width - 58 - b.W
	}
	return b
}

func chatPartAt(index int) *chatTextPart {
	if index >= 0 && index < len(chatParts) && chatParts[index].ID != "" {
		return &chatParts[index]
	}
	return nil
}
func chatListWidth() int {
	width := guiWidth - 350
	if guiControls[126] != 0 {
		var r rect
		call(user.NewProc("GetClientRect"), guiControls[126], uintptr(unsafe.Pointer(&r)))
		if r.R > 0 {
			width = int(float64(r.R) / guiScale)
		}
	}
	if width < 300 {
		width = 700
	}
	return width
}
func chatRowHeight(t Task) int { return chatRowHeightAt(t, -1) }
func chatRowHeightAt(t Task, index int) int {
	b := chatBubblePart(t, chatListWidth(), chatPartAt(index))
	height := b.Y + b.H
	if b.Last {
		height += 27
	}
	return height
}
func chatActionRect(t Task, b chatBubble) rect {
	if b.Text {
		x := b.X
		if t.Direction == "out" {
			x += b.W - 42
		}
		return rct(x, b.Y+b.H+2, 42, 21)
	}
	return rct(b.X+14, b.Y+b.H-31, 76, 23)
}

// Status occupies a row-level area instead of inheriting a tiny text bubble's
// width. Reserve copy space only while the copy control is actually visible.
func chatTextStatusRect(t Task, b chatBubble, rowWidth int, copying bool) rect {
	left, right := b.X, rowWidth-16
	if t.Direction == "out" {
		right = b.X + b.W
		if copying {
			right -= 50
		}
		left = right - 260
		if left < 16 {
			left = 16
		}
	} else {
		if copying {
			left += 50
		}
		if right > left+260 {
			right = left + 260
		}
	}
	return rct(left, b.Y+b.H+2, right-left, 20)
}

func chatSecondaryRect(t Task, b chatBubble) rect {
	if b.Text {
		return rct(b.X+12, b.Y+b.H-27, 86, 24)
	}
	return rct(b.X+102, b.Y+b.H-31, 50, 23)
}
func chatCopyVisible(t Task) bool {
	return chatHoverID == t.ID || guiKeyboardMode && call(user.NewProc("GetFocus")) == guiControls[126] && func() bool { v, ok := currentTask(); return ok && v.ID == t.ID }()
}

func paintChatItem(dc uintptr, t Task, x, y, width, index int) {
	b := chatBubblePart(t, width, chatPartAt(index))
	b.X += x
	b.Y += y
	outgoing := t.Direction == "out"
	ax := x + 10
	if outgoing {
		ax = x + width - 43
	}
	if b.First {
		rounded(dc, rct(ax, y+11, 30, 30), paper, lineColor, 9)
		kind := "computer"
		if strings.HasPrefix(conversationPeer(t, guiNode.id), "web:") && !outgoing {
			kind = "phone"
		}
		icon(dc, kind, ax+7, y+18, 16, accent)
	}
	bg, edge := paper, lineColor
	if outgoing {
		bg = color(224, 237, 222)
		edge = bg
	}
	if (b.Text || isChatMedia(t)) && !(b.First && b.Last) {
		rounded(dc, rct(b.X, b.Y, b.W, b.H), bg, bg, 10)
		if !b.First {
			fill(dc, rct(b.X, b.Y, b.W, 12), bg)
		}
		if !b.Last {
			fill(dc, rct(b.X, b.Y+b.H-12, b.W, 12), bg)
		}
	} else {
		rounded(dc, rct(b.X, b.Y, b.W, b.H), bg, edge, 10)
	}
	if b.Text {
		pad := 0
		if b.First {
			pad = 9
		}
		for i, line := range b.Lines {
			label(dc, line, b.X+14, b.Y+pad+i*21, b.W-24, 21, ink, guiFont)
		}
		if b.Last && (b.More || b.Expanded) {
			toggle := "展开全文"
			if b.Expanded {
				toggle = "收起"
			}
			label(dc, toggle, b.X+14, b.Y+b.H-26, b.W-20, 21, accent, guiSmall)
		}
		if b.Last && chatCopyVisible(t) {
			r := chatActionRect(t, b)
			rounded(dc, r, paper, lineColor, 5)
			centerText(dc, "复制", r, muted, fontSize(10, 400))
		}
		if b.Last && t.Status != "done" {
			status := taskStatus(t)
			if t.Error != "" {
				status = t.Error
			}
			r := chatTextStatusRect(t, b, x+width, chatCopyVisible(t))
			if outgoing {
				rightLabel(dc, status, r, muted, guiTiny)
			} else {
				label(dc, status, int(float64(r.L)/guiScale), b.Y+b.H+2, int(float64(r.R-r.L)/guiScale), 20, muted, guiTiny)
			}
		}
		return
	} else if paintChatMedia(dc, t, b) {
		if b.Last && t.Status != "done" {
			rightLabel(dc, taskStatus(t), chatTextStatusRect(t, b, x+width, false), muted, guiTiny)
		}
		return
	} else {
		rounded(dc, rct(b.X+16, b.Y+15, 36, 40), tint, tint, 8)
		if t.Folder {
			icon(dc, "folder", b.X+20, b.Y+20, 28, accent)
		} else {
			paintDocumentGlyph(dc, b.X+22, b.Y+20)
		}
		title := taskDisplayTitle(t)
		if t.Kind == "text" {
			title = "文字尚未接收完成"
		}
		label(dc, title, b.X+64, b.Y+13, b.W-80, 22, ink, guiMedium)
		label(dc, formatSize(t.Total)+" · "+taskStatus(t), b.X+64, b.Y+37, b.W-80, 18, muted, guiSmall)
		if t.Status != "done" && t.Status != "rejected" && t.Status != "waiting" {
			bw := b.W - 32
			rounded(dc, rct(b.X+16, b.Y+65, bw, 3), lineColor, lineColor, 2)
			if t.Total > 0 {
				p := int(float64(bw) * float64(t.Done) / float64(t.Total))
				if p > bw {
					p = bw
				}
				if p > 0 {
					rounded(dc, rct(b.X+16, b.Y+65, p, 3), accent, accent, 2)
				}
			}
		}
	}
	action := ""
	if _, s := taskAction(t); s != "" {
		action = s
	}

	if action != "" {
		if t.Status == "waiting" {
			rounded(dc, chatActionRect(t, b), accent, accent, 6)
			centerText(dc, action, chatActionRect(t, b), paper, guiSmall)
		} else {
			label(dc, action, b.X+16, b.Y+b.H-30, 80, 20, accent, guiSmall)
		}
	}
	if t.Status == "waiting" {
		label(dc, "拒绝", b.X+104, b.Y+b.H-30, 64, 20, muted, guiSmall)
	}
	status := taskStatus(t)
	if t.Status == "done" {
		status = "✓ 已完成"
	}
	if t.Error != "" {
		status = t.Error
	}
	if t.Rate > 0 && t.Status != "done" {
		status = progressSize(t.Done) + " / " + progressSize(t.Total) + " · " + formatSize(int64(t.Rate)) + "/s"
	}
	if action == "" {
		label(dc, status, b.X+16, b.Y+b.H-30, b.W-32, 20, muted, guiTiny)
	} else {
		rightLabel(dc, status, rct(b.X+176, b.Y+b.H-30, b.W-192, 20), muted, guiTiny)
	}
}

func chatHit(t Task, width, x, y int) int { return chatHitPart(t, width, x, y, nil) }
func chatHitPart(t Task, width, x, y int, part *chatTextPart) int {
	b := chatBubblePart(t, width, part)
	inside := func(r rect) bool { return x >= int(r.L) && x < int(r.R) && y >= int(r.T) && y < int(r.B) }
	if b.Text {
		if b.Last && chatCopyVisible(t) && inside(chatActionRect(t, b)) {
			return 172
		}
		if b.Last && (b.More || b.Expanded) && inside(chatSecondaryRect(t, b)) {
			return 173
		}
		return 0
	}
	if isChatMedia(t) && chatMediaReady(t) && inside(rct(b.X, b.Y, b.W, b.H)) {
		return 176
	}
	if inside(chatActionRect(t, b)) {
		id, _ := taskAction(t)
		return id
	}
	if t.Status == "waiting" && inside(chatSecondaryRect(t, b)) {
		return 129
	}
	return 0
}

func trackChatHover(hwnd uintptr, l uintptr) {
	id := ""
	hit := msg(hwnd, 0x1a9, 0, l)
	if hit>>16 == 0 {
		index := int(hit & 0xffff)
		if index < len(guiTasks) && guiTasks[index].Kind == "text" {
			var row rect
			msg(hwnd, 0x198, uintptr(index), uintptr(unsafe.Pointer(&row)))
			b := chatBubblePart(guiTasks[index], chatListWidth(), chatPartAt(index))
			x, y := int(int16(l&0xffff)), int(int16(l>>16))-int(row.T)
			if x >= sc(b.X-2) && x < sc(b.X+b.W+2) && y >= sc(b.Y) && y < sc(b.Y+b.H+25) {
				id = guiTasks[index].ID
			}
		}
	}
	if chatHoverID != id {
		old := chatHoverID
		chatHoverID = id
		invalidateChatCopy(old)
		invalidateChatCopy(id)
	}
	track := struct {
		Size, Flags uint32
		Hwnd        uintptr
		Hover       uint32
	}{16, 2, hwnd, 0}
	call(user.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&track)))
}

func toggleChatText() {
	t, ok := currentTask()
	if !ok || t.Kind != "text" || chatBody(t) == "" {
		return
	}
	parts := chatMessageParts(t, chatListWidth())
	if !chatExpanded[t.ID] && (len(parts) == 0 || !parts[0].More) {
		doCommand(172, 0)
		return
	}
	chatExpanded[t.ID] = !chatExpanded[t.ID]
	chatKeepAnchor = t.ID
	guiLastTasks = ""
	chatHoverID = ""
	refreshGUI()
	layout()
}

func refreshChatList(all []Task) bool {
	tasks := conversationTasks(all, guiNode.id, guiSelected)
	raw := fmt.Sprintf("%s:%d:%v", guiSelected, guiWidth, tasks)
	if raw == guiLastTasks {
		return false
	}
	h := guiControls[126]
	top := int(msg(h, 0x18e, 0, 0))
	nearBottom := len(guiTasks) == 0
	if len(guiTasks) > 0 {
		var r, client rect
		msg(h, 0x198, uintptr(len(guiTasks)-1), uintptr(unsafe.Pointer(&r)))
		call(user.NewProc("GetClientRect"), h, uintptr(unsafe.Pointer(&client)))
		nearBottom = r.B <= client.B+int32(sc(30))
	}
	selected := ""
	if t, ok := currentTask(); ok {
		selected = t.ID
	}
	changedDevice := chatLastDevice != guiSelected
	guiTasks = nil
	chatParts = nil
	for _, t := range tasks {
		parts := chatMessageParts(t, chatListWidth())
		if len(parts) == 0 {
			guiTasks = append(guiTasks, t)
			chatParts = append(chatParts, chatTextPart{})
		} else {
			for _, part := range parts {
				guiTasks = append(guiTasks, t)
				chatParts = append(chatParts, part)
			}
		}
	}
	// Text integrity is checked once before measuring rows; failed reads remain retryable.
	for _, t := range tasks {
		if t.Kind == "text" {
			chatBody(t)
		}
	}
	guiLastTasks = raw
	chatLastDevice = guiSelected
	resetList(h, len(guiTasks))
	for i, t := range guiTasks {
		if t.ID == selected {
			msg(h, 0x186, uintptr(i), 0)
			break
		}
	}
	if chatKeepAnchor != "" {
		for i, t := range guiTasks {
			if t.ID == chatKeepAnchor {
				msg(h, 0x197, uintptr(i), 0)
				break
			}
		}
		chatKeepAnchor = ""
	} else if changedDevice || nearBottom {
		scrollChatBottom()
	} else if top < len(guiTasks) {
		msg(h, 0x197, uintptr(top), 0)
	}
	return true
}

func scrollChatBottom() {
	h := guiControls[126]
	if len(guiTasks) == 0 {
		return
	}
	msg(h, 0x197, uintptr(len(guiTasks)-1), 0)
}

func paintChatScene(dc uintptr) {
	x, w, h := 318, guiWidth-318, guiHeight
	fill(dc, rct(x, 0, w, 66), paper)
	stroke(dc, x, 65, guiWidth, 65, lineColor, 1)
	d, ok := currentDevice()
	title, detail := "选择设备，开始对话", "文字与文件，都在这里收发"
	if ok && d.ID != guiNode.id {
		title = deviceName(d)
		detail = deviceDetail(d)
	}
	label(dc, title, x+28, 11, w-56, 26, ink, guiTitle)
	label(dc, detail, x+28, 37, w-56, 18, muted, guiSmall)
	if len(guiTasks) == 0 {
		paintEmpty(dc, x+16, 74, w-32, h-212, "transfer", "发一段文字，或分享一份文件", "对方发来的在左边，你发出的在右边。")
	}
	fill(dc, rct(x, h-138, w, 138), paper)
	stroke(dc, x, h-138, guiWidth, h-138, lineColor, 1)
	label(dc, "拖入发送 · 文件可拖出复制", x+232, h-128, w-260, 20, muted, guiTiny)
	hint := "Enter 换行 · Ctrl + Enter 发送 · 双击气泡选字"
	if time.Now().Before(chatFeedbackUntil) {
		hint = chatFeedback
	}
	label(dc, hint, x+24, h-34, w-156, 24, muted, guiTiny)
}
