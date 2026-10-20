package main

// The desktop stays on the Windows 7 Win32 surface. Presentation is kept here;
// device identity, transport, sharing and transfer state remain in the node.
import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	canvas                                                = color(246, 247, 243)
	paper                                                 = color(255, 255, 255)
	railColor                                             = color(236, 240, 232)
	ink                                                   = color(38, 59, 50)
	muted                                                 = color(129, 139, 131)
	lineColor                                             = color(233, 237, 231)
	accent                                                = color(49, 95, 78)
	tint                                                  = color(237, 243, 236)
	warning                                               = color(157, 111, 48)
	guiWhiteBrush, guiSmall, guiMedium, guiTitle, guiTiny uintptr
	guiConnect                                            uintptr
	guiPage, guiFilter, guiHover                          int
	guiAllTasks                                           []Task
	guiWidth, guiHeight                                   int
	guiButtonProc                                         = map[uintptr]uintptr{}
	guiListProc                                           = map[uintptr]uintptr{}
	guiAllControls                                        []int
	guiInLayout                                           bool
	guiWant                                               map[int]bool
	guiPositions                                          = map[int][4]int{}
	guiSelfName                                           string
)

var guiReadSnapshot func() map[string]interface{}
var guiReadShares func(Device) ([]Share, error)
var guiWindowTitle = "邻传"
var guiClientWidth, guiClientHeight = 1200, 654

type paintStruct struct {
	DC              uintptr
	Erase           int32
	Rect            rect
	Restore, Update int32
	Reserved        [32]byte
}

func invalidate(h uintptr) {
	if h != 0 {
		call(user.NewProc("InvalidateRect"), h, 0, 0)
	}
}
func rct(x, y, w, h int) rect {
	return rect{int32(sc(x)), int32(sc(y)), int32(sc(x + w)), int32(sc(y + h))}
}
func fill(dc uintptr, r rect, c uintptr) {
	b := call(gdi.NewProc("CreateSolidBrush"), c)
	call(user.NewProc("FillRect"), dc, uintptr(unsafe.Pointer(&r)), b)
	call(gdi.NewProc("DeleteObject"), b)
}
func rounded(dc uintptr, r rect, c, border uintptr, radius int) {
	if vectorRound(dc, r, c, border, radius, false) {
		return
	}
	b := call(gdi.NewProc("CreateSolidBrush"), c)
	p := call(gdi.NewProc("CreatePen"), 0, 1, border)
	oldB := call(gdi.NewProc("SelectObject"), dc, b)
	oldP := call(gdi.NewProc("SelectObject"), dc, p)
	call(gdi.NewProc("RoundRect"), dc, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B), uintptr(sc(radius*2)), uintptr(sc(radius*2)))
	call(gdi.NewProc("SelectObject"), dc, oldB)
	call(gdi.NewProc("SelectObject"), dc, oldP)
	call(gdi.NewProc("DeleteObject"), b)
	call(gdi.NewProc("DeleteObject"), p)
}
func stroke(dc uintptr, x1, y1, x2, y2 int, c uintptr, width int) {
	if vectorLine(dc, x1, y1, x2, y2, c, width) {
		return
	}
	p := call(gdi.NewProc("CreatePen"), 0, uintptr(sc(width)), c)
	old := call(gdi.NewProc("SelectObject"), dc, p)
	call(gdi.NewProc("MoveToEx"), dc, uintptr(sc(x1)), uintptr(sc(y1)), 0)
	call(gdi.NewProc("LineTo"), dc, uintptr(sc(x2)), uintptr(sc(y2)))
	call(gdi.NewProc("SelectObject"), dc, old)
	call(gdi.NewProc("DeleteObject"), p)
}
func label(dc uintptr, s string, x, y, w, h int, c, font uintptr) {
	drawText(dc, s, rct(x, y, w, h), c, font)
}
func centerText(dc uintptr, s string, r rect, c, font uintptr) {
	if vectorText(dc, s, r, c, font, 1) {
		return
	}
	call(gdi.NewProc("SetTextColor"), dc, c)
	call(gdi.NewProc("SetBkMode"), dc, 1)
	call(gdi.NewProc("SelectObject"), dc, font)
	call(user.NewProc("DrawTextW"), dc, uintptr(unsafe.Pointer(wp(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x20|0x800|0x4|1)
}
func icon(dc uintptr, kind string, x, y, size int, c uintptr) { sourceIcon(dc, kind, x, y, size, c) }
func visible(id int, yes bool) {
	if guiInLayout {
		guiWant[id] = yes
		return
	}
	if h := guiControls[id]; h != 0 {
		isVisible := call(user.NewProc("GetWindowLongW"), h, uintptr(0xfffffff0))&0x10000000 != 0
		if isVisible == yes {
			return
		}
		v := uintptr(0)
		if yes {
			v = 5
		}
		showWindow.Call(h, v)
	}
}
func enabled(id int, yes bool) {
	h := guiControls[id]
	if h == 0 || (call(user.NewProc("IsWindowEnabled"), h) != 0) == yes {
		return
	}
	v := uintptr(0)
	if yes {
		v = 1
	}
	call(user.NewProc("EnableWindow"), guiControls[id], v)
	invalidate(guiControls[id])
}

func layout() {
	var r rect
	call(user.NewProc("GetClientRect"), guiMain, uintptr(unsafe.Pointer(&r)))
	guiWidth, guiHeight = int(float64(r.R)/guiScale), int(float64(r.B)/guiScale)
	w, h := guiWidth, guiHeight
	x := 351
	right := w - x - 33
	if right < 300 {
		return
	}
	guiInLayout = true
	guiWant = map[int]bool{}
	for _, id := range guiAllControls {
		guiWant[id] = false
	}
	move(201, 9, 95, 58, 61)
	move(202, 9, 169, 58, 61)
	move(203, 9, h-80, 58, 61)
	move(103, 19, h-132, 38, 38)
	visible(103, true)
	deviceH := 75 * len(guiDevices)
	if deviceH > h-190 {
		deviceH = h - 190
	}
	if deviceH < 1 {
		deviceH = 1
	}
	move(111, 92, 79, 209, deviceH)
	connectY := 79 + deviceH + 6
	if connectY > h-126 || len(guiDevices) == 0 {
		connectY = h - 126
	}
	move(204, 92, connectY, 209, 38)
	move(205, 92, h-67, 209, 51)
	for _, id := range []int{201, 202, 203, 204, 205} {
		visible(id, true)
	}
	visible(111, len(guiDevices) > 0)
	move(154, w-153, h-37, 120, 23)
	visible(154, guiPage != 0)
	switch guiPage {
	case 0:
		switchChatDraft(guiSelected)
		move(126, 334, 74, w-350, h-212)
		if chatLayoutWidth != chatListWidth() {
			refreshChatList(guiAllTasks)
		}
		visible(126, len(guiTasks) > 0)
		for i, t := range guiTasks {
			height := uintptr(sc(chatRowHeightAt(t, i)))
			if msg(guiControls[126], 0x1a1, uintptr(i), 0) != height {
				msg(guiControls[126], 0x1a0, uintptr(i), height)
			}
		}
		move(121, 342, h-129, 80, 27)
		move(122, 432, h-129, 80, 27)
		move(170, 343, h-94, w-370, 49)
		move(171, w-103, h-36, 78, 29)
		for _, id := range []int{121, 122, 170, 171} {
			visible(id, true)
		}
	case 1:
		move(246, w-153, 40, 120, 37)
		move(123, x, 111, 116, 37)
		move(124, x+129, 111, 126, 37)
		move(141, w-129, 111, 96, 37)
		move(143, x, 202, right, h-296)
		visible(143, len(guiShares) > 0)
		move(142, w-161, h-84, 128, 34)
		move(144, x, h-84, 142, 34)
		for _, id := range []int{246, 123, 124, 141, 142, 144} {
			visible(id, true)
		}
	case 2:
		move(105, x+35, 148, right-173, 24)
		move(106, w-132, 141, 76, 34)
		move(145, x+24, 242, right-48, 23)
		move(150, x+24, 271, 126, 27)
		move(151, x+163, 271, 134, 27)
		move(116, x+24, 545, 142, 29)
		move(152, x+181, 545, 120, 29)
		col := (right - 60) / 2
		for i := 0; i < 6; i++ {
			move(260+i, x+24+(i%2)*(col+12), 353+(i/2)*44, col, 32)
			visible(260+i, true)
		}
		for _, id := range []int{105, 106, 145, 150, 151, 116, 152} {
			visible(id, true)
		}
	}
	updateContext()
	guiInLayout = false
	for id, show := range guiWant {
		visible(id, show)
	}
	for _, id := range []int{201, 202, 203, 205, 210, 211, 212} {
		invalidate(guiControls[id])
	}
	invalidate(guiMain)
}
func taskAction(t Task) (int, string) {
	if t.Status == "done" {
		if (t.Media == "file" || t.Media == "folder" || t.Media == "files") && chatMediaReady(t) {
			if t.Folder {
				return 176, "打开文件夹"
			}
			return 176, "打开文件"
		}
		if isChatMedia(t) && chatMediaReady(t) {
			return 176, "查看 / 播放"
		}
		if t.Kind == "text" && (t.Direction == "in" || t.Direction == "out") {
			return 164, "查看文字"
		}
		if t.Direction == "in" || t.Direction == "out" && t.Pull {
			return 153, "打开"
		}
		return 0, ""
	}
	if t.Status == "rejected" {
		return 0, ""
	}
	if t.Status == "waiting" {
		return 127, "接收"
	}
	if t.Direction == "web" || t.Direction == "webout" {
		return 0, ""
	}
	if t.Status == "paused" || t.Status == "queued" {
		return 131, "继续"
	}
	if t.Status == "sending" || t.Status == "receiving" || t.Status == "waiting_remote" {
		return 130, "暂停"
	}
	return 0, ""
}
func updateContext() {
	for _, id := range []int{127, 128, 129, 130, 131, 153, 164} {
		visible(id, false)
	}
	d, ok := currentDevice()
	ready := ok && d.Online && d.ID != guiNode.id
	enabled(121, ready)
	enabled(122, ready)
	enabled(159, ready)
	enabled(170, ready)
	enabled(171, ready && !chatSending)

	selected := int(int32(msg(guiControls[143], 0x188, 0, 0)))
	valid := selected >= 0 && selected < len(guiShares)
	enabled(142, ok && d.Online && d.ID != guiNode.id && valid)
	enabled(144, ok && d.ID == guiNode.id && valid)
}
func refreshGUI() {
	if guiNode == nil || guiDragging {
		return
	}
	snapshot := guiNode.snapshot(true, WebClient{})
	if guiReadSnapshot != nil {
		snapshot = guiReadSnapshot()
	}
	if name := guiNode.name(); name != guiSelfName {
		guiSelfName = name
		invalidate(guiControls[205])
		invalidate(guiMain)
	}
	devices := snapshot["devices"].([]Device)
	other := make([]Device, 0, len(devices))
	for _, d := range devices {
		if d.ID != guiNode.id {
			other = append(other, d)
		}
	}
	allTasks := snapshot["tasks"].([]Task)
	known := map[string]bool{}
	for _, d := range other {
		known[d.ID] = true
	}
	for _, t := range allTasks {
		peer := conversationPeer(t, guiNode.id)
		if peer == "" || peer == guiNode.id || known[peer] {
			continue
		}
		name := t.SenderName
		if t.Direction == "out" {
			name = t.TargetName
		}
		if name == "" {
			name = "离线设备"
		}
		other = append(other, Device{ID: peer, Name: name, Paired: true})
		known[peer] = true
	}
	raw, _ := json.Marshal(other)
	changed := false
	if string(raw) != guiLastDevices {
		guiDevices = other
		guiLastDevices = string(raw)
		resetList(guiControls[111], len(other))
		changed = true
		found := guiSelected == guiNode.id
		for i, d := range other {
			if d.ID == guiSelected {
				msg(guiControls[111], 0x186, uintptr(i), 0)
				found = true
			}
		}
		if !found {
			guiSelected = ""
			if len(other) > 0 {
				guiSelected = other[0].ID
				msg(guiControls[111], 0x186, 0, 0)
			}
			guiShares = nil
			resetList(guiControls[143], 0)
		}
	}
	guiAllTasks = snapshot["tasks"].([]Task)
	refreshIncomingNotices(guiAllTasks)
	active, finished := 0, 0
	for _, t := range guiAllTasks {
		if t.Status == "done" || t.Status == "rejected" {
			finished++
		} else {
			active++
		}
	}
	set(guiControls[211], fmt.Sprintf("进行中 %d", active))
	set(guiControls[212], fmt.Sprintf("已完成 %d", finished))
	if refreshChatList(guiAllTasks) {
		changed = true
		invalidate(guiControls[111])
	}

	if v := snapshot["saveDir"].(string); text(guiControls[145]) != v {
		set(guiControls[145], v)
		changed = true
	}
	// Editing is not overwritten by periodic refreshes.
	if text(guiControls[107]) != guiNode.addr {
		set(guiControls[107], guiNode.addr)
		changed = true
	}
	if changed {
		layout()
	}
}

func deviceDetail(d Device) string {
	if d.ID == guiNode.id {
		return "本机 · 查看我的分享"
	}
	if d.Online {
		if strings.HasPrefix(d.ID, "web:") {
			return "在线 · 手机网页"
		}
		if d.Via != "" {
			return "在线 · 经 " + d.Via + " 中转"
		}
		return "在线 · 已连接"
	}
	if d.Paired {
		return "离线 · 等待重新连接"
	}
	return "已发现 · 正在自动连接"
}
func deviceName(d Device) string {
	name := d.Name
	for _, other := range guiDevices {
		if other.ID != d.ID && other.Name == d.Name {
			suffix := d.ID
			if len(suffix) > 6 {
				suffix = suffix[len(suffix)-6:]
			}
			return name + " · " + suffix
		}
	}
	return name
}
func taskStatus(t Task) string {
	status := map[string]string{"preparing": "准备文件", "waiting": "等待接收", "waiting_remote": "等待对方接收", "receiving": "接收中", "sending": "发送中", "paused": "已暂停", "queued": "等待继续", "done": "已完成", "rejected": "已拒绝", "browser": "等待手机下载", "waiting_browser": "等待手机点击下载"}[t.Status]
	if status == "" {
		return t.Status
	}
	return status
}
func taskPeer(t Task) string {
	if t.Direction == "in" || t.Direction == "web" {
		return "来自 " + t.SenderName
	}
	return "发往 " + t.TargetName
}
func fileBadge(title string, folder bool) string {
	if folder {
		return "目录"
	}
	ext := strings.TrimPrefix(strings.ToUpper(filepath.Ext(title)), ".")
	switch ext {
	case "DOCX":
		return "DOC"
	case "XLSX":
		return "XLS"
	case "PPTX":
		return "PPT"
	}
	if len(ext) > 4 || ext == "" {
		return "文件"
	}
	return ext
}

func paintMain(hwnd uintptr) {
	var ps paintStruct
	dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	mem := call(gdi.NewProc("CreateCompatibleDC"), dc)
	bmp := call(gdi.NewProc("CreateCompatibleBitmap"), dc, uintptr(sc(guiWidth)), uintptr(sc(guiHeight)))
	old := call(gdi.NewProc("SelectObject"), mem, bmp)
	paintScene(mem)
	call(gdi.NewProc("BitBlt"), dc, 0, 0, uintptr(sc(guiWidth)), uintptr(sc(guiHeight)), mem, 0, 0, 0x00cc0020)
	call(gdi.NewProc("SelectObject"), mem, old)
	call(gdi.NewProc("DeleteObject"), bmp)
	call(gdi.NewProc("DeleteDC"), mem)
	call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
}
func paintScene(dc uintptr) {
	w, h := guiWidth, guiHeight
	x := 351
	right := w - x - 33
	fill(dc, rct(0, 0, w, h), canvas)
	fill(dc, rct(0, 0, 76, h), railColor)
	fill(dc, rct(76, 0, 242, h), paper)
	stroke(dc, 317, 0, 317, h, lineColor, 1)
	rounded(dc, rct(20, 25, 37, 37), accent, accent, 12)
	icon(dc, "logo", 26, 31, 25, paper)
	label(dc, "我的设备", 101, 26, 160, 22, ink, fontSize(17, 600))
	rounded(dc, rct(270, 26, 22, 22), color(243, 245, 239), color(243, 245, 239), 6)
	centerText(dc, fmt.Sprintf("%d", len(guiDevices)), rct(270, 26, 22, 22), ink, fontSize(11, 400))
	label(dc, "选择一台设备，开始传文件", 101, 55, 200, 17, muted, fontSize(10, 400))
	stroke(dc, 92, h-67, 301, h-67, lineColor, 1)
	if len(guiDevices) == 0 {
		icon(dc, "computer", 178, 154, 34, muted)
		label(dc, "还没有发现其他设备", 112, 208, 187, 22, ink, fontSize(12, 400))
		label(dc, "在另一台电脑打开邻传，", 112, 239, 187, 20, muted, fontSize(11, 400))
		label(dc, "或用手机扫描本机二维码。", 112, 261, 190, 20, muted, fontSize(11, 400))
		label(dc, "有线与无线若分属不同网段，", 112, 297, 190, 20, muted, fontSize(11, 400))
		label(dc, "发现后自动连接，也可填写地址。", 112, 319, 190, 20, muted, fontSize(11, 400))
	}
	title, subtitle := "传文件，就在身边。", "选好设备，剩下的交给邻传。"
	if guiPage == 1 {
		title = "共享，随时可取。"
		subtitle = "只展示主动分享的内容，下载进度可在传输页查看。"
	}
	if guiPage == 2 {
		title = "按你的习惯。"
		subtitle = "设备名称、接收位置和消息提醒，都在这里。"
	}
	if guiPage != 0 {
		label(dc, title, x, 30, right-130, 30, ink, fontSize(23, 600))
		label(dc, subtitle, x, 67, right, 19, muted, fontSize(11, 400))
	}
	switch guiPage {
	case 0:
		paintChatScene(dc)
	case 1:
		name := "请在左侧选择一台设备"
		if d, ok := currentDevice(); ok {
			name = deviceName(d) + " 的共享文件"
		}
		label(dc, name, x, 169, right, 22, ink, fontSize(13, 600))
		if len(guiShares) == 0 {
			detail := "选择设备，再点击刷新。"
			if d, ok := currentDevice(); ok {
				detail = "这台设备暂未分享文件。"
				if !d.Online {
					detail = "正在自动连接；对方也需运行新版邻传。"
				}
				if strings.HasPrefix(d.ID, "web:") {
					detail = "手机网页支持收发文件，暂不提供共享列表。"
				}
			}
			paintEmpty(dc, x, 202, right, h-296, "folder", "这里还没有共享文件", detail)
		}
	case 2:
		rounded(dc, rct(x, 104, right, 95), paper, lineColor, 12)
		label(dc, "设备名称", x+24, 115, right-48, 21, muted, fontSize(11, 400))
		rounded(dc, rct(x+24, 141, right-151, 34), paper, lineColor, 7)
		label(dc, "其他设备会用这个名字识别你的电脑。", x+24, 180, right-48, 16, muted, fontSize(10, 400))
		rounded(dc, rct(x, 211, right, 96), paper, lineColor, 12)
		label(dc, "文件接收位置", x+24, 219, right-48, 21, muted, fontSize(11, 400))
		rounded(dc, rct(x, 319, right, 176), paper, lineColor, 12)
		label(dc, "消息提醒 · 点击即保存", x+24, 329, right-48, 21, ink, fontSize(13, 600))
		rounded(dc, rct(x, 508, right, 80), paper, lineColor, 12)
		label(dc, "后台运行", x+24, 519, right-48, 22, ink, fontSize(13, 600))
	}
	status := text(guiControls[190])
	if status == "" {
		status = guiNode.name() + "已就绪"
	}
	if guiPage == 0 {
		return
	}
	dot(dc, x, h-26, 4, color(120, 148, 119))
	label(dc, status, x+8, h-34, right-140, 20, muted, fontSize(9, 400))
}
func paintEmpty(dc uintptr, x, y, w, h int, kind, title, detail string) {
	if h < 80 {
		return
	}
	mid := y + h/2
	icon(dc, kind, x+w/2-15, mid-61, 30, muted)
	centerText(dc, title, rct(x+16, mid-17, w-32, 27), ink, guiFont)
	centerText(dc, detail, rct(x+12, mid+17, w-24, 24), muted, guiSmall)
}

func paintButton(di *drawItem) {
	if di.ID >= 260 && di.ID <= 265 {
		dc, r := di.DC, di.Rect
		fill(dc, r, paper)
		on := guiNode.noticeSettings().values()[int(di.ID)-260]
		label(dc, noticeSettingLabels[int(di.ID)-260], 8, 7, int(float64(r.R-r.L)/guiScale)-65, 22, ink, guiSmall)
		end := int(float64(r.R-r.L)/guiScale) - 8
		bg := lineColor
		knob := end - 35
		if on {
			bg = accent
			knob = end - 19
		}
		rounded(dc, rct(end-40, 5, 40, 22), bg, bg, 11)
		rounded(dc, rct(knob, 8, 16, 16), paper, paper, 8)
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	id, dc, r := int(di.ID), di.DC, di.Rect
	if id >= 201 && id <= 203 {
		fill(dc, r, railColor)
		selected := id-201 == guiPage
		if selected {
			rounded(dc, r, paper, paper, 11)
		} else if guiHover == id {
			rounded(dc, r, tint, tint, 11)
		}
		c := muted
		if selected {
			c = accent
		}
		kind := []string{"transfer", "folder", "settings"}[id-201]
		icon(dc, kind, 19, 11, 20, c)
		tr := rct(0, 37, 58, 13)
		centerText(dc, text(di.Hwnd), tr, c, fontSize(10, 400))
		if id == 201 && guiNotices.count("") > 0 && guiNode.noticeSettings().Badges {
			rounded(dc, rct(38, 5, 16, 16), color(190, 70, 58), color(190, 70, 58), 8)
			centerText(dc, "!", rct(38, 5, 16, 16), paper, guiTiny)
		}
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	if id == 103 {
		fill(dc, r, railColor)
		if guiHover == id {
			rounded(dc, r, paper, paper, 10)
		}
		icon(dc, "qr", 9, 9, 20, accent)
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	if id == 205 {
		fill(dc, r, paper)
		if guiSelected == guiNode.id {
			rounded(dc, r, tint, tint, 10)
		}
		stroke(dc, 0, 0, 209, 0, lineColor, 1)
		rounded(dc, rct(8, 20, 32, 32), color(243, 245, 239), color(243, 245, 239), 16)
		centerText(dc, "本机", rct(8, 20, 32, 32), accent, fontSize(11, 400))
		label(dc, guiNode.name(), 50, 20, 150, 15, ink, fontSize(11, 500))
		dot(dc, 50, 43, 4, color(120, 148, 119))
		label(dc, "可被发现 · 正在运行", 58, 38, 145, 13, muted, fontSize(9, 400))
		return
	}
	if id >= 210 && id <= 212 {
		fill(dc, r, canvas)
		c := muted
		weight := 400
		if id-210 == guiFilter {
			c = accent
			weight = 600
			rounded(dc, rect{r.L, r.B - int32(sc(2)), r.R, r.B}, accent, accent, 1)
		}
		tr := r
		tr.B = tr.T + int32(sc(13))
		centerText(dc, text(di.Hwnd), tr, c, fontSize(10, weight))
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	base := canvas
	if id == 121 || id == 122 || id == 159 || id == 106 || id == 115 || id == 127 || id == 129 || id == 162 || id == 163 || id == 171 {
		base = paper
	}
	fill(dc, r, base)
	if id == 122 || id == 154 || id == 129 {
		size := 10
		if id == 154 {
			size = 9
		}
		centerText(dc, text(di.Hwnd), r, muted, fontSize(size, 400))
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	if id == 204 {
		bg := paper
		if guiHover == id {
			bg = tint
		}
		vectorRoundStyle(dc, r, bg, color(189, 199, 188), 8, false, true)
		centerText(dc, text(di.Hwnd), r, muted, fontSize(11, 400))
		if guiKeyboardMode && di.State&0x10 != 0 {
			focusOutline(dc, r)
		}
		return
	}
	if id == 130 || id == 131 || id == 153 || id == 164 {
		rounded(dc, r, paper, lineColor, 7)
		glyph := "Ⅱ"
		if id == 131 {
			glyph = "▷"
		}
		if id == 153 {
			glyph = "↗"
		}
		if id == 164 {
			glyph = "文"
		}
		centerText(dc, glyph, r, muted, fontSize(12, 400))
		return
	}
	bg, fg, edge := paper, ink, lineColor
	primary := id == 121 || id == 106 || id == 115 || id == 127 || id == 123 || id == 162 || id == 163 || id == 171
	if primary {
		bg = accent
		fg = paper
		edge = accent
	}
	if guiHover == id {
		if primary {
			bg = color(44, 86, 70)
			edge = bg
		} else {
			bg = tint
		}
	}
	if di.State&1 != 0 {
		if primary {
			bg = color(39, 78, 63)
			edge = bg
		} else {
			bg = lineColor
		}
	}
	if di.State&4 != 0 {
		if primary {
			bg = color(173, 191, 177)
			edge = bg
			fg = paper
		} else {
			fg = color(173, 180, 171)
		}
	}
	rounded(dc, r, bg, edge, 8)
	size := 11
	if id == 121 {
		size = 12
	}
	if id == 127 {
		size = 10
	}
	tr := r
	if id == 103 {
		icon(dc, "qr", 13, 11, 15, ink)
		tr.L += int32(sc(18))
	}
	centerText(dc, text(di.Hwnd), tr, fg, fontSize(size, 400))
	if guiKeyboardMode && di.State&0x10 != 0 {
		f := r
		f.L += 2
		f.T += 2
		f.R -= 2
		f.B -= 2
		focusOutline(dc, f)
	}
}
func paintItem(di *drawItem) {
	w, h := int(di.Rect.R-di.Rect.L), int(di.Rect.B-di.Rect.T)
	if di.ItemID == 0xffffffff || w <= 0 || h <= 0 {
		return
	}
	bufferDC(di.DC, w, h, int(di.Rect.L), int(di.Rect.T), func(dc uintptr) {
		copyItem := *di
		copyItem.DC = dc
		copyItem.Rect = rect{0, 0, int32(w), int32(h)}
		paintItemDirect(&copyItem)
	})
}
func paintItemDirect(di *drawItem) {
	if di.ItemID == 0xffffffff {
		return
	}
	i, dc, r := int(di.ItemID), di.DC, di.Rect
	bg := canvas
	if di.ID == 111 {
		bg = paper
	}
	fill(dc, r, bg)
	x, y := int(float64(r.L)/guiScale), int(float64(r.T)/guiScale)
	w := int(float64(r.R-r.L) / guiScale)
	switch di.ID {
	case 111:
		if i >= len(guiDevices) {
			return
		}
		d := guiDevices[i]
		if di.State&1 != 0 {
			rounded(dc, rct(x, y, w, 69), tint, lineColor, 10)
		}
		rounded(dc, rct(x+11, y+17, 36, 36), paper, lineColor, 10)
		kind := "computer"
		if strings.HasPrefix(d.ID, "web:") {
			kind = "phone"
		}
		icon(dc, kind, x+19, y+25, 19, muted)
		c := ink
		if !d.Online {
			c = muted
		}
		label(dc, deviceName(d), x+57, y+17, w-86, 16, c, fontSize(12, 500))
		pending := 0
		for _, t := range guiAllTasks {
			if conversationPeer(t, guiNode.id) == d.ID && t.Direction == "in" && t.Status == "waiting" {
				pending++
			}
		}
		if unread := guiNotices.count(d.ID); unread > pending {
			pending = unread
		}
		if pending > 0 && guiNode.noticeSettings().Badges {
			rounded(dc, rct(x+w-31, y+12, 25, 21), color(190, 70, 58), color(190, 70, 58), 9)
			badge := fmt.Sprint(pending)
			if pending > 99 {
				badge = "99+"
			}
			centerText(dc, badge, rct(x+w-31, y+12, 25, 21), paper, guiTiny)
		}
		detail := deviceDetail(d)
		if d.Via != "" {
			detail = "在线 · 经其他设备中转"
		}
		offset := 57
		if d.Online {
			dot(dc, x+57, y+44, 5, color(120, 148, 119))
			offset = 66
		}
		label(dc, detail, x+offset, y+38, w-offset-9, 14, muted, fontSize(10, 400))
		if di.State&1 != 0 {
			label(dc, "›", x+w-18, y+25, 12, 20, accent, fontSize(18, 400))
		}
	case 126:
		if i < len(guiTasks) {
			paintChatItem(dc, guiTasks[i], x, y, w, i)
		}
	case 143:
		if i >= len(guiShares) {
			return
		}
		v := guiShares[i]
		c := paper
		if di.State&1 != 0 {
			c = tint
		}
		rounded(dc, rct(x, y+3, w, 77), c, lineColor, 11)
		rounded(dc, rct(x+15, y+20, 37, 41), tint, tint, 7)
		if v.Folder {
			icon(dc, "folder", x+22, y+29, 22, muted)
		} else {
			centerText(dc, fileBadge(v.Name, false), rct(x+15, y+20, 37, 41), accent, fontSize(9, 600))
		}
		label(dc, v.Name, x+67, y+15, w-85, 25, ink, fontSize(12, 500))
		label(dc, fmt.Sprintf("%s · %d 个文件", formatSize(v.Size), len(v.Files)), x+67, y+45, w-85, 22, muted, fontSize(10, 400))
	}
	if guiKeyboardMode && di.State&0x10 != 0 {
		f := r
		f.L += 1
		f.R -= 1
		f.T += 1
		f.B -= 1
		focusOutline(dc, f)
	}
}
func themedControl(class, label string, id int, style uint32) uintptr {
	h := control(class, label, id, style)
	guiAllControls = append(guiAllControls, id)
	if class == "BUTTON" {
		acceptDesktopDrops(h)
		guiButtonProc[h] = setWindowProcedure(h, syscall.NewCallback(buttonProc))
	}
	if class == "LISTBOX" {
		acceptDesktopDrops(h)
		guiListProc[h] = setWindowProcedure(h, syscall.NewCallback(listProc))
	}
	return h
}
func buttonProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if m == wmDropFiles {
		postMessage.Call(guiMain, wmDropFiles, w, 0)
		return 0
	}
	if m == 0x201 {
		guiKeyboardMode = false
	}
	if m == 0x200 {
		id := int(call(user.NewProc("GetDlgCtrlID"), hwnd))
		if id == 103 && !guiQRVisible {
			showPhonePopover(false)
		}
		if guiHover != id {
			guiHover = id
			invalidate(hwnd)
		}
		track := struct {
			Size, Flags uint32
			Hwnd        uintptr
			Hover       uint32
		}{Flags: 2, Hwnd: hwnd}
		track.Size = uint32(unsafe.Sizeof(track))
		call(user.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&track)))
	}
	if m == 0x2a3 {
		guiHover = 0
		invalidate(hwnd)
	}
	return call(user.NewProc("CallWindowProcW"), guiButtonProc[hwnd], hwnd, uintptr(m), w, l)
}
func listProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if m == 0x14 {
		fillListBackground(hwnd, w)
		return 1
	}
	if m == 0xf {
		paintBufferedList(hwnd)
		return 0
	}
	if hwnd == guiControls[126] && chatDragMessage(hwnd, m, w, l) {
		return 0
	}
	if m == wmDropFiles {
		postMessage.Call(guiMain, wmDropFiles, w, 0)
		return 0
	}
	if hwnd == guiControls[126] && m == 0x204 {
		hit := msg(hwnd, 0x1a9, 0, l)
		if hit>>16 == 0 {
			msg(hwnd, 0x186, hit&0xffff, 0)
		}
	}
	if hwnd == guiControls[126] && m == 0x100 && w == 67 && call(user.NewProc("GetKeyState"), 17)&0x8000 != 0 {
		doCommand(172, 0)
		return 0
	}
	if hwnd == guiControls[126] && (m == 0x115 || m == 0x20a) {
		closeChatSelection()
	}

	if m == 0x201 || m == 0x204 {
		guiKeyboardMode = false
	}
	if hwnd == guiControls[126] && m == 0x200 {
		trackChatHover(hwnd, l)
	}
	if hwnd == guiControls[126] && (m == 0x2a3 || m == 0x115 || m == 0x20a) {
		if chatHoverID != "" {
			old := chatHoverID
			chatHoverID = ""
			invalidateChatCopy(old)
		}
	}
	if hwnd == guiControls[126] && m == 0x7b {
		taskContextMenu(guiMain)
		return 0
	}
	if hwnd == guiControls[126] && m == 0x100 && w == 13 {
		if t, ok := currentTask(); ok {
			if t.Kind == "text" && chatBody(t) != "" {
				doCommand(173, 0)
				return 0
			}
			if id, _ := taskAction(t); id != 0 {
				doCommand(id, 0)
			}
		}
		return 0
	}
	result := call(user.NewProc("CallWindowProcW"), guiListProc[hwnd], hwnd, uintptr(m), w, l)
	if m == 0x202 && hwnd == guiControls[126] {
		var client rect
		call(user.NewProc("GetClientRect"), hwnd, uintptr(unsafe.Pointer(&client)))
		hit := msg(hwnd, 0x1a9, 0, l)
		if hit>>16 == 0 {
			i := int(hit & 0xffff)
			if i < len(guiTasks) {
				var row rect
				msg(hwnd, 0x198, uintptr(i), uintptr(unsafe.Pointer(&row)))
				id := chatHitPart(guiTasks[i], int(float64(client.R)/guiScale), int(int16(l&0xffff)), int(int16(l>>16))-int(row.T), chatPartAt(i))
				if id != 0 {
					doCommand(id, 0)
				}
			}
		}
	}

	return result
}
func newFont(size, weight int) uintptr {
	return call(gdi.NewProc("CreateFontW"), uintptr(int32(-sc(size))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wp(guiFontFamily))))
}

func setMinimum(l uintptr) {
	info := (*struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point })(unsafe.Pointer(l))
	w, h := sc(940), sc(650)
	sw, sh := int(call(user.NewProc("GetSystemMetrics"), 0)), int(call(user.NewProc("GetSystemMetrics"), 1))
	if w > sw-30 {
		w = sw - 30
	}
	if h > sh-60 {
		h = sh - 60
	}
	info.MinTrack = point{int32(w), int32(h)}
}

func runDesktop(n *Node) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if int32(call(oleDrag.NewProc("OleInitialize"), 0)) >= 0 {
		defer call(oleDrag.NewProc("OleUninitialize"))
	}
	guiNode = n
	vectorStart()
	defer func() {
		if vectorToken != 0 {
			call(vector.NewProc("GdiplusShutdown"), vectorToken)
		}
	}()
	if e := loadDesktopAssets(n.dir); e != nil {
		showError(e.Error())
		return
	}
	defer releaseDesktopAssets()
	defer releaseChatMedia()
	defer releaseVectorFonts()
	user.NewProc("SetProcessDPIAware").Call()
	dc := call(user.NewProc("GetDC"), 0)
	dpi := call(gdi.NewProc("GetDeviceCaps"), dc, 88)
	call(user.NewProc("ReleaseDC"), 0, dc)
	if dpi > 0 {
		guiScale = float64(dpi) / 96
	}
	selectDesktopFontFace()
	guiBrush = call(gdi.NewProc("CreateSolidBrush"), canvas)
	guiWhiteBrush = call(gdi.NewProc("CreateSolidBrush"), paper)
	guiFont = fontSize(13, 400)
	guiSmall = fontSize(11, 400)
	guiTiny = fontSize(9, 400)
	guiMedium = fontSize(13, 600)
	guiTitle = fontSize(18, 600)
	guiBold = fontSize(23, 600)
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	if e := loadDesktopIcons(instance); e != nil {
		// An icon failure must not prevent file transfers; retain a system fallback.
		guiAppIcon = call(user.NewProc("LoadIconW"), 0, 32512)
		guiSmallIcon = guiAppIcon
	}
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(mainProc), Instance: instance, Icon: guiAppIcon, SmallIcon: guiSmallIcon, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Background: guiBrush, Name: wp(desktopClass(n.dir))}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	sw, sh := int(call(user.NewProc("GetSystemMetrics"), 0)), int(call(user.NewProc("GetSystemMetrics"), 1))
	width, height := sc(1120), sc(770)
	if guiClientWidth > 0 {
		frame := rct(0, 0, guiClientWidth, guiClientHeight)
		call(user.NewProc("AdjustWindowRectEx"), uintptr(unsafe.Pointer(&frame)), 0x02cf0000, 0, 0)
		width = int(frame.R - frame.L)
		height = int(frame.B - frame.T)
	}
	if width > sw-40 {
		width = sw - 40
	}
	if height > sh-60 {
		height = sh - 60
	}
	guiMain = call(createWindow, 0, uintptr(unsafe.Pointer(wc.Name)), uintptr(unsafe.Pointer(wp(guiWindowTitle))), 0x02cf0000, uintptr((sw-width)/2), uintptr((sh-height)/2), uintptr(width), uintptr(height), 0, 0, instance, 0)
	if guiMain == 0 {
		showError("无法创建客户端窗口")
		return
	}
	setDesktopWindowIcons(guiMain)
	acceptDesktopDrops(guiMain)
	for _, item := range []struct {
		id    int
		label string
	}{{201, "传输"}, {202, "共享"}, {203, "设置"}, {204, "＋ 连接其他设备"}, {205, "本机"}, {103, "手机连接"}, {121, "＋ 文件"}, {122, "文件夹"}, {123, "＋ 分享文件"}, {124, "分享文件夹"}, {127, "接收"}, {128, "接收并信任"}, {129, "拒绝"}, {130, "暂停"}, {131, "继续"}, {141, "刷新共享"}, {142, "下载到本机"}, {144, "移除我的分享"}, {150, "打开接收目录"}, {151, "更改保存位置"}, {152, "退出邻传"}, {153, "打开所在文件夹"}, {154, "打开接收文件夹  ↗"}, {116, "最小化到托盘"}, {106, "保存"}, {210, "全部"}, {211, "进行中"}, {212, "已完成"}, {246, "我的分享"}} {
		themedControl("BUTTON", item.label, item.id, 0)
	}
	createChatComposer()
	for i, name := range noticeSettingLabels {
		themedControl("BUTTON", name, 260+i, 0)
	}
	updateNoticeControls()
	themedControl("BUTTON", "发送文字", 159, 0)
	themedControl("BUTTON", "查看文字", 164, 0)
	themedControl("EDIT", n.name(), 105, 0x80)
	registerChatViewport()
	for _, id := range []int{111, 126, 143} {
		style := uint32(0x00200000 | 0x10 | 1 | 0x100 | 0x40 | 0x2000)
		if id == 126 {
			themedControl("LinDropConversation", "", id, 0x00200000|0x02000000)
			acceptDesktopDrops(guiControls[id])
			continue
		}
		themedControl("LISTBOX", "", id, style)
	}
	for _, id := range []int{107, 140, 145, 190} {
		themedControl("STATIC", "", id, 0)
	}
	msg(guiControls[145], 0x30, guiFont, 1)
	// Pairing inputs live in their own owned window and are created on demand.
	refreshGUI()
	layout()
	call(user.NewProc("SetTimer"), guiMain, 1, 800, 0)
	guiTray = trayData{Size: uint32(unsafe.Sizeof(trayData{})), Hwnd: guiMain, ID: 1, Flags: 7, Message: 0x8004, Icon: guiSmallIcon}
	copy(guiTray.Tip[:], syscall.StringToUTF16("邻传 · 节点正在运行"))
	call(shell.NewProc("Shell_NotifyIconW"), 0, uintptr(unsafe.Pointer(&guiTray)))
	showWindow.Call(guiMain, 1)
	call(user.NewProc("UpdateWindow"), guiMain)
	var message winMsg
	for int32(call(user.NewProc("GetMessageW"), uintptr(unsafe.Pointer(&message)), 0, 0, 0)) > 0 {
		if message.Message == 0x100 {
			guiKeyboardMode = true
		}
		if message.Message == 0x100 && message.WParam == 27 {
			if guiTextWindow != 0 {
				destroyWindow.Call(guiTextWindow)
				continue
			}
			if guiConnect != 0 {
				destroyWindow.Call(guiConnect)
				continue
			}
			if guiQRVisible {
				msg(qrWindow, 0x10, 0, 0)
				continue
			}
		}
		if guiTextWindow != 0 {
			if !guiTextReadOnly && message.Message == 0x100 && message.WParam == 13 && int16(call(user.NewProc("GetKeyState"), 0x11)) < 0 {
				sendTextFromWindow()
				continue
			}
			if call(user.NewProc("IsDialogMessageW"), guiTextWindow, uintptr(unsafe.Pointer(&message))) != 0 {
				continue
			}
		}
		if guiConnect != 0 && call(user.NewProc("IsDialogMessageW"), guiConnect, uintptr(unsafe.Pointer(&message))) != 0 {
			continue
		}
		if call(user.NewProc("IsDialogMessageW"), guiMain, uintptr(unsafe.Pointer(&message))) != 0 {
			continue
		}
		call(user.NewProc("TranslateMessage"), uintptr(unsafe.Pointer(&message)))
		call(user.NewProc("DispatchMessageW"), uintptr(unsafe.Pointer(&message)))
	}
}

func connectProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x10:
		destroyWindow.Call(hwnd)
		return 0
	case 0x2:
		guiConnect = 0
		for _, id := range []int{113, 114, 115} {
			delete(guiControls, id)
		}
		return 0
	case 0x111:
		if w&0xffff == 2 {
			destroyWindow.Call(hwnd)
			return 0
		}
		if w&0xffff == 1 {
			doCommand(115, 0)
			return 0
		}
		doCommand(int(w&0xffff), int((w>>16)&0xffff))
		return 0
	case 0x2b:
		paintButton((*drawItem)(unsafe.Pointer(l)))
		return 1
	case 0x133, 0x138:
		call(gdi.NewProc("SetTextColor"), w, ink)
		call(gdi.NewProc("SetBkColor"), w, paper)
		return guiWhiteBrush
	case 0xf:
		var ps paintStruct
		dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		label(dc, "连接另一台设备", 26, 20, 400, 35, ink, guiTitle)
		label(dc, "同一局域网会自动连接，跨网段可在这里填地址。", 26, 65, 412, 24, muted, guiSmall)
		label(dc, "对方电脑地址（含端口）", 26, 105, 398, 23, muted, guiSmall)
		rounded(dc, rct(26, 136, 398, 35), paper, lineColor, 8)
		label(dc, "无需配对码 · 两台电脑均需使用新版", 26, 185, 412, 20, muted, guiSmall)
		if guiPairError != "" {
			label(dc, guiPairError, 26, 210, 412, 23, warning, guiSmall)
		}
		call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), hwnd, uintptr(m), w, l)
}
func showConnect() {
	hideQR()
	if guiConnect != 0 {
		showWindow.Call(guiConnect, 9)
		call(user.NewProc("SetForegroundWindow"), guiConnect)
		return
	}
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	guiPairError = ""
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(connectProc), Instance: instance, Icon: guiAppIcon, SmallIcon: guiSmallIcon, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Background: guiWhiteBrush, Name: wp("LinNodeConnect")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	var main rect
	call(user.NewProc("GetWindowRect"), guiMain, uintptr(unsafe.Pointer(&main)))
	guiConnect = call(createWindow, 0, uintptr(unsafe.Pointer(wc.Name)), uintptr(unsafe.Pointer(wp("邻传 · 连接设备"))), 0x00ca0000, uintptr(main.L+int32(sc(290))), uintptr(main.T+int32(sc(100))), uintptr(sc(470)), uintptr(sc(330)), guiMain, 0, instance, 0)
	setDesktopWindowIcons(guiConnect)
	addr := ""
	if d, ok := currentDevice(); ok {
		addr = d.Addr
	}
	for _, it := range []struct {
		id         int
		label      string
		style      uint32
		x, y, w, h int
	}{{113, addr, 0x80, 36, 142, 378, 24}, {115, "连接设备", 11, 26, 235, 398, 38}} {
		class := "EDIT"
		if it.id == 115 {
			class = "BUTTON"
		}
		h := call(createWindow, 0, uintptr(unsafe.Pointer(wp(class))), uintptr(unsafe.Pointer(wp(it.label))), uintptr(0x50010000|it.style), uintptr(sc(it.x)), uintptr(sc(it.y)), uintptr(sc(it.w)), uintptr(sc(it.h)), guiConnect, uintptr(it.id), instance, 0)
		guiControls[it.id] = h
		msg(h, 0x30, guiFont, 1)
		if class == "BUTTON" {
			guiButtonProc[h] = setWindowProcedure(h, syscall.NewCallback(buttonProc))
		}
	}
	cue(113, "例如 192.168.1.10:8776")
	showWindow.Call(guiConnect, 1)
	call(user.NewProc("SetFocus"), guiControls[113])
}
